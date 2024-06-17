package builder

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tinyrange/pkg2/v2/common"
	"github.com/tinyrange/pkg2/v2/filesystem"
	"go.starlark.net/starlark"
)

type zipToArchiveBuildResult struct {
	r *zip.Reader
}

// WriteTo implements common.BuildResult.
func (z *zipToArchiveBuildResult) WriteTo(w io.Writer) (n int64, err error) {
	for _, file := range z.r.File {
		var typ filesystem.FileType

		if file.Mode().IsDir() {
			typ = filesystem.TypeDirectory
		} else {
			typ = filesystem.TypeRegular
		}

		ent := &filesystem.CacheEntry{
			COffset:   n + 1024,
			CTypeflag: typ,
			CName:     file.Name,
			CLinkname: "",
			CSize:     int64(file.UncompressedSize64),
			CMode:     int64(file.Mode()),
			CUid:      0,
			CGid:      0,
			CModTime:  file.Modified.UnixMicro(),
			CDevmajor: 0,
			CDevminor: 0,
		}

		bytes, err := json.Marshal(&ent)
		if err != nil {
			return -1, err
		}

		if len(bytes) > filesystem.CACHE_ENTRY_SIZE {
			return -1, fmt.Errorf("oversized entry header: %d > %d", len(bytes), filesystem.CACHE_ENTRY_SIZE)
		} else if len(bytes) < filesystem.CACHE_ENTRY_SIZE {
			tmp := make([]byte, filesystem.CACHE_ENTRY_SIZE)
			copy(tmp, bytes)
			bytes = tmp
		}

		childN, err := w.Write(bytes)
		if err != nil {
			return -1, err
		}

		n += int64(childN)

		fh, err := file.Open()
		if err != nil {
			return -1, err
		}

		childN64, err := io.CopyN(w, fh, int64(file.UncompressedSize64))
		if err != nil {
			return -1, err
		}

		n += childN64
	}

	return
}

var (
	_ common.BuildResult = &zipToArchiveBuildResult{}
)

type tarToArchiveBuildResult struct {
	r *tar.Reader
}

// WriteTo implements common.BuildResult.
func (r *tarToArchiveBuildResult) WriteTo(w io.Writer) (n int64, err error) {
	for {
		hdr, err := r.r.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return -1, err
		}

		info := hdr.FileInfo()

		var typeFlag = filesystem.TypeRegular

		switch hdr.Typeflag {
		case tar.TypeReg:
			// pass
		case tar.TypeDir:
			typeFlag = filesystem.TypeDirectory
		case tar.TypeSymlink:
			typeFlag = filesystem.TypeSymlink
		case tar.TypeLink:
			typeFlag = filesystem.TypeLink
		default:
			return -1, fmt.Errorf("unknown type flag: %d", hdr.Typeflag)
		}

		ent := &filesystem.CacheEntry{
			COffset:   n + 1024,
			CTypeflag: typeFlag,
			CName:     hdr.Name,
			CLinkname: hdr.Linkname,
			CSize:     hdr.Size,
			CMode:     int64(info.Mode()),
			CUid:      hdr.Uid,
			CGid:      hdr.Gid,
			CModTime:  hdr.ModTime.UnixMicro(),
			CDevmajor: hdr.Devmajor,
			CDevminor: hdr.Devminor,
		}

		bytes, err := json.Marshal(&ent)
		if err != nil {
			return -1, err
		}

		if len(bytes) > filesystem.CACHE_ENTRY_SIZE {
			return -1, fmt.Errorf("oversized entry header: %d > %d", len(bytes), filesystem.CACHE_ENTRY_SIZE)
		} else if len(bytes) < filesystem.CACHE_ENTRY_SIZE {
			tmp := make([]byte, filesystem.CACHE_ENTRY_SIZE)
			copy(tmp, bytes)
			bytes = tmp
		}

		childN, err := w.Write(bytes)
		if err != nil {
			return -1, err
		}

		n += int64(childN)

		childN64, err := io.CopyN(w, r.r, hdr.Size)
		if err != nil {
			return -1, err
		}

		n += childN64
	}

	return
}

var (
	_ common.BuildResult = &tarToArchiveBuildResult{}
)

type ReadArchiveBuildDefinition struct {
	Base common.BuildDefinition
	Kind string
}

// NeedsBuild implements BuildDefinition.
func (r *ReadArchiveBuildDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	build, err := ctx.NeedsBuild(r.Base)
	if err != nil {
		return true, err
	}
	if build {
		return true, nil
	} else {
		return false, nil // archives don't need to be re-extracted unless the underlying file changes.
	}
}

// Build implements BuildDefinition.
func (r *ReadArchiveBuildDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	f, err := ctx.Database().Build(ctx, r.Base)
	if err != nil {
		return nil, err
	}

	fh, err := f.Open()
	if err != nil {
		return nil, err
	}

	if strings.HasSuffix(r.Kind, ".zip") {
		info, err := f.Stat()
		if err != nil {
			return nil, err
		}

		reader, err := zip.NewReader(fh, info.Size())
		if err != nil {
			return nil, err
		}

		return &zipToArchiveBuildResult{r: reader}, nil
	} else {
		kind := r.Kind

		var reader io.Reader

		if strings.HasSuffix(kind, ".gz") {
			reader, err = gzip.NewReader(fh)
			if err != nil {
				return nil, err
			}

			kind = strings.TrimSuffix(kind, ".gz")
		} else {
			reader = fh
		}

		if strings.HasSuffix(kind, ".tar") {
			return &tarToArchiveBuildResult{r: tar.NewReader(reader)}, nil
		} else {
			return nil, fmt.Errorf("ReadArchive with unknown kind: %s", r.Kind)
		}
	}
}

// Tag implements BuildDefinition.
func (r *ReadArchiveBuildDefinition) Tag() string {
	return strings.Join([]string{"ReadArchive", r.Base.Tag(), r.Kind}, "_")
}

func (def *ReadArchiveBuildDefinition) String() string { return def.Tag() }
func (*ReadArchiveBuildDefinition) Type() string       { return "FetchHttpBuildDefinition" }
func (*ReadArchiveBuildDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("FetchHttpBuildDefinition is not hashable")
}
func (*ReadArchiveBuildDefinition) Truth() starlark.Bool { return starlark.True }
func (*ReadArchiveBuildDefinition) Freeze()              {}

var (
	_ starlark.Value         = &ReadArchiveBuildDefinition{}
	_ common.BuildDefinition = &ReadArchiveBuildDefinition{}
)

func NewReadArchiveBuildDefinition(base common.BuildDefinition, kind string) *ReadArchiveBuildDefinition {
	return &ReadArchiveBuildDefinition{Base: base, Kind: kind}
}
