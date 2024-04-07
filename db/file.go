package db

import (
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	"github.com/tinyrange/pkg2/memtar"
	"go.starlark.net/starlark"
)

type File interface {
	io.Reader
}

type StarFile struct {
	f    File
	name string
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
			contents, err := io.ReadAll(f.f)
			if err != nil {
				return nil, fmt.Errorf("failed to read file: %s", err)
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
			var (
				ext string
			)

			if err := starlark.UnpackArgs("File.read_archive", args, kwargs,
				"ext", &ext,
			); err != nil {
				return starlark.None, err
			}

			reader, err := ReadArchive(f.f, ext)
			if err != nil {
				return starlark.None, fmt.Errorf("failed to read archive: %s", err)
			}

			return &StarArchive{r: reader}, nil
		}), nil
	} else if name == "read_compressed" {
		return starlark.NewBuiltin("File.read_compressed", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				ext string
			)

			if err := starlark.UnpackArgs("File.read_compressed", args, kwargs,
				"ext", &ext,
			); err != nil {
				return starlark.None, err
			}

			if strings.HasSuffix(ext, ".gz") {
				r, err := gzip.NewReader(f.f)
				if err != nil {
					return nil, fmt.Errorf("failed to read compressed")
				}
				return &StarFile{f: r, name: strings.TrimSuffix(f.name, ext)}, nil
			} else {
				return starlark.None, fmt.Errorf("unsupported extension: %s", ext)
			}
		}), nil
	} else if name == "name" {
		return starlark.String(f.name), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (*StarFile) AttrNames() []string {
	return []string{"read", "read_archive", "read_compressed", "name"}
}

func (f *StarFile) String() string      { return fmt.Sprintf("File{%s}", f.name) }
func (*StarFile) Type() string          { return "File" }
func (*StarFile) Hash() (uint32, error) { return 0, fmt.Errorf("File is not hashable") }
func (*StarFile) Truth() starlark.Bool  { return starlark.True }
func (*StarFile) Freeze()               {}

var (
	_ starlark.Value    = &StarFile{}
	_ starlark.HasAttrs = &StarFile{}
)

type StarArchiveIterator struct {
	ents  []memtar.Entry
	index int
}

// Done implements starlark.Iterator.
func (it *StarArchiveIterator) Done() {

}

// Next implements starlark.Iterator.
func (it *StarArchiveIterator) Next(p *starlark.Value) bool {
	if it.index >= len(it.ents) {
		return false
	}

	ent := it.ents[it.index]

	*p = &StarFile{f: ent.Open(), name: ent.Filename()}

	it.index += 1

	return true
}

var (
	_ starlark.Iterator = &StarArchiveIterator{}
)

type StarArchive struct {
	r memtar.TarReader
}

// Get implements starlark.Mapping.
func (ar *StarArchive) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	filename, _ := starlark.AsString(k)

	for _, ent := range ar.r.Entries() {
		if ent.Filename() == filename {
			return &StarFile{f: ent.Open(), name: ent.Filename()}, true, nil
		}
	}

	return starlark.None, false, nil
}

// Iterate implements starlark.Iterable.
func (ar *StarArchive) Iterate() starlark.Iterator {
	return &StarArchiveIterator{ents: ar.r.Entries()}
}

func (*StarArchive) String() string        { return "StarArchive" }
func (*StarArchive) Type() string          { return "StarArchive" }
func (*StarArchive) Hash() (uint32, error) { return 0, fmt.Errorf("StarArchive is not hashable") }
func (*StarArchive) Truth() starlark.Bool  { return starlark.True }
func (*StarArchive) Freeze()               {}

var (
	_ starlark.Value    = &StarArchive{}
	_ starlark.Iterable = &StarArchive{}
	_ starlark.Mapping  = &StarArchive{}
)
