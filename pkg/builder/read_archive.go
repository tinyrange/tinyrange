package builder

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/blakesmith/ar"
	"github.com/cavaliergopher/cpio"
	"github.com/klauspost/compress/zstd"
	"github.com/tinyrange/tinyrange/pkg/archive"
	"github.com/tinyrange/tinyrange/pkg/archive2"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/star"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/path"
	"github.com/xi2/xz"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&readArchiveBuildDefinition{})
	hash.RegisterType(&readArchive2BuildDefinition{})
}

type archiveConverter interface {
	common.BuildResult

	WriteArchive2(w *archive2.ArchiveWriter) error
}

type directoryToArchiveBuildResult struct {
	dir filesystem.Directory
	w   *archive.ArchiveWriter
}

func (d *directoryToArchiveBuildResult) getEntry(
	file filesystem.File,
	name string,
) (filesystem.Entry, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	var typ filesystem.FileType

	// log.Info("info", "name", name, "mode", info.Mode(), "isDir", info.Mode().IsDir(), "size", info.Size())

	var cacheEnt filesystem.Entry

	if cEnt, ok := file.(filesystem.Entry); ok {
		cacheEnt = archive.NewEntryBuilder().
			CloneFrom(cEnt).
			Name(name).
			Build()
	} else {
		var linkname = ""

		if info.Mode().Type() == fs.ModeSymlink {
			linkname, err = filesystem.GetLinkName(file)
			if err != nil {
				return nil, err
			}
			typ = filesystem.TypeSymlink
		} else if info.Mode().IsDir() {
			typ = filesystem.TypeDirectory
		} else {
			typ = filesystem.TypeRegular
		}

		uid, gid, err := filesystem.GetUidAndGid(file)
		if err != nil {
			return nil, err
		}

		cacheEnt = archive.NewEntryBuilder().
			Typeflag(typ).
			Name(name).
			Linkname(linkname).
			Size(info.Size()).
			Mode(info.Mode()).
			UidAndGid(uid, gid).
			ModTime(info.ModTime()).
			Device(0, 0).Build()
	}

	// log.Info("archive", "ent", cacheEnt.Name())

	return cacheEnt, nil
}

func (d *directoryToArchiveBuildResult) writeFileTo(ent filesystem.File, name string) error {
	if starEnt, ok := ent.(*star.StarFile); ok {
		ent = starEnt.File
	}

	if cEnt, ok := ent.(filesystem.Entry); ok {
		if cEnt.Typeflag() != filesystem.TypeRegular {
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
			err := d.writeDirTo(dir, path.Unix.Join(name, ent.Name))
			if err != nil {
				return fmt.Errorf("failed to write directory %s: %s", ent.Name, err)
			}
		} else {
			err := d.writeFileTo(ent.File, path.Unix.Join(name, ent.Name))
			if err != nil {
				return fmt.Errorf("failed to write file %s: %s", ent.Name, err)
			}
		}
	}

	return nil
}

// WriteTo implements common.BuildResult.
func (d *directoryToArchiveBuildResult) WriteResult(w io.Writer) error {
	d.w = archive.NewArchiveWriter(w)

	return d.writeDirTo(d.dir, "")
}

// WriteArchive2 implements archiveConverter.
func (d *directoryToArchiveBuildResult) WriteArchive2(w *archive2.ArchiveWriter) error {
	return fmt.Errorf("archive2 unimplemented for directoryToArchive")
}

func (*directoryToArchiveBuildResult) String() string { return "directoryToArchiveBuildResult" }
func (*directoryToArchiveBuildResult) Type() string   { return "directoryToArchiveBuildResult" }
func (*directoryToArchiveBuildResult) Hash() (uint32, error) {
	return 0, fmt.Errorf("directoryToArchiveBuildResult is not hashable")
}
func (*directoryToArchiveBuildResult) Truth() starlark.Bool { return starlark.True }
func (*directoryToArchiveBuildResult) Freeze()              {}

var (
	_ starlark.Value   = &directoryToArchiveBuildResult{}
	_ archiveConverter = &directoryToArchiveBuildResult{}
)

type StarBuildResult interface {
	starlark.Value
	common.BuildResult
}

func NewDirectoryToArchiveBuildResult(dir filesystem.Directory) StarBuildResult {
	return &directoryToArchiveBuildResult{dir: dir}
}

type zipToArchiveBuildResult struct {
	r *zip.Reader
}

// WriteTo implements common.BuildResult.
func (z *zipToArchiveBuildResult) WriteResult(w io.Writer) error {
	ark := archive.NewArchiveWriter(w)

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

		if err := ark.WriteEntry(archive.NewEntryBuilder().
			Typeflag(typ).
			Name(file.Name).
			Size(int64(file.UncompressedSize64)).
			Mode(file.Mode()).
			ModTime(file.Modified).Build(), fh); err != nil {
			return err
		}
	}

	return nil
}

// WriteArchive2 implements archiveConverter.
func (z *zipToArchiveBuildResult) WriteArchive2(w *archive2.ArchiveWriter) error {
	return fmt.Errorf("archive2 unimplemented for zip")
}

var (
	_ archiveConverter = &zipToArchiveBuildResult{}
)

type tarToArchiveBuildResult struct {
	r               *tar.Reader
	oci             bool
	stripComponents int
}

func stripComponents(name string, strip int) string {
	if strip <= 0 {
		return name
	}

	components := strings.Split(name, "/")

	if len(components) <= strip {
		return ""
	}

	return strings.Join(components[strip:], "/")
}

// WriteTo implements common.BuildResult.
func (r *tarToArchiveBuildResult) WriteResult(w io.Writer) error {
	ark := archive.NewArchiveWriter(w)

	for {
		hdr, err := r.r.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		deleted := false

		if r.oci {
			if path.Unix.Base(hdr.Name) == ".wh..wh..opq" {
				deleted = true
				hdr.Name = path.Unix.Dir(hdr.Name)
			} else if strings.HasPrefix(path.Unix.Base(hdr.Name), ".wh.") {
				deleted = true
				hdr.Name = path.Unix.Join(path.Unix.Dir(hdr.Name), path.Unix.Base(hdr.Name)[4:])
			}
		}

		if r.stripComponents > 0 {
			hdr.Name = stripComponents(hdr.Name, r.stripComponents)
			if hdr.Name == "" {
				continue
			}
		}

		info := hdr.FileInfo()

		var typeFlag filesystem.FileType

		switch hdr.Typeflag {
		case tar.TypeReg:
			typeFlag = filesystem.TypeRegular
		case tar.TypeDir:
			typeFlag = filesystem.TypeDirectory
		case tar.TypeChar:
			// TODO(joshua): Handle character devices.
			continue
		case tar.TypeBlock:
			// TODO(joshua): Handle block devices.
			continue
		case tar.TypeSymlink:
			typeFlag = filesystem.TypeSymlink
		case tar.TypeLink:
			typeFlag = filesystem.TypeLink
		case tar.TypeXGlobalHeader:
			continue
		default:
			return fmt.Errorf("unknown type flag: %d", hdr.Typeflag)
		}

		if deleted {
			typeFlag = filesystem.TypeDeleted
		}

		if err := ark.WriteEntry(archive.NewEntryBuilder().
			Typeflag(typeFlag).
			Name(hdr.Name).
			Linkname(hdr.Linkname).
			Size(hdr.Size).
			Mode(info.Mode()).
			UidAndGid(hdr.Uid, hdr.Gid).
			ModTime(hdr.ModTime).
			Device(hdr.Devmajor, hdr.Devminor).Build(), r.r); err != nil {
			return err
		}
	}

	return nil
}

// WriteArchive2 implements archiveConverter.
func (r *tarToArchiveBuildResult) WriteArchive2(w *archive2.ArchiveWriter) error {
	for {
		hdr, err := r.r.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		deleted := false

		if r.oci {
			if path.Unix.Base(hdr.Name) == ".wh..wh..opq" {
				deleted = true
				hdr.Name = path.Unix.Dir(hdr.Name)
			} else if strings.HasPrefix(path.Unix.Base(hdr.Name), ".wh.") {
				deleted = true
				hdr.Name = path.Unix.Join(path.Unix.Dir(hdr.Name), path.Unix.Base(hdr.Name)[4:])
			}
		}

		if r.stripComponents > 0 {
			hdr.Name = stripComponents(hdr.Name, r.stripComponents)
			if hdr.Name == "" {
				continue
			}
		}

		info := hdr.FileInfo()

		var typeFlag archive2.EntryKind

		switch hdr.Typeflag {
		case tar.TypeReg:
			typeFlag = archive2.EntryKindRegular
		case tar.TypeDir:
			typeFlag = archive2.EntryKindDirectory
		case tar.TypeChar:
			// TODO(joshua): Handle character devices.
			continue
		case tar.TypeBlock:
			// TODO(joshua): Handle block devices.
			continue
		case tar.TypeSymlink:
			typeFlag = archive2.EntryKindSymlink
		case tar.TypeLink:
			typeFlag = archive2.EntryKindHardlink
		case tar.TypeXGlobalHeader:
			continue
		default:
			return fmt.Errorf("unknown type flag: %d", hdr.Typeflag)
		}

		if deleted {
			typeFlag = archive2.EntryKindDeleted
		}

		var fact archive2.EntryFactory

		if err := w.WriteEntry(fact.
			Kind(typeFlag).
			Name(hdr.Name).
			Linkname(hdr.Linkname).
			Size(hdr.Size).
			Mode(info.Mode()).
			Owner(hdr.Uid, hdr.Gid).
			ModTime(hdr.ModTime), r.r); err != nil {
			return err
		}
	}

	return nil
}

var (
	_ archiveConverter = &tarToArchiveBuildResult{}
)

type cpioToArchiveBuildResult struct {
	r *cpio.Reader
}

// WriteTo implements common.BuildResult.
func (c *cpioToArchiveBuildResult) WriteResult(w io.Writer) error {
	ark := archive.NewArchiveWriter(w)

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

		if err := ark.WriteEntry(archive.NewEntryBuilder().
			Typeflag(typeFlag).
			Name(hdr.Name).
			Linkname(hdr.Linkname).
			Size(hdr.Size).
			Mode(fileInfo.Mode()).
			UidAndGid(hdr.Uid, hdr.Guid).
			ModTime(hdr.ModTime).Build(), c.r); err != nil {
			return err
		}
	}

	return nil
}

// WriteArchive2 implements archiveConverter.
func (c *cpioToArchiveBuildResult) WriteArchive2(w *archive2.ArchiveWriter) error {
	return fmt.Errorf("archive2 unimplemented for cpio")
}

var (
	_ archiveConverter = &cpioToArchiveBuildResult{}
)

type arToArchiveBuildResult struct {
	r *ar.Reader
}

// WriteTo implements common.BuildResult.
func (c *arToArchiveBuildResult) WriteResult(w io.Writer) error {
	ark := archive.NewArchiveWriter(w)

	for {
		hdr, err := c.r.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		var typeFlag = filesystem.TypeRegular

		if err := ark.WriteEntry(archive.NewEntryBuilder().
			Typeflag(typeFlag).
			Name(hdr.Name).
			Size(hdr.Size).
			Mode(fs.FileMode(hdr.Mode)).
			UidAndGid(hdr.Uid, hdr.Gid).
			ModTime(hdr.ModTime).Build(), c.r); err != nil {
			return err
		}
	}

	return nil
}

// WriteArchive2 implements archiveConverter.
func (c *arToArchiveBuildResult) WriteArchive2(w *archive2.ArchiveWriter) error {
	panic("unimplemented")
}

var (
	_ archiveConverter = &arToArchiveBuildResult{}
)

func ReadArchiveSupportsExtracting(kind string, archive2 bool) (string, bool) {
	if strings.HasSuffix(kind, ".zip") {
		return ".zip", !archive2
	}

	var compression = ""

	if strings.HasSuffix(kind, ".gz") {
		kind = strings.TrimSuffix(kind, ".gz")
		compression = ".gz"
	} else if strings.HasSuffix(kind, ".zst") {
		kind = strings.TrimSuffix(kind, ".zst")
		compression = ".zst"
	} else if strings.HasSuffix(kind, ".xz") {
		kind = strings.TrimSuffix(kind, ".xz")
		compression = ".xz"
	}

	if strings.HasSuffix(kind, ".tar") {
		return ".tar" + compression, true
	} else if strings.HasSuffix(kind, ".cpio") {
		return ".cpio" + compression, !archive2
	} else if strings.HasSuffix(kind, ".ar") {
		return ".ar" + compression, !archive2
	} else {
		return "", false
	}
}

func getConverterForFile(file filesystem.File, kind string, stripComponents int) (archiveConverter, error) {
	fh, err := file.Open()
	if err != nil {
		return nil, err
	}

	if strings.HasSuffix(kind, ".zip") {
		info, err := file.Stat()
		if err != nil {
			return nil, err
		}

		reader, err := zip.NewReader(fh, info.Size())
		if err != nil {
			return nil, err
		}

		if stripComponents > 0 {
			return nil, fmt.Errorf("zip archives do not support stripping components")
		}

		return &zipToArchiveBuildResult{r: reader}, nil
	} else {
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
			return &tarToArchiveBuildResult{r: tar.NewReader(reader), stripComponents: stripComponents}, nil
		} else if strings.HasSuffix(kind, ".tar$oci") {
			return &tarToArchiveBuildResult{r: tar.NewReader(reader), stripComponents: stripComponents, oci: true}, nil
		} else if strings.HasSuffix(kind, ".cpio") {
			if stripComponents > 0 {
				return nil, fmt.Errorf("cpio archives do not support stripping components")
			}

			return &cpioToArchiveBuildResult{r: cpio.NewReader(reader)}, nil
		} else if strings.HasSuffix(kind, ".ar") {
			if stripComponents > 0 {
				return nil, fmt.Errorf("ar archives do not support stripping components")
			}

			return &arToArchiveBuildResult{r: ar.NewReader(reader)}, nil
		} else {
			return nil, fmt.Errorf("ReadArchive with unknown kind: %s", kind)
		}
	}
}

type readArchiveBuildDefinition struct {
	params ReadArchiveParameters
}

// Dependencies implements common.BuildDefinition.
func (def *readArchiveBuildDefinition) Dependencies() ([]common.BuildDefinition, error) {
	return []common.BuildDefinition{def.params.Base}, nil
}

// implements common.BuildDefinition.
func (def *readArchiveBuildDefinition) Params() hash.SerializableValue { return def.params }
func (def *readArchiveBuildDefinition) SerializableType() string       { return "ReadArchiveBuildDefinition" }
func (def *readArchiveBuildDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &readArchiveBuildDefinition{params: params.(ReadArchiveParameters)}
}

// AsFragments implements common.Directive.
func (r *readArchiveBuildDefinition) AsFragments(ctx common.BuildContext, special common.SpecialDirectiveHandlers) ([]config.Fragment, error) {
	art, err := ctx.BuildChild(r)
	if err != nil {
		return nil, err
	}

	res, err := art.ReferenceForDefault()
	if err != nil {
		return nil, err
	}

	return []config.Fragment{
		{Archive: &config.ArchiveFragment{DatabaseReference: res}},
	}, nil
}

// ToStarlark implements common.BuildDefinition.
func (r *readArchiveBuildDefinition) ToStarlark(artifact common.BuildArtifact) (starlark.Value, error) {
	result, err := artifact.Default()
	if err != nil {
		return nil, err
	}

	ark, err := archive.ReadArchiveFromFile(result)
	if err != nil {
		return starlark.None, err
	}

	return star.NewStarArchive(ark, r, artifact.DefinitionHash().String()), nil
}

// NeedsBuild implements BuildDefinition.
func (r *readArchiveBuildDefinition) NeedsBuild(ctx common.BuildContext) (bool, error) {
	return false, nil
}

// Build implements BuildDefinition.
func (r *readArchiveBuildDefinition) Build(ctx common.BuildContext) error {
	art, err := ctx.BuildChild(r.params.Base)
	if err != nil {
		return err
	}

	res, err := art.Default()
	if err != nil {
		return err
	}

	converter, err := getConverterForFile(res, r.params.Kind, r.params.StripComponents)
	if err != nil {
		return err
	}

	return ctx.WriteDefault(converter)
}

func (def *readArchiveBuildDefinition) String() string { return "ReadArchive" }
func (*readArchiveBuildDefinition) Type() string       { return "ReadArchiveBuildDefinition" }
func (*readArchiveBuildDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("ReadArchiveBuildDefinition is not hashable")
}
func (*readArchiveBuildDefinition) Truth() starlark.Bool { return starlark.True }
func (*readArchiveBuildDefinition) Freeze()              {}

var (
	_ common.ReadArchiveDefinition = &readArchiveBuildDefinition{}
)

func newReadArchiveBuildDefinition(base common.BuildDefinition, kind string, stripComponents int) common.ReadArchiveDefinition {
	return &readArchiveBuildDefinition{params: ReadArchiveParameters{Base: base, Kind: kind, StripComponents: stripComponents}}
}

type readArchive2BuildDefinition struct {
	params ReadArchiveParameters
}

// Dependencies implements common.BuildDefinition.
func (def *readArchive2BuildDefinition) Dependencies() ([]common.BuildDefinition, error) {
	return []common.BuildDefinition{def.params.Base}, nil
}

// implements common.BuildDefinition.
func (def *readArchive2BuildDefinition) Params() hash.SerializableValue { return def.params }
func (def *readArchive2BuildDefinition) SerializableType() string {
	return "ReadArchive2BuildDefinition"
}
func (def *readArchive2BuildDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &readArchive2BuildDefinition{params: params.(ReadArchiveParameters)}
}

// AsFragments implements common.Directive.
func (r *readArchive2BuildDefinition) AsFragments(ctx common.BuildContext, special common.SpecialDirectiveHandlers) ([]config.Fragment, error) {
	art, err := ctx.BuildChild(r)
	if err != nil {
		return nil, err
	}

	index, err := art.ReferenceForFile("index")
	if err != nil {
		return nil, err
	}

	contents, err := art.ReferenceForFile("contents")
	if err != nil {
		return nil, err
	}

	return []config.Fragment{
		{Archive2: &config.Archive2Fragment{
			IndexReference:    index,
			ContentsReference: contents,
		}},
	}, nil
}

// ToStarlark implements common.BuildDefinition.
func (r *readArchive2BuildDefinition) ToStarlark(artifact common.BuildArtifact) (starlark.Value, error) {
	return nil, fmt.Errorf("ReadArchive2BuildDefinition can not be converted into a Starlark value")
}

// NeedsBuild implements BuildDefinition.
func (r *readArchive2BuildDefinition) NeedsBuild(ctx common.BuildContext) (bool, error) {
	return false, nil
}

// Build implements BuildDefinition.
func (r *readArchive2BuildDefinition) Build(ctx common.BuildContext) error {
	art, err := ctx.BuildChild(r.params.Base)
	if err != nil {
		return err
	}

	res, err := art.Default()
	if err != nil {
		return err
	}

	converter, err := getConverterForFile(res, r.params.Kind, r.params.StripComponents)
	if err != nil {
		return err
	}

	index, err := ctx.CreateFile("index")
	if err != nil {
		return err
	}

	contents, err := ctx.CreateFile("contents")
	if err != nil {
		return err
	}

	ark, err := archive2.NewArchiveWriter(index, contents)
	if err != nil {
		return err
	}

	if err := converter.WriteArchive2(ark); err != nil {
		return err
	}

	return nil
}

func (def *readArchive2BuildDefinition) String() string { return "ReadArchive2" }
func (*readArchive2BuildDefinition) Type() string       { return "ReadArchive2BuildDefinition" }
func (*readArchive2BuildDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("ReadArchive2BuildDefinition is not hashable")
}
func (*readArchive2BuildDefinition) Truth() starlark.Bool { return starlark.True }
func (*readArchive2BuildDefinition) Freeze()              {}

var (
	_ common.ReadArchiveDefinition = &readArchive2BuildDefinition{}
)

func newReadArchive2BuildDefinition(base common.BuildDefinition, kind string, stripComponents int) common.ReadArchiveDefinition {
	return &readArchive2BuildDefinition{params: ReadArchiveParameters{Base: base, Kind: kind, StripComponents: stripComponents}}
}

func Archive2FromArtifact(artifact common.OutputFileProvider) (*archive2.ArchiveReader, error) {
	index, err := artifact.File("index")
	if err != nil {
		return nil, err
	}

	indexHandle, err := index.Open()
	if err != nil {
		return nil, err
	}

	contents, err := artifact.File("contents")
	if err != nil {
		return nil, err
	}

	contentsHandle, err := contents.Open()
	if err != nil {
		return nil, err
	}

	return archive2.NewArchiveReader(indexHandle, indexHandle, contentsHandle)
}
