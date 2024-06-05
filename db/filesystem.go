package db

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/tinyrange/pkg2/db/common"
	"github.com/tinyrange/pkg2/memtar"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

type MemoryFile struct {
	contents []byte
	mode     fs.FileMode
	modTime  time.Time
	uid      int
	gid      int
	linkname string
}

// Linkname implements FileInfo.
func (m *MemoryFile) Linkname() string {
	return m.linkname
}

// OwnerGroup implements FileInfo.
func (m *MemoryFile) OwnerGroup() (int, int) {
	return m.uid, m.gid
}

// IsDir implements fs.FileInfo.
func (m *MemoryFile) IsDir() bool {
	return false
}

// ModTime implements fs.FileInfo.
func (m *MemoryFile) ModTime() time.Time {
	return m.modTime
}

// Mode implements fs.FileInfo.
func (m *MemoryFile) Mode() fs.FileMode {
	return m.mode
}

// Name implements fs.FileInfo.
func (m *MemoryFile) Name() string {
	return ""
}

// Size implements fs.FileInfo.
func (m *MemoryFile) Size() int64 {
	return int64(len(m.contents))
}

// Sys implements fs.FileInfo.
func (m *MemoryFile) Sys() any {
	return nil
}

// Stat implements common.FileIf.
func (m *MemoryFile) Stat() (common.FileInfo, error) {
	return m, nil
}

// Open implements common.FileIf.
func (m *MemoryFile) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.contents)), nil
}

func (m *MemoryFile) WrapStarlark(name string) starlark.Value {
	return &starFileWrapper{name: name, FileIf: m}
}

var (
	_ common.FileIf = &MemoryFile{}
)

func NewMemoryFile(contents []byte) *MemoryFile {
	return &MemoryFile{
		contents: contents,
		mode:     fs.FileMode(0644),
	}
}

type entryWrapper struct {
	memtar.Entry
}

// IsDir implements FileInfo.
func (e *entryWrapper) IsDir() bool {
	return e.Mode().IsDir()
}

// Mode implements FileInfo.
// Subtle: this method shadows the method (Entry).Mode of entryWrapper.Entry.
func (e *entryWrapper) Mode() fs.FileMode {
	return e.FileMode()
}

// OwnerGroup implements FileInfo.
func (e *entryWrapper) OwnerGroup() (int, int) {
	return e.Uid(), e.Gid()
}

// Sys implements FileInfo.
func (e *entryWrapper) Sys() any {
	return nil
}

// Open implements common.FileIf.
func (e *entryWrapper) Open() (io.ReadCloser, error) {
	return io.NopCloser(e.Entry.Open()), nil
}

// Stat implements common.FileIf.
func (e *entryWrapper) Stat() (common.FileInfo, error) {
	return e, nil
}

var (
	_ common.FileIf = &entryWrapper{}
)

type starFileWrapper struct {
	common.FileIf
	name string
}

// Source implements common.StarFileIf.
func (f *starFileWrapper) Source() common.FileSource {
	return nil
}

// Attr implements starlark.HasAttrs.
func (f *starFileWrapper) Attr(name string) (starlark.Value, error) {
	return starFileCommonAttrs(f, name)
}

// AttrNames implements starlark.HasAttrs.
func (f *starFileWrapper) AttrNames() []string {
	return starFileCommonAttrNames(f)
}

// Name implements common.StarFileIf.
func (f *starFileWrapper) Name() string { return f.name }

// SetName implements common.StarFileIf.
func (f *starFileWrapper) SetName(name string) (common.StarFileIf, error) {
	return &starFileWrapper{name: name, FileIf: f}, nil
}

func (f *starFileWrapper) String() string      { return fmt.Sprintf("File{%s}", f.Name()) }
func (*starFileWrapper) Type() string          { return "StarFile" }
func (*starFileWrapper) Hash() (uint32, error) { return 0, fmt.Errorf("File is not hashable") }
func (*starFileWrapper) Truth() starlark.Bool  { return starlark.True }
func (*starFileWrapper) Freeze()               {}

var (
	_ common.StarFileIf = &starFileWrapper{}
	_ starlark.HasAttrs = &starFileWrapper{}
)

func asStarFileIf(name string, v starlark.Value) (common.StarFileIf, error) {
	switch val := v.(type) {
	case starlark.String:
		return &starFileWrapper{
			name:   name,
			FileIf: NewMemoryFile([]byte(val)),
		}, nil
	case common.StarFileIf:
		return val, nil
	default:
		return nil, fmt.Errorf("could not convert %s to File", v.Type())
	}
}

var (
	ErrUpwardPathTraversal = errors.New("upward path traversal")
)

type directoryIterator struct {
	dir  *StarDirectory
	keys []string
	idx  int
}

// Done implements starlark.Iterator.
func (d *directoryIterator) Done() {
	d.idx = len(d.keys)
}

// Next implements starlark.Iterator.
func (d *directoryIterator) Next(p *starlark.Value) bool {
	if d.idx >= len(d.keys) {
		return false
	}

	*p = d.dir.entries[d.keys[d.idx]]

	d.idx += 1

	return true
}

var (
	_ starlark.Iterator = &directoryIterator{}
)

type StarDirectory struct {
	name string

	entries map[string]common.StarFileIf
}

// Entries implements common.StarDirectory.
func (f *StarDirectory) Entries() map[string]common.StarFileIf {
	return f.entries
}

// OpenChild implements common.StarDirectory.
func (f *StarDirectory) OpenChild(path string, mkdir bool) (common.StarFileIf, bool, error) {
	return f.openChild(path, mkdir)
}

// Source implements common.StarFileIf.
func (f *StarDirectory) Source() common.FileSource {
	return nil
}

// Linkname implements FileInfo.
func (f *StarDirectory) Linkname() string {
	return ""
}

// Binary implements starlark.HasBinary.
func (f *StarDirectory) Binary(op syntax.Token, y starlark.Value, side starlark.Side) (starlark.Value, error) {
	switch op {
	case syntax.PLUS:
		switch y := y.(type) {
		case *StarDirectory:
			return f.MergeWith(y)
		default:
			return starlark.None, fmt.Errorf("unknown binary op: Directory %s %s", op, y.Type())
		}
	default:
		return starlark.None, fmt.Errorf("unknown binary op: Directory %s %s", op, y.Type())
	}
}

// OwnerGroup implements FileInfo.
func (f *StarDirectory) OwnerGroup() (int, int) {
	return 0, 0
}

// IsDir implements fs.FileInfo.
func (f *StarDirectory) IsDir() bool {
	return true
}

// ModTime implements fs.FileInfo.
func (f *StarDirectory) ModTime() time.Time {
	return time.Now()
}

// Mode implements fs.FileInfo.
func (f *StarDirectory) Mode() fs.FileMode {
	return fs.FileMode(0644) | fs.ModeDir
}

// Size implements fs.FileInfo.
func (f *StarDirectory) Size() int64 {
	return 0
}

// Sys implements fs.FileInfo.
func (f *StarDirectory) Sys() any {
	return nil
}

// Stat implements common.StarFileIf.
func (f *StarDirectory) Stat() (common.FileInfo, error) {
	return f, nil
}

// SetName implements common.StarFileIf.
func (f *StarDirectory) SetName(name string) (common.StarFileIf, error) {
	return &StarDirectory{
		name:    name,
		entries: f.entries,
	}, nil
}

// Name implements common.StarFileIf.
func (f *StarDirectory) Name() string { return f.name }

// Open implements common.StarFileIf.
func (f *StarDirectory) Open() (io.ReadCloser, error) {
	panic("unimplemented")
}

// Items implements starlark.IterableMapping.
func (f *StarDirectory) Items() []starlark.Tuple {
	var ret []starlark.Tuple

	for k, v := range f.entries {
		ret = append(ret, starlark.Tuple{starlark.String(k), v})
	}

	return ret
}

// Iterate implements starlark.IterableMapping.
func (f *StarDirectory) Iterate() starlark.Iterator {
	var keys []string
	for k := range f.entries {
		keys = append(keys, k)
	}

	return &directoryIterator{dir: f, keys: keys}
}

func (f *StarDirectory) cleanPath(p string) (string, error) {
	cleaned := path.Clean(path.Join(f.name, p)) // Get the absolute path.

	if f.name == "." {
		f.name = ""
	}

	if !strings.HasPrefix(cleaned, f.name) {
		slog.Error("upward path traversal", "cleaned", cleaned, "f.name", f.name)
		return "", ErrUpwardPathTraversal
	}

	return strings.TrimPrefix(cleaned, f.name), nil
}

func (f *StarDirectory) splitPath(p string) ([]string, error) {
	p, err := f.cleanPath(p)
	if err != nil {
		return nil, err
	}

	return strings.Split(p, "/"), nil
}

func (f *StarDirectory) getChild(name string, mkdir bool) (common.StarFileIf, bool, error) {
	child, ok := f.entries[name]
	if !ok && mkdir {
		if child, err := f.mkdir(name); err == nil {
			return child, true, nil
		} else {
			return nil, false, err
		}
	} else {
		return child, ok, nil
	}
}

func (f *StarDirectory) openPath(p string, mkdir bool) (*StarDirectory, string, bool, error) {
	tokens, err := f.splitPath(p)
	if err != nil {
		return nil, "", false, err
	}

	current := f

	for _, token := range tokens[:len(tokens)-1] {
		if token == "" {
			continue
		}

		child, ok, err := current.getChild(token, mkdir)
		if err != nil {
			return nil, "", false, err
		}
		if !ok {
			return nil, "", false, nil
		}

		if dir, ok := child.(*StarDirectory); ok {
			current = dir
		} else {
			// not a directory
			return nil, "", true, fs.ErrInvalid
		}
	}

	return current, tokens[len(tokens)-1], true, nil
}

func (f *StarDirectory) mkdirInternal(name string) (*StarDirectory, error) {
	dir := &StarDirectory{name: path.Join(f.name, name), entries: make(map[string]common.StarFileIf)}

	f.entries[name] = dir

	return dir, nil
}

func (f *StarDirectory) mkdir(p string) (*StarDirectory, error) {
	parent, name, ok, err := f.openPath(p, true)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fs.ErrInvalid
	}

	return parent.mkdirInternal(name)
}

func (f *StarDirectory) openChild(p string, mkdir bool) (common.StarFileIf, bool, error) {
	parent, name, ok, err := f.openPath(p, mkdir)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}

	return parent.getChild(name, mkdir)
}

// Get implements starlark.Mapping.
func (f *StarDirectory) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	str, ok := starlark.AsString(k)
	if !ok {
		return starlark.None, false, fmt.Errorf("could not convert %s to string", k.Type())
	}

	return f.openChild(str, false)
}

// SetKey implements starlark.HasSetKey.
func (f *StarDirectory) SetKey(k starlark.Value, v starlark.Value) error {
	str, ok := starlark.AsString(k)
	if !ok {
		return fmt.Errorf("could not convert %s to string", k.Type())
	}

	cleanPath, err := f.cleanPath(str)
	if err != nil {
		return err
	}

	fileIf, err := asStarFileIf(cleanPath, v)
	if err != nil {
		return err
	}

	parent, name, ok, err := f.openPath(cleanPath, true)
	if !ok {
		return fs.ErrInvalid
	}
	if err != nil {
		return err
	}

	newFile, err := fileIf.SetName(cleanPath)
	if err != nil {
		return err
	}

	parent.entries[name] = newFile

	return nil
}

// Attr implements starlark.HasAttrs.
func (f *StarDirectory) Attr(name string) (starlark.Value, error) {
	return nil, nil
}

// AttrNames implements starlark.HasAttrs.
func (f *StarDirectory) AttrNames() []string {
	return []string{}
}

func (f *StarDirectory) writeTar(w *tar.Writer, name string) error {
	for n, val := range f.entries {
		filename := path.Join(name, n)

		info, err := val.Stat()
		if err != nil {
			return err
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		hdr.Uid, hdr.Gid = info.OwnerGroup()

		hdr.Name = filename
		hdr.Linkname = info.Linkname()

		if err := w.WriteHeader(hdr); err != nil {
			return err
		}

		if info.Mode().Type() == fs.ModeSymlink {
			// continue
		} else if info.Mode().IsRegular() {
			f, err := val.Open()
			if err != nil {
				return err
			}
			defer f.Close()

			if _, err = io.Copy(w, f); err != nil {
				return err
			}
		} else if dir, ok := val.(*StarDirectory); ok {
			if err := dir.writeTar(w, filename); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("unexpected entry type: %T", val)
		}
	}

	return nil
}

func (f *StarDirectory) WriteTar(w *tar.Writer) (int64, error) {
	if err := f.writeTar(w, "."); err != nil {
		return -1, err
	}

	if err := w.Close(); err != nil {
		return -1, err
	}

	return 0, nil
}

func (f *StarDirectory) MergeWith(other *StarDirectory) (*StarDirectory, error) {
	// TODO(joshua): merge properties

	ret := &StarDirectory{
		name:    f.name,
		entries: make(map[string]common.StarFileIf),
	}

	for name, ent := range f.entries {
		if otherEnt, ok := other.entries[name]; ok {
			entDir, entIsDir := ent.(*StarDirectory)
			otherDir, otherIsDir := otherEnt.(*StarDirectory)

			if entIsDir && otherIsDir {
				merged, err := entDir.MergeWith(otherDir)
				if err != nil {
					return nil, err
				}

				ret.entries[name] = merged
			} else {
				ret.entries[name] = otherEnt
			}
		} else {
			ret.entries[name] = ent
		}
	}

	for name, ent := range other.entries {
		// Check if we already handled in the previous loop.
		if _, ok := f.entries[name]; ok {
			continue
		}

		ret.entries[name] = ent
	}

	// slog.Info("merged", "f", len(f.entries), "other", len(other.entries), "ret", len(ret.entries))

	return ret, nil
}

func (f *StarDirectory) addArchive(ark *StarArchive) error {
	for _, ent := range ark.Entries() {
		cleanPath, err := f.cleanPath(ent.Filename())
		if err != nil {
			return err
		}

		parent, name, ok, err := f.openPath(cleanPath, true)
		if !ok {
			return fs.ErrInvalid
		}
		if err != nil {
			return err
		}

		switch ent.Typeflag() {
		case tar.TypeReg:
			parent.entries[name] = &starFileWrapper{name: cleanPath, FileIf: &entryWrapper{ent}}
		case tar.TypeSymlink:
			parent.entries[name] = &starFileWrapper{name: cleanPath, FileIf: &entryWrapper{ent}}
		case tar.TypeDir:
			if _, err := parent.mkdirInternal(name); err != nil {
				return err
			}
		case tar.TypeXGlobalHeader:
			// ignored
			continue
		default:
			return fmt.Errorf("StarDirectory.addArchive: unexpected Typeflag %d", ent.Typeflag())
		}
	}

	return nil
}

func (f *StarDirectory) String() string      { return fmt.Sprintf("Directory{%s}", f.name) }
func (*StarDirectory) Type() string          { return "Directory" }
func (*StarDirectory) Hash() (uint32, error) { return 0, fmt.Errorf("Directory is not hashable") }
func (*StarDirectory) Truth() starlark.Bool  { return starlark.True }
func (*StarDirectory) Freeze()               {}

var (
	_ starlark.Value           = &StarDirectory{}
	_ starlark.HasAttrs        = &StarDirectory{}
	_ starlark.Mapping         = &StarDirectory{}
	_ starlark.IterableMapping = &StarDirectory{}
	_ starlark.HasSetKey       = &StarDirectory{}
	_ starlark.HasBinary       = &StarDirectory{}
	_ common.StarDirectory     = &StarDirectory{}
)

func newFilesystem() *StarDirectory {
	return &StarDirectory{
		name:    ".",
		entries: make(map[string]common.StarFileIf),
	}
}
