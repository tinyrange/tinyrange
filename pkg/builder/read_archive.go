package builder

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"go.starlark.net/starlark"
)

type directoryToArchiveBuildResult struct {
	dir filesystem.Directory
	off int64
}

func (d *directoryToArchiveBuildResult) writeEntry(w io.Writer, ent filesystem.File, name string) (n int64, err error) {
	info, err := ent.Stat()
	if err != nil {
		return
	}

	var typ filesystem.FileType

	// slog.Info("info", "name", name, "mode", info.Mode(), "isDir", info.Mode().IsDir(), "size", info.Size())

	var cacheEnt *filesystem.CacheEntry

	if cEnt, ok := ent.(*filesystem.CacheEntry); ok {
		cacheEnt = &filesystem.CacheEntry{
			COffset:   d.off + 1024,
			CTypeflag: cEnt.CTypeflag,
			CName:     name,
			CLinkname: cEnt.CLinkname,
			CSize:     int64(info.Size()),
			CMode:     int64(info.Mode()),
			CUid:      cEnt.CUid,
			CGid:      cEnt.CGid,
			CModTime:  info.ModTime().UnixMicro(),
			CDevmajor: 0,
			CDevminor: 0,
		}
	} else {
		var linkname = ""

		if info.Mode().Type() == fs.ModeSymlink {
			linkname, err = filesystem.GetLinkName(ent)
			if err != nil {
				return
			}
			typ = filesystem.TypeSymlink
		} else if info.Mode().IsDir() {
			typ = filesystem.TypeDirectory
		} else {
			typ = filesystem.TypeRegular
		}

		uid, gid, err := filesystem.GetUidAndGid(ent)
		if err != nil {
			return -1, err
		}

		cacheEnt = &filesystem.CacheEntry{
			COffset:   d.off + 1024,
			CTypeflag: typ,
			CName:     name,
			CLinkname: linkname,
			CSize:     int64(info.Size()),
			CMode:     int64(info.Mode()),
			CUid:      uid,
			CGid:      gid,
			CModTime:  info.ModTime().UnixMicro(),
			CDevmajor: 0,
			CDevminor: 0,
		}
	}

	// slog.Info("archive", "ent", cacheEnt)

	bytes, err := json.Marshal(&cacheEnt)
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
	d.off += int64(childN)

	return
}

func (d *directoryToArchiveBuildResult) writeFileTo(w io.Writer, ent filesystem.File, name string) (n int64, err error) {
	if cEnt, ok := ent.(*filesystem.CacheEntry); ok {
		if cEnt.CTypeflag != filesystem.TypeRegular {
			return d.writeEntry(w, ent, name)
		}
	}

	n, err = d.writeEntry(w, ent, name)
	if err != nil {
		return
	}

	contents, err := ent.Open()
	if err != nil {
		return
	}

	childN, err := io.Copy(w, contents)
	if err != nil {
		return n + childN, err
	}

	n += childN
	d.off += childN

	return
}

func (d *directoryToArchiveBuildResult) writeDirTo(w io.Writer, ent filesystem.Directory, name string) (n int64, err error) {
	n, err = d.writeEntry(w, ent, name)
	if err != nil {
		return
	}

	ents, err := ent.Readdir()
	if err != nil {
		return 0, err
	}

	for _, ent := range ents {
		if dir, ok := ent.File.(filesystem.Directory); ok {
			childN, err := d.writeDirTo(w, dir, path.Join(name, ent.Name))
			if err != nil {
				return n + childN, err
			}

			n += childN
		} else {
			childN, err := d.writeFileTo(w, ent.File, path.Join(name, ent.Name))
			if err != nil {
				return n + childN, err
			}

			n += childN
		}
	}

	return
}

// WriteTo implements common.BuildResult.
func (d *directoryToArchiveBuildResult) WriteTo(w io.Writer) (n int64, err error) {
	return d.writeDirTo(w, d.dir, "")
}

func (*directoryToArchiveBuildResult) String() string { return "directoryToArchiveBuildResult" }
func (*directoryToArchiveBuildResult) Type() string   { return "directoryToArchiveBuildResult" }
func (*directoryToArchiveBuildResult) Hash() (uint32, error) {
	return 0, fmt.Errorf("directoryToArchiveBuildResult is not hashable")
}
func (*directoryToArchiveBuildResult) Truth() starlark.Bool { return starlark.True }
func (*directoryToArchiveBuildResult) Freeze()              {}

var (
	_ starlark.Value     = &directoryToArchiveBuildResult{}
	_ common.BuildResult = &directoryToArchiveBuildResult{}
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
		case tar.TypeXGlobalHeader:
			continue
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
	f, err := ctx.BuildChild(r.Base)
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
		} else if strings.HasSuffix(kind, ".zst") {
			reader, err = zstd.NewReader(fh)
			if err != nil {
				return nil, err
			}

			kind = strings.TrimSuffix(kind, ".zst")
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
