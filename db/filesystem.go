package db

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"go.starlark.net/starlark"
)

type FileIf interface {
	Open() (io.ReadCloser, error)
	Stat() (fs.FileInfo, error)
}

type StarFileIf interface {
	starlark.Value
	FileIf
	Name() string
	SetName(name string) (StarFileIf, error)
}

type memoryFile struct {
	contents []byte
}

// IsDir implements fs.FileInfo.
func (m *memoryFile) IsDir() bool {
	return false
}

// ModTime implements fs.FileInfo.
func (m *memoryFile) ModTime() time.Time {
	return time.Now()
}

// Mode implements fs.FileInfo.
func (m *memoryFile) Mode() fs.FileMode {
	return fs.ModePerm
}

// Name implements fs.FileInfo.
func (m *memoryFile) Name() string {
	return ""
}

// Size implements fs.FileInfo.
func (m *memoryFile) Size() int64 {
	return int64(len(m.contents))
}

// Sys implements fs.FileInfo.
func (m *memoryFile) Sys() any {
	return nil
}

// Stat implements FileIf.
func (m *memoryFile) Stat() (fs.FileInfo, error) {
	return m, nil
}

// Open implements FileIf.
func (m *memoryFile) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.contents)), nil
}

var (
	_ FileIf = &memoryFile{}
)

type starFileWrapper struct {
	FileIf
	name string
}

// Name implements StarFileIf.
func (f *starFileWrapper) Name() string { return f.name }

// SetName implements StarFileIf.
func (f *starFileWrapper) SetName(name string) (StarFileIf, error) {
	return &starFileWrapper{name: name, FileIf: f}, nil
}

func (f *starFileWrapper) String() string      { return fmt.Sprintf("File{%s}", f.Name()) }
func (*starFileWrapper) Type() string          { return "File" }
func (*starFileWrapper) Hash() (uint32, error) { return 0, fmt.Errorf("Directory is not hashable") }
func (*starFileWrapper) Truth() starlark.Bool  { return starlark.True }
func (*starFileWrapper) Freeze()               {}

var (
	_ StarFileIf = &starFileWrapper{}
)

func asStarFileIf(name string, v starlark.Value) (StarFileIf, error) {
	switch val := v.(type) {
	case starlark.String:
		return &starFileWrapper{
			name: name,
			FileIf: &memoryFile{
				contents: []byte(val),
			},
		}, nil
	case StarFileIf:
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

	entries map[string]StarFileIf
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
	return fs.ModePerm | fs.ModeDir
}

// Size implements fs.FileInfo.
func (f *StarDirectory) Size() int64 {
	return 0
}

// Sys implements fs.FileInfo.
func (f *StarDirectory) Sys() any {
	return nil
}

// Stat implements StarFileIf.
func (f *StarDirectory) Stat() (fs.FileInfo, error) {
	return f, nil
}

// SetName implements StarFileIf.
func (f *StarDirectory) SetName(name string) (StarFileIf, error) {
	return &StarDirectory{
		name:    name,
		entries: f.entries,
	}, nil
}

// Name implements StarFileIf.
func (f *StarDirectory) Name() string { return f.name }

// Open implements StarFileIf.
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

	if !strings.HasPrefix(cleaned, f.name) {
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

func (f *StarDirectory) getChild(name string, mkdir bool) (StarFileIf, bool, error) {
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
	dir := &StarDirectory{name: path.Join(f.name, name), entries: make(map[string]StarFileIf)}

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

func (f *StarDirectory) openChild(p string, mkdir bool) (StarFileIf, bool, error) {
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

		hdr.Name = filename

		if err := w.WriteHeader(hdr); err != nil {
			return err
		}

		if !info.IsDir() {
			f, err := val.Open()
			if err != nil {
				return err
			}
			defer f.Close()

			if _, err = io.Copy(w, f); err != nil {
				return err
			}
		}
	}

	if err := w.Close(); err != nil {
		return err
	}

	return nil
}

func (f *StarDirectory) WriteTar(w *tar.Writer) (int64, error) {
	return 0, f.writeTar(w, ".")
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
	_ StarFileIf               = &StarDirectory{}
)

func newFilesystem() *StarDirectory {
	return &StarDirectory{
		name:    "/",
		entries: make(map[string]StarFileIf),
	}
}
