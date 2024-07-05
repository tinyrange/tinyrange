package filesystem

import (
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"path"

	"go.starlark.net/starlark"
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
	} else if name == "read_archive" {
		return starlark.NewBuiltin("File.read_archive", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			ark, err := ReadArchiveFromFile(f)
			if err != nil {
				return starlark.None, nil
			}

			return NewStarArchive(ark, f.Name), nil
		}), nil
	} else if name == "read_compressed" {
		return starlark.NewBuiltin("File.read_compressed", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				kind string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"kind", &kind,
			); err != nil {
				return starlark.None, err
			}

			fh, err := f.Open()
			if err != nil {
				return starlark.None, err
			}
			defer fh.Close()

			if kind == ".gz" {
				r, err := gzip.NewReader(fh)
				if err != nil {
					return starlark.None, err
				}

				contents, err := io.ReadAll(r)
				if err != nil {
					return starlark.None, err
				}

				return starlark.String(contents), nil
			} else {
				return starlark.None, fmt.Errorf("read_compressed does not support kind: %s", kind)
			}
		}), nil
	} else if name == "name" {
		return starlark.String(f.Name), nil
	} else if name == "base" {
		return starlark.String(path.Base(f.Name)), nil
	}

	if mut, ok := f.File.(MutableFile); ok {
		_ = mut
	}

	return nil, nil
}

// AttrNames implements starlark.HasAttrs.
func (f *StarFile) AttrNames() []string {
	ret := []string{"read", "read_archive", "name", "base"}

	if _, ok := f.File.(MutableFile); ok {
		ret = append(ret, []string{}...)
	}

	return ret
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

type starDirectoryIterator struct {
	name string
	ents []DirectoryEntry
	off  int
}

// Done implements starlark.Iterator.
func (s *starDirectoryIterator) Done() {
	s.off = len(s.ents)
}

// Next implements starlark.Iterator.
func (s *starDirectoryIterator) Next(p *starlark.Value) bool {
	if s.off == len(s.ents) {
		return false
	}

	ent := s.ents[s.off]

	childName := path.Join(s.name, ent.Name)

	if dir, ok := ent.File.(Directory); ok {
		*p = NewStarDirectory(dir, childName)
	} else {
		*p = NewStarFile(ent.File, childName)
	}

	s.off += 1

	return true
}

var (
	_ starlark.Iterator = &starDirectoryIterator{}
)

type StarDirectory struct {
	Name string
	Directory
}

// Iterate implements starlark.Iterable.
func (f *StarDirectory) Iterate() starlark.Iterator {
	children, err := f.Readdir()
	if err != nil {
		// It's kinda annoying that this method can't return an error.
		return nil
	}

	return &starDirectoryIterator{name: f.Name, ents: children}
}

// Get implements starlark.Mapping.
func (f *StarDirectory) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	name, ok := starlark.AsString(k)
	if !ok {
		return nil, false, fmt.Errorf("expected string got %s", k.Type())
	}

	ent, err := OpenPath(f, name)
	if err == fs.ErrNotExist {
		return nil, false, nil
	} else if err != nil {
		return nil, false, err
	}

	childName := path.Join(f.Name, ent.Name)

	if dir, ok := ent.File.(Directory); ok {
		return NewStarDirectory(dir, childName), true, nil
	} else {
		return NewStarFile(ent.File, childName), true, nil
	}
}

// SetKey implements starlark.HasSetKey.
func (f *StarDirectory) SetKey(k starlark.Value, v starlark.Value) error {
	name, ok := starlark.AsString(k)
	if !ok {
		return fmt.Errorf("expected string got %s", k.Type())
	}

	if file, ok := v.(File); ok {
		return CreateChild(f, name, file)
	} else if contents, ok := v.(starlark.String); ok {
		file := NewMemoryFile(TypeRegular)

		if err := file.Overwrite([]byte(contents)); err != nil {
			return err
		}

		return CreateChild(f, name, file)
	} else {
		return fmt.Errorf("expected File got %s", v.Type())
	}

}

// Attr implements starlark.HasAttrs.
func (f *StarDirectory) Attr(name string) (starlark.Value, error) {
	if name == "name" {
		return starlark.String(f.Name), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (f *StarDirectory) AttrNames() []string {
	return []string{"name"}
}

func (f *StarDirectory) String() string      { return fmt.Sprintf("Directory{%s}", f.Name) }
func (*StarDirectory) Type() string          { return "Directory" }
func (*StarDirectory) Hash() (uint32, error) { return 0, fmt.Errorf("Directory is not hashable") }
func (*StarDirectory) Truth() starlark.Bool  { return starlark.True }
func (*StarDirectory) Freeze()               {}

var (
	_ starlark.Value     = &StarDirectory{}
	_ starlark.HasAttrs  = &StarDirectory{}
	_ starlark.Mapping   = &StarDirectory{}
	_ starlark.HasSetKey = &StarDirectory{}
	_ starlark.Iterable  = &StarDirectory{}
)

func NewStarDirectory(dir Directory, name string) *StarDirectory {
	return &StarDirectory{Directory: dir, Name: name}
}
