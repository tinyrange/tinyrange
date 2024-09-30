package builder

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/blakesmith/ar"
	"github.com/cavaliergopher/cpio"
	"github.com/klauspost/compress/zstd"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/xi2/xz"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&ReadArchiveBuildDefinition{})
}

type directoryToArchiveBuildResult struct {
	dir filesystem.Directory
	w   *filesystem.ArchiveWriter
}

func (d *directoryToArchiveBuildResult) getEntry(
	ent filesystem.File,
	name string,
) (*filesystem.CacheEntry, error) {
	info, err := ent.Stat()
	if err != nil {
		return nil, err
	}

	var typ filesystem.FileType

	// slog.Info("info", "name", name, "mode", info.Mode(), "isDir", info.Mode().IsDir(), "size", info.Size())

	var cacheEnt *filesystem.CacheEntry

	if cEnt, ok := ent.(*filesystem.CacheEntry); ok {
		cacheEnt = &filesystem.CacheEntry{
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
				return nil, err
			}
			typ = filesystem.TypeSymlink
		} else if info.Mode().IsDir() {
			typ = filesystem.TypeDirectory
		} else {
			typ = filesystem.TypeRegular
		}

		uid, gid, err := filesystem.GetUidAndGid(ent)
		if err != nil {
			return nil, err
		}

		cacheEnt = &filesystem.CacheEntry{
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

	return cacheEnt, nil
}

func (d *directoryToArchiveBuildResult) writeFileTo(ent filesystem.File, name string) error {
	if starEnt, ok := ent.(*filesystem.StarFile); ok {
		ent = starEnt.File
	}

	if cEnt, ok := ent.(*filesystem.CacheEntry); ok {
		if cEnt.CTypeflag != filesystem.TypeRegular {
			cache, err := d.getEntry(ent, name)
			if err != nil {
				return err
			}

			return d.w.WriteEntry(cache, nil)
		}
	}

	contents, err := ent.Open()
	if err != nil {
		return err
	}

	cache, err := d.getEntry(ent, name)
	if err != nil {
		return err
	}

	if err := d.w.WriteEntry(cache, contents); err != nil {
		return err
	}

	return nil
}

func (d *directoryToArchiveBuildResult) writeDirTo(ent filesystem.Directory, name string) error {
	entry, err := d.getEntry(ent, name)
	if err != nil {
		return err
	}

	if err := d.w.WriteEntry(entry, nil); err != nil {
		return err
	}

	ents, err := ent.Readdir()
	if err != nil {
		return err
	}

	for _, ent := range ents {
		if dir, ok := ent.File.(filesystem.Directory); ok {
			err := d.writeDirTo(dir, path.Join(name, ent.Name))
			if err != nil {
				return fmt.Errorf("failed to write directory %s: %s", ent.Name, err)
			}
		} else {
			err := d.writeFileTo(ent.File, path.Join(name, ent.Name))
			if err != nil {
				return fmt.Errorf("failed to write file %s: %s", ent.Name, err)
			}
		}
	}

	return nil
}

// WriteTo implements common.BuildResult.
func (d *directoryToArchiveBuildResult) WriteResult(w io.Writer) error {
	d.w = filesystem.NewArchiveWriter(w)

	return d.writeDirTo(d.dir, "")
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
func (z *zipToArchiveBuildResult) WriteResult(w io.Writer) error {
	ark := filesystem.NewArchiveWriter(w)

	for _, file := range z.r.File {
		var typ filesystem.FileType

		if file.Mode().IsDir() {
			typ = filesystem.TypeDirectory
		} else {
			typ = filesystem.TypeRegular
		}

		fh, err := file.Open()
		if err != nil {
			return err
		}

		if err := ark.WriteEntry(&filesystem.CacheEntry{
			CTypeflag: typ,
			CName:     file.Name,
			CSize:     int64(file.UncompressedSize64),
			CMode:     int64(file.Mode()),
			CModTime:  file.Modified.UnixMicro(),
		}, fh); err != nil {
			return err
		}
	}

	return nil
}

var (
	_ common.BuildResult = &zipToArchiveBuildResult{}
)

type tarToArchiveBuildResult struct {
	r *tar.Reader
}

// WriteTo implements common.BuildResult.
func (r *tarToArchiveBuildResult) WriteResult(w io.Writer) error {
	ark := filesystem.NewArchiveWriter(w)

	for {
		hdr, err := r.r.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		info := hdr.FileInfo()

		var typeFlag filesystem.FileType

		switch hdr.Typeflag {
		case tar.TypeReg:
			typeFlag = filesystem.TypeRegular
		case tar.TypeDir:
			typeFlag = filesystem.TypeDirectory
		case tar.TypeSymlink:
			typeFlag = filesystem.TypeSymlink
		case tar.TypeLink:
			typeFlag = filesystem.TypeLink
		case tar.TypeXGlobalHeader:
			continue
		default:
			return fmt.Errorf("unknown type flag: %d", hdr.Typeflag)
		}

		if err := ark.WriteEntry(&filesystem.CacheEntry{
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
		}, r.r); err != nil {
			return err
		}
	}

	return nil
}

var (
	_ common.BuildResult = &tarToArchiveBuildResult{}
)

type cpioToArchiveBuildResult struct {
	r *cpio.Reader
}

// WriteTo implements common.BuildResult.
func (c *cpioToArchiveBuildResult) WriteResult(w io.Writer) error {
	ark := filesystem.NewArchiveWriter(w)

	for {
		hdr, err := c.r.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		fileInfo := hdr.FileInfo()

		var typeFlag = filesystem.TypeRegular

		typ := hdr.Mode &^ cpio.ModePerm

		switch true {
		case typ&cpio.TypeReg != 0:
			// pass
		case typ&cpio.TypeDir != 0:
			typeFlag = filesystem.TypeDirectory
		case typ&cpio.TypeSymlink != 0:
			typeFlag = filesystem.TypeSymlink
		default:
			return fmt.Errorf("unknown type flag: %d", typ)
		}

		if err := ark.WriteEntry(&filesystem.CacheEntry{
			CTypeflag: typeFlag,
			CName:     hdr.Name,
			CLinkname: hdr.Linkname,
			CSize:     hdr.Size,
			CMode:     int64(fileInfo.Mode()),
			CUid:      hdr.Uid,
			CGid:      hdr.Guid,
			CModTime:  hdr.ModTime.UnixMicro(),
		}, c.r); err != nil {
			return err
		}
	}

	return nil
}

var (
	_ common.BuildResult = &cpioToArchiveBuildResult{}
)

type arToArchiveBuildResult struct {
	r *ar.Reader
}

// WriteTo implements common.BuildResult.
func (c *arToArchiveBuildResult) WriteResult(w io.Writer) error {
	ark := filesystem.NewArchiveWriter(w)

	for {
		hdr, err := c.r.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		var typeFlag = filesystem.TypeRegular

		if err := ark.WriteEntry(&filesystem.CacheEntry{
			CTypeflag: typeFlag,
			CName:     hdr.Name,
			CSize:     hdr.Size,
			CMode:     hdr.Mode,
			CUid:      hdr.Uid,
			CGid:      hdr.Gid,
			CModTime:  hdr.ModTime.UnixMicro(),
		}, c.r); err != nil {
			return err
		}
	}

	return nil
}

var (
	_ common.BuildResult = &arToArchiveBuildResult{}
)

func ReadArchiveSupportsExtracting(kind string) bool {
	if strings.HasSuffix(kind, ".zip") {
		return true
	}

	if strings.HasSuffix(kind, ".gz") {
		kind = strings.TrimSuffix(kind, ".gz")
	} else if strings.HasSuffix(kind, ".zst") {
		kind = strings.TrimSuffix(kind, ".zst")
	} else if strings.HasSuffix(kind, ".xz") {
		kind = strings.TrimSuffix(kind, ".xz")
	}

	if strings.HasSuffix(kind, ".tar") {
		return true
	} else if strings.HasSuffix(kind, ".cpio") {
		return true
	} else if strings.HasSuffix(kind, ".ar") {
		return true
	} else {
		return false
	}
}

type ReadArchiveBuildDefinition struct {
	params ReadArchiveParameters
}

// Dependencies implements common.BuildDefinition.
func (def *ReadArchiveBuildDefinition) Dependencies(ctx common.BuildContext) ([]common.DependencyNode, error) {
	if def.params.Base != nil {
		return []common.DependencyNode{def.params.Base}, nil
	} else {
		return []common.DependencyNode{}, nil
	}
}

// implements common.BuildDefinition.
func (def *ReadArchiveBuildDefinition) Params() hash.SerializableValue { return def.params }
func (def *ReadArchiveBuildDefinition) SerializableType() string       { return "ReadArchiveBuildDefinition" }
func (def *ReadArchiveBuildDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &ReadArchiveBuildDefinition{params: params.(ReadArchiveParameters)}
}

// AsFragments implements common.Directive.
func (r *ReadArchiveBuildDefinition) AsFragments(ctx common.BuildContext, special common.SpecialDirectiveHandlers) ([]config.Fragment, error) {
	res, err := ctx.BuildChild(r)
	if err != nil {
		return nil, err
	}

	digest := res.Digest()

	filename, err := ctx.FilenameFromDigest(digest)
	if err != nil {
		return nil, err
	}

	return []config.Fragment{
		{Archive: &config.ArchiveFragment{HostFilename: filename}},
	}, nil
}

// ToStarlark implements common.BuildDefinition.
func (r *ReadArchiveBuildDefinition) ToStarlark(ctx common.BuildContext, result filesystem.File) (starlark.Value, error) {
	ark, err := filesystem.ReadArchiveFromFile(result)
	if err != nil {
		return starlark.None, err
	}

	return filesystem.NewStarArchive(ark, r.Tag()), nil
}

// NeedsBuild implements BuildDefinition.
func (r *ReadArchiveBuildDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	build, err := ctx.NeedsBuild(r.params.Base)
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
	f, err := ctx.BuildChild(r.params.Base)
	if err != nil {
		return nil, err
	}

	fh, err := f.Open()
	if err != nil {
		return nil, err
	}

	if strings.HasSuffix(r.params.Kind, ".zip") {
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
		kind := r.params.Kind

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
		} else if strings.HasSuffix(kind, ".ar") {
			return &arToArchiveBuildResult{r: ar.NewReader(reader)}, nil
		} else {
			return nil, fmt.Errorf("ReadArchive with unknown kind: %s", r.params.Kind)
		}
	}
}

// Tag implements BuildDefinition.
func (r *ReadArchiveBuildDefinition) Tag() string {
	return strings.Join([]string{"ReadArchive", r.params.Base.Tag(), r.params.Kind}, "_")
}

func (def *ReadArchiveBuildDefinition) String() string { return def.Tag() }
func (*ReadArchiveBuildDefinition) Type() string       { return "ReadArchiveBuildDefinition" }
func (*ReadArchiveBuildDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("ReadArchiveBuildDefinition is not hashable")
}
func (*ReadArchiveBuildDefinition) Truth() starlark.Bool { return starlark.True }
func (*ReadArchiveBuildDefinition) Freeze()              {}

var (
	_ starlark.Value         = &ReadArchiveBuildDefinition{}
	_ common.BuildDefinition = &ReadArchiveBuildDefinition{}
	_ common.Directive       = &ReadArchiveBuildDefinition{}
)

func NewReadArchiveBuildDefinition(base common.BuildDefinition, kind string) *ReadArchiveBuildDefinition {
	return &ReadArchiveBuildDefinition{params: ReadArchiveParameters{Base: base, Kind: kind}}
}
