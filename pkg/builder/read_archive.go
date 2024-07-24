package builder

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/blakesmith/ar"
	"github.com/cavaliergopher/cpio"
	"github.com/klauspost/compress/zstd"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/xi2/xz"
	"go.starlark.net/starlark"
)

type directoryToArchiveBuildResult struct {
	dir common.Directory
	off int64
}

func (d *directoryToArchiveBuildResult) writeEntry(w io.Writer, ent common.File, name string) (n int64, err error) {
	info, err := ent.Stat()
	if err != nil {
		return
	}

	var typ common.FileType

	// slog.Info("info", "name", name, "mode", info.Mode(), "isDir", info.Mode().IsDir(), "size", info.Size())

	var cacheEnt *common.CacheEntry

	if cEnt, ok := ent.(*common.CacheEntry); ok {
		cacheEnt = &common.CacheEntry{
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
			linkname, err = common.GetLinkName(ent)
			if err != nil {
				return
			}
			typ = common.TypeSymlink
		} else if info.Mode().IsDir() {
			typ = common.TypeDirectory
		} else {
			typ = common.TypeRegular
		}

		uid, gid, err := common.GetUidAndGid(ent)
		if err != nil {
			return -1, err
		}

		cacheEnt = &common.CacheEntry{
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

	if len(bytes) > common.CACHE_ENTRY_SIZE {
		return -1, fmt.Errorf("oversized entry header: %d > %d", len(bytes), common.CACHE_ENTRY_SIZE)
	} else if len(bytes) < common.CACHE_ENTRY_SIZE {
		tmp := make([]byte, common.CACHE_ENTRY_SIZE)
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

func (d *directoryToArchiveBuildResult) writeFileTo(w io.Writer, ent common.File, name string) (n int64, err error) {
	if starEnt, ok := ent.(*common.StarFile); ok {
		ent = starEnt.File
	}

	if cEnt, ok := ent.(*common.CacheEntry); ok {
		if cEnt.CTypeflag != common.TypeRegular {
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

func (d *directoryToArchiveBuildResult) writeDirTo(w io.Writer, ent common.Directory, name string) (n int64, err error) {
	n, err = d.writeEntry(w, ent, name)
	if err != nil {
		return
	}

	ents, err := ent.Readdir()
	if err != nil {
		return 0, err
	}

	for _, ent := range ents {
		if dir, ok := ent.File.(common.Directory); ok {
			childN, err := d.writeDirTo(w, dir, path.Join(name, ent.Name))
			if err != nil {
				return n + childN, fmt.Errorf("failed to write directory %s: %s", ent.Name, err)
			}

			n += childN
		} else {
			childN, err := d.writeFileTo(w, ent.File, path.Join(name, ent.Name))
			if err != nil {
				return n + childN, fmt.Errorf("failed to write file %s: %s", ent.Name, err)
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
		var typ common.FileType

		if file.Mode().IsDir() {
			typ = common.TypeDirectory
		} else {
			typ = common.TypeRegular
		}

		ent := &common.CacheEntry{
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

		if len(bytes) > common.CACHE_ENTRY_SIZE {
			return -1, fmt.Errorf("oversized entry header: %d > %d", len(bytes), common.CACHE_ENTRY_SIZE)
		} else if len(bytes) < common.CACHE_ENTRY_SIZE {
			tmp := make([]byte, common.CACHE_ENTRY_SIZE)
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

		var typeFlag = common.TypeRegular

		switch hdr.Typeflag {
		case tar.TypeReg:
			// pass
		case tar.TypeDir:
			typeFlag = common.TypeDirectory
		case tar.TypeSymlink:
			typeFlag = common.TypeSymlink
		case tar.TypeLink:
			typeFlag = common.TypeLink
		case tar.TypeXGlobalHeader:
			continue
		default:
			return -1, fmt.Errorf("unknown type flag: %d", hdr.Typeflag)
		}

		ent := &common.CacheEntry{
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

		if len(bytes) > common.CACHE_ENTRY_SIZE {
			return -1, fmt.Errorf("oversized entry header: %d > %d", len(bytes), common.CACHE_ENTRY_SIZE)
		} else if len(bytes) < common.CACHE_ENTRY_SIZE {
			tmp := make([]byte, common.CACHE_ENTRY_SIZE)
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

type cpioToArchiveBuildResult struct {
	r *cpio.Reader
}

// WriteTo implements common.BuildResult.
func (c *cpioToArchiveBuildResult) WriteTo(w io.Writer) (n int64, err error) {
	for {
		hdr, err := c.r.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return -1, err
		}

		fileInfo := hdr.FileInfo()

		var typeFlag = common.TypeRegular

		typ := hdr.Mode &^ cpio.ModePerm

		switch true {
		case typ&cpio.TypeReg != 0:
			// pass
		case typ&cpio.TypeDir != 0:
			typeFlag = common.TypeDirectory
		case typ&cpio.TypeSymlink != 0:
			typeFlag = common.TypeSymlink
		default:
			return -1, fmt.Errorf("unknown type flag: %d", typ)
		}

		ent := &common.CacheEntry{
			COffset:   n + 1024,
			CTypeflag: typeFlag,
			CName:     hdr.Name,
			CLinkname: hdr.Linkname,
			CSize:     hdr.Size,
			CMode:     int64(fileInfo.Mode()),
			CUid:      hdr.Uid,
			CGid:      hdr.Guid,
			CModTime:  hdr.ModTime.UnixMicro(),
		}

		bytes, err := json.Marshal(&ent)
		if err != nil {
			return -1, err
		}

		if len(bytes) > common.CACHE_ENTRY_SIZE {
			return -1, fmt.Errorf("oversized entry header: %d > %d", len(bytes), common.CACHE_ENTRY_SIZE)
		} else if len(bytes) < common.CACHE_ENTRY_SIZE {
			tmp := make([]byte, common.CACHE_ENTRY_SIZE)
			copy(tmp, bytes)
			bytes = tmp
		}

		childN, err := w.Write(bytes)
		if err != nil {
			return -1, err
		}

		n += int64(childN)

		childN64, err := io.CopyN(w, c.r, hdr.Size)
		if err != nil {
			return -1, err
		}

		n += childN64
	}

	return
}

var (
	_ common.BuildResult = &cpioToArchiveBuildResult{}
)

type arToArchiveBuildResult struct {
	r *ar.Reader
}

// WriteTo implements common.BuildResult.
func (c *arToArchiveBuildResult) WriteTo(w io.Writer) (n int64, err error) {
	for {
		hdr, err := c.r.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return -1, err
		}

		var typeFlag = common.TypeRegular

		ent := &common.CacheEntry{
			COffset:   n + 1024,
			CTypeflag: typeFlag,
			CName:     hdr.Name,
			CSize:     hdr.Size,
			CMode:     hdr.Mode,
			CUid:      hdr.Uid,
			CGid:      hdr.Gid,
			CModTime:  hdr.ModTime.UnixMicro(),
		}

		bytes, err := json.Marshal(&ent)
		if err != nil {
			return -1, err
		}

		if len(bytes) > common.CACHE_ENTRY_SIZE {
			return -1, fmt.Errorf("oversized entry header: %d > %d", len(bytes), common.CACHE_ENTRY_SIZE)
		} else if len(bytes) < common.CACHE_ENTRY_SIZE {
			tmp := make([]byte, common.CACHE_ENTRY_SIZE)
			copy(tmp, bytes)
			bytes = tmp
		}

		childN, err := w.Write(bytes)
		if err != nil {
			return -1, err
		}

		n += int64(childN)

		childN64, err := io.CopyN(w, c.r, hdr.Size)
		if err != nil {
			return -1, err
		}

		n += childN64
	}

	return
}

var (
	_ common.BuildResult = &arToArchiveBuildResult{}
)

type ReadArchiveBuildDefinition struct {
	Base common.BuildDefinition
	Kind string
}

// Create implements common.BuildDefinition.
func (r *ReadArchiveBuildDefinition) Create(params common.BuildDefinitionParameters) common.BuildDefinition {
	panic("unimplemented")
}

// Params implements common.BuildDefinition.
func (r *ReadArchiveBuildDefinition) Params() common.BuildDefinitionParameters {
	panic("unimplemented")
}

// NeedsBuild implements BuildDefinition.
func (r *ReadArchiveBuildDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	build, err := ctx.NeedsBuild(r.Base)
	if err != nil {
		return true, err
	}
	if build {
		slog.Info("rebuild", "base", r.Base)
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
		} else if strings.HasSuffix(kind, ".xz") {
			reader, err = xz.NewReader(fh, xz.DefaultDictMax)
			if err != nil {
				return nil, err
			}

			kind = strings.TrimSuffix(kind, ".xz")
		} else {
			reader = fh
		}

		if strings.HasSuffix(kind, ".tar") {
			return &tarToArchiveBuildResult{r: tar.NewReader(reader)}, nil
		} else if strings.HasSuffix(kind, ".cpio") {
			return &cpioToArchiveBuildResult{r: cpio.NewReader(reader)}, nil
		} else if strings.HasPrefix(kind, ".ar") {
			return &arToArchiveBuildResult{r: ar.NewReader(reader)}, nil
		} else {
			return nil, fmt.Errorf("ReadArchive with unknown kind: %s", r.Kind)
		}
	}
}

func (def *ReadArchiveBuildDefinition) String() string { return "ReadArchiveBuildDefinition" }
func (*ReadArchiveBuildDefinition) Type() string       { return "ReadArchiveBuildDefinition" }
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
