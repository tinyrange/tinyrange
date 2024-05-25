package db

import (
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/tinyrange/pkg2/memtar"
	"go.starlark.net/starlark"
)

type StarFile struct {
	source FileSource
	name   string
	opener func() (io.ReadCloser, error)
	stat   func() (FileInfo, error)
}

// Name implements StarFileIf.
func (f *StarFile) Name() string { return f.name }

// Open implements StarFileIf.
func (f *StarFile) Open() (io.ReadCloser, error) {
	return f.opener()
}

// SetName implements StarFileIf.
func (f *StarFile) SetName(name string) (StarFileIf, error) {
	return &StarFile{
		source: f.source,
		name:   name,
		opener: f.opener,
		stat:   f.stat,
	}, nil
}

// Stat implements StarFileIf.
func (f *StarFile) Stat() (FileInfo, error) {
	if f.stat != nil {
		return f.stat()
	} else {
		return nil, fmt.Errorf("%s: f.stat == nil", f)
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
			f, err := f.opener()
			if err != nil {
				return nil, err
			}
			defer f.Close()

			contents, err := io.ReadAll(f)
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
				ext             string
				stripComponents int
			)

			if err := starlark.UnpackArgs("File.read_archive", args, kwargs,
				"ext", &ext,
				"strip_components?", &stripComponents,
			); err != nil {
				return starlark.None, err
			}

			fh, err := f.opener()
			if err != nil {
				return nil, err
			}

			reader, err := ReadArchive(fh, ext, stripComponents)
			if err != nil {
				return starlark.None, fmt.Errorf("failed to read archive: %s", err)
			}

			return &StarArchive{source: ExtractArchiveSource{
				Kind:            "ExtractArchive",
				Source:          f.source,
				Extension:       ext,
				StripComponents: stripComponents,
			}, r: reader, name: f.name}, nil
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
				return NewFile(
					DecompressSource{
						Kind:      "Decompress",
						Source:    f.source,
						Extension: ".gz",
					},
					strings.TrimSuffix(f.name, ext),
					func() (io.ReadCloser, error) {
						fh, err := f.opener()
						if err != nil {
							return nil, fmt.Errorf("failed to open file: %s", err)
						}

						return gzip.NewReader(fh)
					},
					nil,
				), nil
			} else if strings.HasSuffix(ext, ".bz2") {
				return NewFile(
					DecompressSource{
						Kind:      "Decompress",
						Source:    f.source,
						Extension: ".bz2",
					},
					strings.TrimSuffix(f.name, ext),
					func() (io.ReadCloser, error) {
						fh, err := f.opener()
						if err != nil {
							return nil, fmt.Errorf("failed to open file: %s", err)
						}

						return io.NopCloser(bzip2.NewReader(fh)), nil
					},
					nil,
				), nil
			} else if strings.HasSuffix(ext, ".zst") {
				return NewFile(
					DecompressSource{
						Kind:      "Decompress",
						Source:    f.source,
						Extension: ".zst",
					},
					strings.TrimSuffix(f.name, ext),
					func() (io.ReadCloser, error) {
						fh, err := f.opener()
						if err != nil {
							return nil, fmt.Errorf("failed to open file: %s", err)
						}

						r, err := zstd.NewReader(fh)
						if err != nil {
							return nil, err
						}

						return r.IOReadCloser(), nil
					},
					nil,
				), nil
			} else {
				return starlark.None, fmt.Errorf("unsupported extension: %s", ext)
			}
		}), nil
	} else if name == "read_rpm_xml" {
		return starlark.NewBuiltin("File.read_rpm_xml", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			fh, err := f.opener()
			if err != nil {
				return nil, fmt.Errorf("failed to open file: %s", err)
			}
			defer fh.Close()

			return rpmReadXml(thread, fh)
		}), nil
	} else if name == "name" {
		return starlark.String(f.name), nil

	} else if name == "base" {
		return starlark.String(path.Base(f.name)), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (*StarFile) AttrNames() []string {
	return []string{"read", "read_archive", "read_compressed", "read_rpm_xml", "name", "base"}
}

func (f *StarFile) String() string      { return fmt.Sprintf("File{%s}", f.name) }
func (*StarFile) Type() string          { return "File" }
func (*StarFile) Hash() (uint32, error) { return 0, fmt.Errorf("File is not hashable") }
func (*StarFile) Truth() starlark.Bool  { return starlark.True }
func (*StarFile) Freeze()               {}

var (
	_ starlark.Value    = &StarFile{}
	_ starlark.HasAttrs = &StarFile{}
	_ StarFileIf        = &StarFile{}
)

func NewFile(
	source FileSource,
	name string,
	opener func() (io.ReadCloser, error),
	stat func() (FileInfo, error),
) *StarFile {
	return &StarFile{
		source: source,
		name:   name,
		opener: opener,
		stat:   stat,
	}
}

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

	*p = NewFile(nil, ent.Filename(), func() (io.ReadCloser, error) {
		return io.NopCloser(ent.Open()), nil
	}, func() (FileInfo, error) { return memtar.FileInfoFromEntry(ent) })

	it.index += 1

	return true
}

var (
	_ starlark.Iterator = &StarArchiveIterator{}
)

type StarArchive struct {
	source FileSource
	r      memtar.TarReader
	name   string
}

// Entries implements memtar.TarReader.
func (ar *StarArchive) Entries() []memtar.Entry {
	return ar.r.Entries()
}

// Get implements starlark.Mapping.
func (ar *StarArchive) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	filename, _ := starlark.AsString(k)

	for _, ent := range ar.r.Entries() {
		if ent.Filename() == filename {
			return NewFile(nil, ent.Filename(), func() (io.ReadCloser, error) {
				return io.NopCloser(ent.Open()), nil
			}, func() (FileInfo, error) { return memtar.FileInfoFromEntry(ent) }), true, nil
		}
	}

	return starlark.None, false, nil
}

// Iterate implements starlark.Iterable.
func (ar *StarArchive) Iterate() starlark.Iterator {
	return &StarArchiveIterator{ents: ar.r.Entries()}
}

func (f *StarArchive) String() string      { return fmt.Sprintf("Archive{%s}", f.name) }
func (*StarArchive) Type() string          { return "StarArchive" }
func (*StarArchive) Hash() (uint32, error) { return 0, fmt.Errorf("StarArchive is not hashable") }
func (*StarArchive) Truth() starlark.Bool  { return starlark.True }
func (*StarArchive) Freeze()               {}

var (
	_ starlark.Value    = &StarArchive{}
	_ starlark.Iterable = &StarArchive{}
	_ starlark.Mapping  = &StarArchive{}
	_ memtar.TarReader  = &StarArchive{}
)
