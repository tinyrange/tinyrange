package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

var starlarkJsonEncode = starlarkjson.Module.Members["encode"].(*starlark.Builtin).CallInternal
var starlarkJsonDecode = starlarkjson.Module.Members["decode"].(*starlark.Builtin).CallInternal

func toStringList(it starlark.Iterable) ([]string, error) {
	iter := it.Iterate()
	defer iter.Done()

	var ret []string

	var val starlark.Value
	for iter.Next(&val) {
		str, ok := starlark.AsString(val)
		if !ok {
			return nil, fmt.Errorf("could not convert %s to string", val.Type())
		}

		ret = append(ret, str)
	}

	return ret, nil
}

func getSha256Hash(content []byte) string {
	sum := sha256.Sum256(content)

	return hex.EncodeToString(sum[:])
}

type FileHandle interface {
	io.ReadCloser
}

type FileInfo interface {
	fs.FileInfo
}

type File interface {
	Open() (FileHandle, error)
	Stat() (FileInfo, error)
}

type FileType byte

const (
	TypeRegular FileType = iota
	TypeDirectory
	TypeSymlink
)

type Entry interface {
	File

	Typeflag() FileType

	Name() string     // Name of file entry
	Linkname() string // Target name of link (valid for TypeLink or TypeSymlink)

	Size() int64       // Logical file size in bytes
	Mode() fs.FileMode // Permission and mode bits
	Uid() int          // User ID of owner
	Gid() int          // Group ID of owner

	ModTime() time.Time // Modification time

	Devmajor() int64 // Major device number (valid for TypeChar or TypeBlock)
	Devminor() int64 // Minor device number (valid for TypeChar or TypeBlock)
}

const CACHE_ENTRY_SIZE = 1024

type CacheEntry struct {
	underlyingFile io.ReaderAt

	COffset   int64    `json:"o"`
	CTypeflag FileType `json:"t"`
	CName     string   `json:"n"`
	CLinkname string   `json:"l"`
	CSize     int64    `json:"s"`
	CMode     int64    `json:"m"`
	CUid      int      `json:"u"`
	CGid      int      `json:"g"`
	CModTime  int64    `json:"e"`
	CDevmajor int64    `json:"a"`
	CDevminor int64    `json:"i"`
}

// IsDir implements FileInfo.
func (e *CacheEntry) IsDir() bool {
	return e.Mode().IsDir()
}

// Sys implements FileInfo.
func (e *CacheEntry) Sys() any {
	return nil
}

// Open implements Entry.
func (e *CacheEntry) Open() (FileHandle, error) {
	return io.NopCloser(io.NewSectionReader(e.underlyingFile, e.COffset, e.CSize)), nil
}

// Stat implements Entry.
func (e *CacheEntry) Stat() (FileInfo, error) {
	return e, nil
}

func (e *CacheEntry) Typeflag() FileType { return e.CTypeflag }
func (e *CacheEntry) Name() string       { return e.CName }
func (e *CacheEntry) Linkname() string   { return e.CLinkname }
func (e *CacheEntry) Size() int64        { return e.CSize }
func (e *CacheEntry) Mode() fs.FileMode  { return fs.FileMode(e.CMode) }
func (e *CacheEntry) Uid() int           { return e.CUid }
func (e *CacheEntry) Gid() int           { return e.CGid }
func (e *CacheEntry) ModTime() time.Time { return time.UnixMicro(e.CModTime) }
func (e *CacheEntry) Devmajor() int64    { return e.CDevmajor }
func (e *CacheEntry) Devminor() int64    { return e.CDevminor }

var (
	_ Entry = &CacheEntry{}
)

type Archive interface {
	Entries() ([]Entry, error)
}

type ArrayArchive []Entry

// Entries implements Archive.
func (a ArrayArchive) Entries() ([]Entry, error) {
	return a, nil
}

var (
	_ Archive = ArrayArchive{}
)

type LocalFile struct {
	Filename string
}

// Open implements File.
func (l *LocalFile) Open() (FileHandle, error) {
	return os.Open(l.Filename)
}

// Stat implements File.
func (l *LocalFile) Stat() (FileInfo, error) {
	return os.Stat(l.Filename)
}

var (
	_ File = &LocalFile{}
)

type StarFile struct {
	File
	Name string
}

// Attr implements starlark.HasAttrs.
func (f *StarFile) Attr(name string) (starlark.Value, error) {
	if name == "read" {
		return starlark.NewBuiltin("File.read", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			fh, err := f.Open()
			if err != nil {
				return starlark.None, nil
			}

			contents, err := io.ReadAll(fh)
			if err != nil {
				return starlark.None, err
			}

			return starlark.String(contents), nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (f *StarFile) AttrNames() []string {
	return []string{"read"}
}

func (f *StarFile) String() string      { return fmt.Sprintf("File{%s}", f.Name) }
func (*StarFile) Type() string          { return "File" }
func (*StarFile) Hash() (uint32, error) { return 0, fmt.Errorf("File is not hashable") }
func (*StarFile) Truth() starlark.Bool  { return starlark.True }
func (*StarFile) Freeze()               {}

var (
	_ starlark.Value    = &StarFile{}
	_ starlark.HasAttrs = &StarFile{}
)

func NewStarFile(f File, name string) *StarFile {
	return &StarFile{File: f, Name: name}
}

type StarArchive struct {
	Archive
	Name string
}

// Get implements starlark.Mapping.
func (f *StarArchive) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	name, ok := starlark.AsString(k)
	if !ok {
		return nil, false, fmt.Errorf("could not convert %s to string", k.Type())
	}

	ents, err := f.Entries()
	if err != nil {
		return nil, false, err
	}

	for _, ent := range ents {
		if ent.Name() == name {
			return NewStarFile(ent, ent.Name()), true, nil
		}
	}

	return nil, false, nil
}

func (f *StarArchive) String() string      { return fmt.Sprintf("Archive{%s}", f.Name) }
func (*StarArchive) Type() string          { return "Archive" }
func (*StarArchive) Hash() (uint32, error) { return 0, fmt.Errorf("Archive is not hashable") }
func (*StarArchive) Truth() starlark.Bool  { return starlark.True }
func (*StarArchive) Freeze()               {}

var (
	_ starlark.Value   = &StarArchive{}
	_ starlark.Mapping = &StarArchive{}
)

func NewStarArchive(ark Archive, name string) *StarArchive {
	return &StarArchive{Archive: ark, Name: name}
}

type BuildResult interface {
	io.WriterTo
}

type BuildDefinition interface {
	BuildSource
	Build(ctx *BuildContext) (BuildResult, error)
}

type StarBuildDefinition struct {
	Name        []string
	Builder     starlark.Callable
	BuilderArgs starlark.Tuple
}

// Tag implements BuildSource.
func (def *StarBuildDefinition) Tag() string {
	var parts []string

	parts = append(parts, def.Name...)

	for _, arg := range def.BuilderArgs {
		str, ok := starlark.AsString(arg)
		if !ok {
			continue
		}

		parts = append(parts, str)
	}

	return strings.Join(parts, "_")
}

func (def *StarBuildDefinition) Build(ctx *BuildContext) (BuildResult, error) {
	res, err := ctx.Call(def.Builder, def.BuilderArgs...)
	if err != nil {
		return nil, err
	}

	result, ok := res.(BuildResult)
	if !ok {
		return nil, fmt.Errorf("could not convert %s to BuildResult", res.Type())
	}

	return result, nil
}

func (def *StarBuildDefinition) String() string { return def.Tag() }
func (*StarBuildDefinition) Type() string       { return "BuildDefinition" }
func (*StarBuildDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("BuildDefinition is not hashable")
}
func (*StarBuildDefinition) Truth() starlark.Bool { return starlark.True }
func (*StarBuildDefinition) Freeze()              {}

var (
	_ starlark.Value  = &StarBuildDefinition{}
	_ BuildSource     = &StarBuildDefinition{}
	_ BuildDefinition = &StarBuildDefinition{}
)

func NewStarBuildDefinition(name string, builder starlark.Value, args starlark.Tuple) (*StarBuildDefinition, error) {
	f, ok := builder.(starlark.Callable)
	if !ok {
		return nil, fmt.Errorf("builder %s is not callable", builder.Type())
	}

	return &StarBuildDefinition{
		Name:        []string{name, f.Name()},
		Builder:     f,
		BuilderArgs: args,
	}, nil
}

var ErrNotFound = errors.New("HTTP 404: Not Found")

type FetchHttpBuildDefinition struct {
	Url string

	resp *http.Response
}

// WriteTo implements BuildResult.
func (f *FetchHttpBuildDefinition) WriteTo(w io.Writer) (n int64, err error) {
	defer f.resp.Body.Close()

	return io.Copy(w, f.resp.Body)
}

// Build implements BuildDefinition.
func (f *FetchHttpBuildDefinition) Build(ctx *BuildContext) (BuildResult, error) {
	urls, err := ctx.Database.urlsFor(f.Url)
	if err != nil {
		return nil, err
	}

	client := ctx.Database.getHttpClient()

	onlyNotFound := true

	for _, url := range urls {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}

		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("failed to fetch", "url", url, "err", err)
			onlyNotFound = false
			continue
		}

		if resp.StatusCode == http.StatusOK {
			f.resp = resp

			return f, nil
		} else if resp.StatusCode == http.StatusNotFound {
			slog.Warn("failed to fetch", "url", url, "err", ErrNotFound)
			continue
		} else {
			slog.Warn("failed to fetch", "url", url, "err", fmt.Errorf("bad status: %s", resp.Status))
			onlyNotFound = false
			continue
		}
	}

	if onlyNotFound {
		return nil, ErrNotFound
	} else {
		return nil, fmt.Errorf("unable to find options to fetch %s", f.Url)
	}
}

// Tag implements BuildDefinition.
func (f *FetchHttpBuildDefinition) Tag() string {
	return f.Url
}

func (def *FetchHttpBuildDefinition) String() string { return def.Tag() }
func (*FetchHttpBuildDefinition) Type() string       { return "FetchHttpBuildDefinition" }
func (*FetchHttpBuildDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("FetchHttpBuildDefinition is not hashable")
}
func (*FetchHttpBuildDefinition) Truth() starlark.Bool { return starlark.True }
func (*FetchHttpBuildDefinition) Freeze()              {}

var (
	_ starlark.Value  = &FetchHttpBuildDefinition{}
	_ BuildDefinition = &FetchHttpBuildDefinition{}
	_ BuildResult     = &FetchHttpBuildDefinition{}
)

func NewFetchHttpBuildDefinition(url string) *FetchHttpBuildDefinition {
	return &FetchHttpBuildDefinition{Url: url}
}

func ReadArchiveFromFile(f File) (Archive, error) {
	fh, err := f.Open()
	if err != nil {
		return nil, err
	}

	readAt, ok := fh.(io.ReaderAt)
	if !ok {
		return nil, fmt.Errorf("%T does not support io.ReaderAt", fh)
	}

	var ret ArrayArchive

	var off int64 = 0

	hdrBytes := make([]byte, 1024)

	for {
		_, err := readAt.ReadAt(hdrBytes, off)
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		off += 1024

		hdrEnd := strings.IndexByte(string(hdrBytes), '\x00')

		var hdr CacheEntry

		if err := json.Unmarshal(hdrBytes[:hdrEnd], &hdr); err != nil {
			return nil, err
		}

		hdr.underlyingFile = readAt

		ret = append(ret, &hdr)

		off += hdr.CSize
	}

	return ret, nil
}

type ReadArchiveBuildDefinition struct {
	Base BuildDefinition
	Kind string

	r *tar.Reader
}

// WriteTo implements BuildResult.
func (r *ReadArchiveBuildDefinition) WriteTo(w io.Writer) (n int64, err error) {
	for {
		hdr, err := r.r.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return -1, err
		}

		info := hdr.FileInfo()

		var typeFlag = TypeRegular

		switch hdr.Typeflag {
		case tar.TypeReg:
			// pass
		case tar.TypeDir:
			typeFlag = TypeDirectory
		case tar.TypeSymlink:
			typeFlag = TypeSymlink
		default:
			return -1, fmt.Errorf("unknown type flag: %d", hdr.Typeflag)
		}

		ent := &CacheEntry{
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

		if len(bytes) > CACHE_ENTRY_SIZE {
			return -1, fmt.Errorf("oversized entry header: %d > %d", len(bytes), CACHE_ENTRY_SIZE)
		} else if len(bytes) < CACHE_ENTRY_SIZE {
			tmp := make([]byte, CACHE_ENTRY_SIZE)
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

// Build implements BuildDefinition.
func (r *ReadArchiveBuildDefinition) Build(ctx *BuildContext) (BuildResult, error) {
	f, err := ctx.Database.Build(ctx, r.Base)
	if err != nil {
		return nil, err
	}

	fh, err := f.Open()
	if err != nil {
		return nil, err
	}

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
		r.r = tar.NewReader(reader)
		return r, nil
	} else {
		return nil, fmt.Errorf("ReadArchive with unknown kind: %s", r.Kind)
	}
}

// Tag implements BuildDefinition.
func (r *ReadArchiveBuildDefinition) Tag() string {
	return strings.Join([]string{"ReadArchive", r.Base.Tag(), r.Kind}, "_")
}

var (
	_ BuildDefinition = &ReadArchiveBuildDefinition{}
	_ BuildResult     = &ReadArchiveBuildDefinition{}
)

func NewReadArchiveBuildDefinition(base BuildDefinition, kind string) *ReadArchiveBuildDefinition {
	return &ReadArchiveBuildDefinition{Base: base, Kind: kind}
}

type PackageQuery struct {
	Name    string
	Version string
}

func ParsePackageQuery(s string) (PackageQuery, error) {
	name, version, _ := strings.Cut(s, ":")

	return PackageQuery{Name: name, Version: version}, nil
}

type PackageName struct {
	Name    string
	Version string
	Tags    []string
}

func (name PackageName) Matches(query PackageQuery) bool {
	if query.Name != "" {
		if name.Name != query.Name {
			return false
		}
	}

	if query.Version != "" {
		if name.Version != query.Version {
			return false
		}
	}

	return true
}

func (name PackageName) String() string   { return fmt.Sprintf("%s:%s", name.Name, name.Version) }
func (PackageName) Type() string          { return "PackageName" }
func (PackageName) Hash() (uint32, error) { return 0, fmt.Errorf("PackageName is not hashable") }
func (PackageName) Truth() starlark.Bool  { return starlark.True }
func (PackageName) Freeze()               {}

var (
	_ starlark.Value = PackageName{}
)

type Package struct {
	Name       PackageName
	Directives []Directive
}

func (pkg *Package) Matches(query PackageQuery) bool {
	return pkg.Name.Matches(query)
}

func (pkg *Package) String() string    { return pkg.Name.String() }
func (*Package) Type() string          { return "Package" }
func (*Package) Hash() (uint32, error) { return 0, fmt.Errorf("Package is not hashable") }
func (*Package) Truth() starlark.Bool  { return starlark.True }
func (*Package) Freeze()               {}

var (
	_ starlark.Value = &Package{}
)

func NewPackage(name PackageName, directives []Directive) *Package {
	return &Package{Name: name, Directives: directives}
}

type PackageCollection struct {
	Name    []string
	Parser  starlark.Callable
	Sources []BuildDefinition

	Packages []*Package
}

// Tag implements BuildSource.
func (parser *PackageCollection) Tag() string {
	return strings.Join(parser.Name, "_")
}

func (parser *PackageCollection) Load(db *PackageDatabase) error {
	var records []starlark.Value

	ctx := db.NewBuildContext(parser)

	// Build all the package sources.
	// This can happen in parallel.
	for _, source := range parser.Sources {
		built, err := db.Build(ctx, source)
		if err != nil {
			return err
		}

		sourceRecords, err := ReadRecordsFromFile(built)
		if err != nil {
			return err
		}

		records = append(records, sourceRecords...)
	}

	// For each record in the list call the parser to parse the record into a package.
	// This can also happen in parallel,
	for _, record := range records {
		child := ctx.childContext(parser, "")

		_, err := child.Call(parser.Parser, record)
		if err != nil {
			return err
		}

		parser.Packages = append(parser.Packages, child.packages...)
	}

	return nil
}

func (parser *PackageCollection) Query(query PackageQuery) ([]*Package, error) {
	var ret []*Package

	for _, pkg := range parser.Packages {
		if pkg.Matches(query) {
			ret = append(ret, pkg)
		}
	}

	return ret, nil
}

func (def *PackageCollection) String() string { return strings.Join(def.Name, "_") }
func (*PackageCollection) Type() string       { return "PackageCollection" }
func (*PackageCollection) Hash() (uint32, error) {
	return 0, fmt.Errorf("PackageCollection is not hashable")
}
func (*PackageCollection) Truth() starlark.Bool { return starlark.True }
func (*PackageCollection) Freeze()              {}

var (
	_ starlark.Value = &PackageCollection{}
	_ BuildSource    = &PackageCollection{}
)

func NewPackageCollection(name string, parser starlark.Value, sources []BuildDefinition) (*PackageCollection, error) {
	f, ok := parser.(starlark.Callable)
	if !ok {
		return nil, fmt.Errorf("parser %s is not callable", parser.Type())
	}

	return &PackageCollection{
		Name:    []string{name, f.Name()},
		Parser:  f,
		Sources: sources,
	}, nil
}

type Directive interface {
	tagDirective()
}

type DirectiveBaseImage string

// tagDirective implements Directive.
func (d DirectiveBaseImage) tagDirective() { panic("unimplemented") }

type DirectiveRunCommand string

// tagDirective implements Directive.
func (d DirectiveRunCommand) tagDirective() { panic("unimplemented") }

var (
	_ Directive = DirectiveBaseImage("")
	_ Directive = DirectiveRunCommand("")
)

type StarDirective struct {
	Directive
}

func (*StarDirective) String() string        { return "Directive" }
func (*StarDirective) Type() string          { return "Directive" }
func (*StarDirective) Hash() (uint32, error) { return 0, fmt.Errorf("Directive is not hashable") }
func (*StarDirective) Truth() starlark.Bool  { return starlark.True }
func (*StarDirective) Freeze()               {}

var (
	_ starlark.Value = &StarDirective{}
)

type InstallationPlan struct {
	Packages   []*Package
	Directives []Directive
}

func EmitDockerfile(plan *InstallationPlan) (string, error) {
	ret := ""

	for _, directive := range plan.Directives {
		switch directive := directive.(type) {
		case DirectiveBaseImage:
			ret += fmt.Sprintf("FROM %s\n", string(directive))
		case DirectiveRunCommand:
			ret += fmt.Sprintf("RUN %s\n", string(directive))
		default:
			return "", fmt.Errorf("directive %T not handled for docker", directive)
		}
	}

	return ret, nil
}

type ContainerBuilder struct {
	Name           string
	DisplayName    string
	BaseDirectives []Directive
	Packages       *PackageCollection
}

func (builder *ContainerBuilder) Load(db *PackageDatabase) error {
	return builder.Packages.Load(db)
}

func (builder *ContainerBuilder) Plan(packages []PackageQuery) (*InstallationPlan, error) {
	plan := &InstallationPlan{}

	plan.Directives = append(plan.Directives, builder.BaseDirectives...)

	for _, pkg := range packages {
		results, err := builder.Packages.Query(pkg)
		if err != nil {
			return nil, err
		}

		if len(results) == 0 {
			return nil, fmt.Errorf("could not find package for query: %s", pkg)
		}

		result := results[0]

		plan.Directives = append(plan.Directives, result.Directives...)
		plan.Packages = append(plan.Packages, result)
	}

	return plan, nil
}

func (*ContainerBuilder) String() string { return "ContainerBuilder" }
func (*ContainerBuilder) Type() string   { return "ContainerBuilder" }
func (*ContainerBuilder) Hash() (uint32, error) {
	return 0, fmt.Errorf("ContainerBuilder is not hashable")
}
func (*ContainerBuilder) Truth() starlark.Bool { return starlark.True }
func (*ContainerBuilder) Freeze()              {}

var (
	_ starlark.Value = &ContainerBuilder{}
)

func NewContainerBuilder(name string, displayName string, baseDirectives []Directive, packages *PackageCollection) (*ContainerBuilder, error) {
	return &ContainerBuilder{
		Name:           name,
		BaseDirectives: baseDirectives,
		Packages:       packages,
	}, nil
}

func ReadRecordsFromFile(f File) ([]starlark.Value, error) {
	fh, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	scan := bufio.NewScanner(fh)
	scan.Buffer(make([]byte, 16*1024), 64*1024)

	var ret []starlark.Value

	for scan.Scan() {
		val, err := starlarkJsonDecode(nil, starlark.Tuple{starlark.String(scan.Text())}, []starlark.Tuple{})
		if err != nil {
			return nil, err
		}

		ret = append(ret, val)
	}

	return ret, nil
}

type RecordWriter struct {
	w io.WriteCloser
}

func (r *RecordWriter) emitString(s string) error {
	_, err := fmt.Fprintf(r.w, "%s\n", s)
	if err != nil {
		return err
	}

	return nil
}

func (r *RecordWriter) Emit(val starlark.Value) error {
	res, err := starlarkJsonEncode(nil, starlark.Tuple{val}, []starlark.Tuple{})
	if err != nil {
		return err
	}

	resString := string(res.(starlark.String))

	return r.emitString(resString)
}

// WriteTo implements BuildResult.
func (r *RecordWriter) WriteTo(w io.Writer) (n int64, err error) {
	return 0, r.w.Close()
}

// Attr implements starlark.HasAttrs.
func (r *RecordWriter) Attr(name string) (starlark.Value, error) {
	if name == "emit" {
		return starlark.NewBuiltin("RecordWriter.emit", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				val starlark.Value
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"val", &val,
			); err != nil {
				return starlark.None, err
			}

			if err := r.Emit(val); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (r *RecordWriter) AttrNames() []string {
	return []string{"emit"}
}

func (*RecordWriter) String() string { return "RecordWriter" }
func (*RecordWriter) Type() string   { return "RecordWriter" }
func (*RecordWriter) Hash() (uint32, error) {
	return 0, fmt.Errorf("RecordWriter is not hashable")
}
func (*RecordWriter) Truth() starlark.Bool { return starlark.True }
func (*RecordWriter) Freeze()              {}

var (
	_ starlark.Value    = &RecordWriter{}
	_ starlark.HasAttrs = &RecordWriter{}
	_ BuildResult       = &RecordWriter{}
)

type BuildSource interface {
	Tag() string
}

type BuildContext struct {
	Source   BuildSource
	Database *PackageDatabase
	parent   *BuildContext

	filename string
	output   io.WriteCloser
	packages []*Package
}

func (b *BuildContext) childContext(source BuildSource, filename string) *BuildContext {
	return &BuildContext{
		parent:   b,
		filename: filename,
		output:   nil,
		Source:   source,
		Database: b.Database,
	}
}

func (b *BuildContext) createOutput() (io.WriteCloser, error) {
	if b.output != nil {
		return nil, fmt.Errorf("output already created")
	}

	out, err := os.Create(b.filename)
	if err != nil {
		return nil, err
	}

	b.output = out

	return b.output, nil
}

func (b *BuildContext) hasCreatedOutput() bool {
	return b.output != nil
}

// Attr implements starlark.HasAttrs.
func (b *BuildContext) Attr(name string) (starlark.Value, error) {
	if name == "read_archive" {
		return starlark.NewBuiltin("BuildContext.read_archive", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				def  BuildDefinition
				kind string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"def", &def,
				"kind", &kind,
			); err != nil {
				return starlark.None, err
			}

			archiveDef := NewReadArchiveBuildDefinition(def, kind)

			f, err := b.Database.Build(b, archiveDef)
			if err != nil {
				return starlark.None, err
			}

			ark, err := ReadArchiveFromFile(f)
			if err != nil {
				return starlark.None, err
			}

			return NewStarArchive(ark, def.Tag()), nil
		}), nil
	} else if name == "recordwriter" {
		return starlark.NewBuiltin("BuildContext.recordwriter", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			f, err := b.createOutput()
			if err != nil {
				return nil, err
			}

			return &RecordWriter{w: f}, nil
		}), nil
	} else if name == "add_package" {
		return starlark.NewBuiltin("BuildContext.add_package", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				pkg *Package
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"pkg", &pkg,
			); err != nil {
				return starlark.None, err
			}

			b.packages = append(b.packages, pkg)

			return starlark.None, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (b *BuildContext) AttrNames() []string {
	return []string{"fetch_http"}
}

func (ctx *BuildContext) newThread() *starlark.Thread {
	return &starlark.Thread{Name: ctx.Source.Tag()}
}

func (ctx *BuildContext) Call(target starlark.Callable, args ...starlark.Value) (starlark.Value, error) {
	result, err := starlark.Call(ctx.newThread(), target, append(starlark.Tuple{ctx}, args...), []starlark.Tuple{})
	if err != nil {
		return starlark.None, err
	}

	return result, nil
}

func (*BuildContext) String() string        { return "BuildContext" }
func (*BuildContext) Type() string          { return "BuildContext" }
func (*BuildContext) Hash() (uint32, error) { return 0, fmt.Errorf("BuildContext is not hashable") }
func (*BuildContext) Truth() starlark.Bool  { return starlark.True }
func (*BuildContext) Freeze()               {}

var (
	_ starlark.Value    = &BuildContext{}
	_ starlark.HasAttrs = &BuildContext{}
)

type PackageDatabase struct {
	ContainerBuilders map[string]*ContainerBuilder

	mirrors map[string][]string
}

func (db *PackageDatabase) newThread(name string) *starlark.Thread {
	return &starlark.Thread{Name: name}
}

func (db *PackageDatabase) getGlobals(name string) starlark.StringDict {
	ret := starlark.StringDict{}

	ret["__name__"] = starlark.String(name)

	ret["json"] = starlarkjson.Module

	ret["db"] = db

	ret["define"] = &starlarkstruct.Module{
		Name: "define",
		Members: starlark.StringDict{
			"build": starlark.NewBuiltin("define.build", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				builder := args[0]
				builderArgs := args[1:]

				return NewStarBuildDefinition(thread.Name, builder, builderArgs)
			}),
			"package_collection": starlark.NewBuiltin("define.package_collection", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				parser := args[0]

				var defs []BuildDefinition

				for _, arg := range args[1:] {
					def, ok := arg.(BuildDefinition)
					if !ok {
						return starlark.None, fmt.Errorf("could not convert %s to BuildDefinition", arg.Type())
					}

					defs = append(defs, def)
				}

				return NewPackageCollection(thread.Name, parser, defs)
			}),
			"container_builder": starlark.NewBuiltin("define.container_builder", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					name             string
					displayName      string
					baseDirectivesIt starlark.Iterable
					packages         *PackageCollection
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"name", &name,
					"display_name?", &displayName,
					"base_directives?", &baseDirectivesIt,
					"packages?", &packages,
				); err != nil {
					return starlark.None, err
				}

				iter := baseDirectivesIt.Iterate()
				defer iter.Done()

				var baseDirectives []Directive

				var val starlark.Value
				for iter.Next(&val) {
					dir, ok := val.(*StarDirective)
					if !ok {
						return nil, fmt.Errorf("could not convert %s to Directive", val.Type())
					}

					baseDirectives = append(baseDirectives, dir.Directive)
				}

				return NewContainerBuilder(name, displayName, baseDirectives, packages)
			}),
			"fetch_http": starlark.NewBuiltin("define.fetch_http", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					url string
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"url", &url,
				); err != nil {
					return starlark.None, err
				}

				return NewFetchHttpBuildDefinition(url), nil
			}),
		},
	}

	ret["directive"] = &starlarkstruct.Module{
		Name: "directive",
		Members: starlark.StringDict{
			"base_image": starlark.NewBuiltin("directive.base_image", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					image string
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"image", &image,
				); err != nil {
					return starlark.None, err
				}

				return &StarDirective{Directive: DirectiveBaseImage(image)}, nil
			}),
			"run_command": starlark.NewBuiltin("directive.run_command", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					command string
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"command", &command,
				); err != nil {
					return starlark.None, err
				}

				return &StarDirective{Directive: DirectiveRunCommand(command)}, nil
			}),
		},
	}

	ret["package"] = starlark.NewBuiltin("package", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			name          PackageName
			directiveList starlark.Iterable
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"name", &name,
			"directives", &directiveList,
		); err != nil {
			return starlark.None, err
		}

		iter := directiveList.Iterate()
		defer iter.Done()

		var directives []Directive

		var val starlark.Value
		for iter.Next(&val) {
			dir, ok := val.(*StarDirective)
			if !ok {
				return nil, fmt.Errorf("could not convert %s to Directive", val.Type())
			}

			directives = append(directives, dir.Directive)
		}

		return NewPackage(name, directives), nil
	})

	ret["name"] = starlark.NewBuiltin("name", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			name    string
			version string
			tags    starlark.Iterable
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"name", &name,
			"version", &version,
			"tags?", &tags,
		); err != nil {
			return starlark.None, err
		}

		var stringTags []string

		if tags != nil {
			var err error

			stringTags, err = toStringList(tags)
			if err != nil {
				return starlark.None, err
			}
		}

		return db.NewName(name, version, stringTags)
	})

	ret["error"] = starlark.NewBuiltin("error", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			message string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"message", &message,
		); err != nil {
			return starlark.None, err
		}

		return starlark.None, errors.New(message)
	})

	return ret
}

func (db *PackageDatabase) getFileOptions() *syntax.FileOptions {
	return &syntax.FileOptions{
		Set:             true,
		While:           true,
		TopLevelControl: true,
	}
}

func (db *PackageDatabase) getHttpClient() *http.Client {
	return &http.Client{}
}

func (db *PackageDatabase) urlsFor(urlStr string) ([]string, error) {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	if parsed.Scheme != "mirror" {
		return []string{urlStr}, nil
	}

	mirror := parsed.Hostname()
	suffix := strings.TrimPrefix(urlStr, fmt.Sprintf("mirror://%s", mirror))

	urls, ok := db.mirrors[mirror]
	if !ok {
		return nil, fmt.Errorf("mirror %s not defined", mirror)
	}

	var ret []string

	for _, url := range urls {
		ret = append(ret, url+suffix)
	}

	return ret, nil
}

func (db *PackageDatabase) AddMirror(name string, options []string) error {
	db.mirrors[name] = options
	return nil
}

func (db *PackageDatabase) AddContainerBuilder(builder *ContainerBuilder) error {
	db.ContainerBuilders[builder.Name] = builder

	return nil
}

func (db *PackageDatabase) LoadScript(filename string) error {
	thread := db.newThread(filename)

	globals := db.getGlobals("__main__")

	// Execute the file.
	if _, err := starlark.ExecFileOptions(db.getFileOptions(), thread, filename, nil, globals); err != nil {
		return err
	}

	return nil
}

func (db *PackageDatabase) LoadAll() error {
	for _, builder := range db.ContainerBuilders {
		if err := builder.Load(db); err != nil {
			return err
		}
	}

	return nil
}

func (db *PackageDatabase) NewBuildContext(source BuildSource) *BuildContext {
	return &BuildContext{Source: source, Database: db}
}

func (db *PackageDatabase) Build(ctx *BuildContext, def BuildDefinition) (File, error) {
	hash := getSha256Hash([]byte(def.Tag()))

	filename := filepath.Join("local", "build", hash+".bin")

	// Check if the file already exists. If it does then return it.
	if _, err := os.Stat(filename); err == nil {
		return &LocalFile{Filename: filename}, nil
	}

	// Get a child context for the build.
	child := ctx.childContext(def, filename+".tmp")

	slog.Info("building", "Tag", def.Tag())

	// If not then trigger the build.
	result, err := def.Build(child)
	if err != nil {
		return nil, err
	}

	// If the build has already been written then don't write it again.
	if !child.hasCreatedOutput() {
		// Once the build is complete then write it to disk.
		outFile, err := os.Create(filename + ".tmp")
		if err != nil {
			return nil, err
		}

		// Write the build result to disk. If any of these steps fail then remove the temporary file.
		if _, err := result.WriteTo(outFile); err != nil {
			outFile.Close()
			os.Remove(filename + ".tmp")
			return nil, err
		}

		if err := outFile.Close(); err != nil {
			os.Remove(filename + ".tmp")
			return nil, err
		}
	} else {
		// Let the result close the file on it's own.
		if _, err := result.WriteTo(nil); err != nil {
			os.Remove(filename + ".tmp")
			return nil, err
		}
	}

	// Finally rename the temporary file to the final filename.
	if err := os.Rename(filename+".tmp", filename); err != nil {
		os.Remove(filename + ".tmp")
		return nil, err
	}

	// Return the file.
	return &LocalFile{Filename: filename}, nil
}

func (db *PackageDatabase) NewName(name string, version string, tags []string) (PackageName, error) {
	return PackageName{
		Name:    name,
		Version: version,
		Tags:    tags,
	}, nil
}

func (db *PackageDatabase) GetBuilder(name string) (*ContainerBuilder, error) {
	builder, ok := db.ContainerBuilders[name]
	if !ok {
		return nil, fmt.Errorf("builder %s not found", name)
	}

	return builder, nil
}

// Attr implements starlark.HasAttrs.
func (db *PackageDatabase) Attr(name string) (starlark.Value, error) {
	if name == "add_mirror" {
		return starlark.NewBuiltin("Database.add_mirror", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name       string
				mirrorsVal starlark.Iterable
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"name", &name,
				"mirrors", &mirrorsVal,
			); err != nil {
				return starlark.None, err
			}

			mirrors, err := toStringList(mirrorsVal)
			if err != nil {
				return starlark.None, err
			}

			return starlark.None, db.AddMirror(name, mirrors)
		}), nil
	} else if name == "add_container_builder" {
		return starlark.NewBuiltin("Database.add_container_builder", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				builder *ContainerBuilder
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"builder", &builder,
			); err != nil {
				return starlark.None, err
			}

			return starlark.None, db.AddContainerBuilder(builder)
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (db *PackageDatabase) AttrNames() []string {
	return []string{"add_mirror"}
}

func (*PackageDatabase) String() string        { return "Database" }
func (*PackageDatabase) Type() string          { return "Database" }
func (*PackageDatabase) Hash() (uint32, error) { return 0, fmt.Errorf("Database is not hashable") }
func (*PackageDatabase) Truth() starlark.Bool  { return starlark.True }
func (*PackageDatabase) Freeze()               {}

var (
	_ starlark.Value    = &PackageDatabase{}
	_ starlark.HasAttrs = &PackageDatabase{}
)

func New() *PackageDatabase {
	return &PackageDatabase{
		ContainerBuilders: make(map[string]*ContainerBuilder),
		mirrors:           make(map[string][]string),
	}
}

var (
	makeList = flag.String("make", "", "make a container from a list of packages")
	builder  = flag.String("builder", "", "specify a builder to use for making containers")
)

func main() {
	flag.Parse()

	db := New()

	for _, arg := range flag.Args() {
		if err := db.LoadScript(arg); err != nil {
			log.Fatal(err)
		}
	}

	if err := db.LoadAll(); err != nil {
		log.Fatal(err)
	}

	if *builder != "" {
		builder, err := db.GetBuilder(*builder)
		if err != nil {
			log.Fatal(err)
		}

		if *makeList != "" {
			pkgs := strings.Split((*makeList), ",")

			var queries []PackageQuery
			for _, pkg := range pkgs {
				query, err := ParsePackageQuery(pkg)
				if err != nil {
					log.Fatal(err)
				}

				queries = append(queries, query)
			}

			plan, err := builder.Plan(queries)
			if err != nil {
				log.Fatal(err)
			}

			contents, err := EmitDockerfile(plan)
			if err != nil {
				log.Fatal(err)
			}

			if _, err := fmt.Fprintf(os.Stdout, "%s\n", contents); err != nil {
				log.Fatal(err)
			}
		} else {
			flag.Usage()
		}
	} else {
		flag.Usage()
	}
}
