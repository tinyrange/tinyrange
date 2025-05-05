package star

import (
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"strings"

	xj "github.com/basgys/goxml2json"
	"github.com/tinyrange/tinyrange/pkg/archive"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/fsutil"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/log"
	"github.com/tinyrange/tinyrange/pkg/path"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
)

var starlarkJsonDecode = starlarkjson.Module.Members["decode"].(*starlark.Builtin).CallInternal

func AsFile(f any) (filesystem.File, error) {
	switch f := f.(type) {
	case *StarFile:
		return f.File, nil
	case filesystem.File:
		return f, nil
	case starlark.Value:
		str, ok := starlark.AsString(f)
		if !ok {
			return nil, fmt.Errorf("could not convert %s to File", f.Type())
		}

		ret := filesystem.NewMemoryFile(filesystem.TypeRegular)

		if err := ret.Overwrite([]byte(str)); err != nil {
			return nil, err
		}

		return ret, nil
	default:
		return nil, fmt.Errorf("could not convert %T to File", f)
	}
}

type StarFile struct {
	filesystem.File
	Name string
}

// Source implements filesystem.HasSource.
func (f *StarFile) Source() (hash.SerializableValue, error) {
	return filesystem.SourceFromFile(f.File)
}

// LinkName implements filesystem.HasLinkName.
func (f *StarFile) LinkName() (string, error) {
	return filesystem.GetLinkName(f.File)
}

// UidAndGid implements filesystem.HasUidAndGid.
func (f *StarFile) UidAndGid() (int, int, error) {
	return filesystem.GetUidAndGid(f.File)
}

// AsSerializableValue implements hash.ValueCaster.
func (f *StarFile) AsSerializableValue() (hash.SerializableValue, error) {
	switch val := f.File.(type) {
	default:
		return nil, fmt.Errorf("StarFile.AsSerializableValue unimplemented: %T %+v", val, val)
	}
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
			ark, err := archive.ReadArchiveFromFile(f)
			if err != nil {
				return starlark.None, nil
			}

			return NewStarArchive(ark, nil, f.Name), nil
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

			if strings.HasSuffix(kind, ".gz") {
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
	} else if name == "read_xml" {
		return starlark.NewBuiltin("File.read_xml", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			fh, err := f.Open()
			if err != nil {
				return starlark.None, err
			}
			defer fh.Close()

			json, err := xj.Convert(fh)
			if err != nil {
				return starlark.None, err
			}

			return starlarkJsonDecode(
				thread,
				starlark.Tuple{starlark.String(json.String())},
				[]starlark.Tuple{},
			)
		}), nil
	} else if name == "read_rpm_xml" {
		return starlark.NewBuiltin("File.read_rpm_xml", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			fh, err := f.Open()
			if err != nil {
				return starlark.None, err
			}
			defer fh.Close()

			return rpmReadXml(fh)
		}), nil
	} else if name == "read_rpm" {
		return starlark.NewBuiltin("File.read_rpm", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			fh, err := f.Open()
			if err != nil {
				return starlark.None, err
			}

			return parseRpm(fh)
		}), nil
	} else if name == "name" {
		return starlark.String(f.Name), nil
	} else if name == "base" {
		return starlark.String(path.Unix.Base(f.Name)), nil
	} else if name == "dir" {
		return starlark.String(path.Unix.Dir(f.Name)), nil
	}

	if mut, ok := f.File.(filesystem.MutableFile); ok {
		_ = mut
	}

	return nil, nil
}

// AttrNames implements starlark.HasAttrs.
func (f *StarFile) AttrNames() []string {
	ret := []string{"read", "read_archive", "name", "base", "dir"}

	if _, ok := f.File.(filesystem.MutableFile); ok {
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
	_ starlark.Value          = &StarFile{}
	_ starlark.HasAttrs       = &StarFile{}
	_ filesystem.HasUidAndGid = &StarFile{}
	_ filesystem.HasLinkName  = &StarFile{}
	_ filesystem.HasSource    = &StarFile{}
	_ hash.ValueCaster        = &StarFile{}
)

func NewStarFile(f filesystem.File, name string) *StarFile {
	return &StarFile{File: f, Name: name}
}

type archiveIterator struct {
	ents []filesystem.Entry
	i    int
}

// Done implements starlark.Iterator.
func (a *archiveIterator) Done() {
	a.i = len(a.ents)
}

// Next implements starlark.Iterator.
func (a *archiveIterator) Next(p *starlark.Value) bool {
	if a.i == len(a.ents) {
		return false
	}

	ent := a.ents[a.i]

	*p = NewStarFile(ent, ent.Name())

	a.i += 1

	return true
}

var (
	_ starlark.Iterator = &archiveIterator{}
)

type StarArchive struct {
	filesystem.Archive
	Source hash.SerializableValue
	Name   string
}

// Iterate implements starlark.Iterable.
func (f *StarArchive) Iterate() starlark.Iterator {
	ents, err := f.Entries()
	if err != nil {
		log.Error("could not get entries", "err", err)
		return nil
	}

	return &archiveIterator{ents: ents}
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
	_ starlark.Value    = &StarArchive{}
	_ starlark.Mapping  = &StarArchive{}
	_ starlark.Iterable = &StarArchive{}
)

func NewStarArchive(ark filesystem.Archive, source hash.SerializableValue, name string) *StarArchive {
	return &StarArchive{Archive: ark, Source: source, Name: name}
}

type starDirectoryIterator struct {
	name string
	ents []filesystem.DirectoryEntry
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

	childName := path.Unix.Join(s.name, ent.Name)

	if dir, ok := ent.File.(filesystem.Directory); ok {
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
	filesystem.Directory
}

// LinkName implements filesystem.HasLinkName.
func (f *StarDirectory) LinkName() (string, error) {
	return filesystem.GetLinkName(f.Directory)
}

// UidAndGid implements filesystem.HasUidAndGid.
func (f *StarDirectory) UidAndGid() (int, int, error) {
	return filesystem.GetUidAndGid(f.Directory)
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

	ent, err := fsutil.OpenPath(f, name)
	if err == fs.ErrNotExist {
		return nil, false, nil
	} else if err != nil {
		return nil, false, err
	}

	childName := path.Unix.Join(f.Name, ent.Name)

	if dir, ok := ent.File.(filesystem.Directory); ok {
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

	if file, ok := v.(filesystem.File); ok {
		if _, err := fsutil.CreateChild(f, name, file); err != nil {
			return err
		}

		return nil
	} else if contents, ok := v.(starlark.String); ok {
		file := filesystem.NewMemoryFile(filesystem.TypeRegular)

		if err := file.Overwrite([]byte(contents)); err != nil {
			return err
		}

		if _, err := fsutil.CreateChild(f, name, file); err != nil {
			return err
		}

		return nil
	} else {
		return fmt.Errorf("expected File got %s", v.Type())
	}

}

// Attr implements starlark.HasAttrs.
func (f *StarDirectory) Attr(name string) (starlark.Value, error) {
	if name == "name" {
		return starlark.String(f.Name), nil
	} else if name == "base" {
		return starlark.String(path.Unix.Base(f.Name)), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (f *StarDirectory) AttrNames() []string {
	return []string{"name", "base"}
}

func (f *StarDirectory) AsMutableDirectory() filesystem.MutableDirectory {
	mut, ok := f.Directory.(filesystem.MutableDirectory)
	if !ok {
		return nil
	}

	return mut
}

func (f *StarDirectory) String() string      { return fmt.Sprintf("Directory{%s}", f.Name) }
func (*StarDirectory) Type() string          { return "Directory" }
func (*StarDirectory) Hash() (uint32, error) { return 0, fmt.Errorf("Directory is not hashable") }
func (*StarDirectory) Truth() starlark.Bool  { return starlark.True }
func (*StarDirectory) Freeze()               {}

var (
	_ starlark.Value                = &StarDirectory{}
	_ starlark.HasAttrs             = &StarDirectory{}
	_ starlark.Mapping              = &StarDirectory{}
	_ starlark.HasSetKey            = &StarDirectory{}
	_ starlark.Iterable             = &StarDirectory{}
	_ filesystem.AsMutableDirectory = &StarDirectory{}
	_ filesystem.HasLinkName        = &StarDirectory{}
	_ filesystem.HasUidAndGid       = &StarDirectory{}
)

func NewStarDirectory(dir filesystem.Directory, name string) *StarDirectory {
	return &StarDirectory{Directory: dir, Name: name}
}
