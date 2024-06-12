package filesystem

import (
	"fmt"
	"io"

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
	} else if name == "name" {
		return starlark.String(f.Name), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (f *StarFile) AttrNames() []string {
	return []string{"read", "name"}
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
