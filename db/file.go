package db

import (
	"compress/bzip2"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/tinyrange/pkg2/db/common"
	"github.com/tinyrange/pkg2/memtar"
	"go.starlark.net/starlark"
)

type StarFile struct {
	source common.FileSource
	name   string
	opener func() (io.ReadCloser, error)
	stat   func() (common.FileInfo, error)
}

// Source implements StarFileIf.
func (f *StarFile) Source() common.FileSource {
	return f.source
}

// Name implements StarFileIf.
func (f *StarFile) Name() string { return f.name }

// Open implements StarFileIf.
func (f *StarFile) Open() (io.ReadCloser, error) {
	return f.opener()
}

// SetName implements StarFileIf.
func (f *StarFile) SetName(name string) (common.StarFileIf, error) {
	return &StarFile{
		source: f.source,
		name:   name,
		opener: f.opener,
		stat:   f.stat,
	}, nil
}

// Stat implements StarFileIf.
func (f *StarFile) Stat() (common.FileInfo, error) {
	if f.stat != nil {
		return f.stat()
	} else {
		return nil, fmt.Errorf("%s: f.stat == nil", f)
	}
}

func starFileCommonAttrs(f common.StarFileIf, name string) (starlark.Value, error) {
	if name == "read" {
		return starlark.NewBuiltin("File.read", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			f, err := f.Open()
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

			fh, err := f.Open()
			if err != nil {
				return nil, err
			}

			reader, err := ReadArchive(fh, ext, stripComponents)
			if err != nil {
				return starlark.None, fmt.Errorf("failed to read archive: %s", err)
			}

			return &StarArchive{source: common.ExtractArchiveSource{
				Kind:            "ExtractArchive",
				Source:          f.Source(),
				Extension:       ext,
				StripComponents: stripComponents,
			}, r: reader, name: f.Name()}, nil
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
					common.DecompressSource{
						Kind:      "Decompress",
						Source:    f.Source(),
						Extension: ".gz",
					},
					strings.TrimSuffix(f.Name(), ext),
					func() (io.ReadCloser, error) {
						fh, err := f.Open()
						if err != nil {
							return nil, fmt.Errorf("failed to open file: %s", err)
						}

						return gzip.NewReader(fh)
					},
					nil,
				), nil
			} else if strings.HasSuffix(ext, ".bz2") {
				return NewFile(
					common.DecompressSource{
						Kind:      "Decompress",
						Source:    f.Source(),
						Extension: ".bz2",
					},
					strings.TrimSuffix(f.Name(), ext),
					func() (io.ReadCloser, error) {
						fh, err := f.Open()
						if err != nil {
							return nil, fmt.Errorf("failed to open file: %s", err)
						}

						return io.NopCloser(bzip2.NewReader(fh)), nil
					},
					nil,
				), nil
			} else if strings.HasSuffix(ext, ".zst") {
				return NewFile(
					common.DecompressSource{
						Kind:      "Decompress",
						Source:    f.Source(),
						Extension: ".zst",
					},
					strings.TrimSuffix(f.Name(), ext),
					func() (io.ReadCloser, error) {
						fh, err := f.Open()
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
			fh, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open file: %s", err)
			}
			defer fh.Close()

			return rpmReadXml(thread, fh)
		}), nil
	} else if name == "hash" {
		return starlark.NewBuiltin("File.hash", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				algorithm string
			)

			if err := starlark.UnpackArgs("File.hash", args, kwargs,
				"algorithm", &algorithm,
			); err != nil {
				return starlark.None, err
			}

			fh, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open file: %s", err)
			}
			defer fh.Close()

			if algorithm == "sha256" {
				h := sha256.New()

				if _, err := io.Copy(h, fh); err != nil {
					return nil, fmt.Errorf("failed to hash file: %s", err)
				}

				digest := hex.EncodeToString(h.Sum(nil))

				return starlark.String(digest), nil
			} else {
				return starlark.None, fmt.Errorf("unknown hash algorithm: %s", algorithm)
			}
		}), nil
	} else if name == "name" {
		return starlark.String(f.Name()), nil
	} else if name == "base" {
		return starlark.String(path.Base(f.Name())), nil
	} else if name == "size" {
		info, err := f.Stat()
		if err != nil {
			return starlark.None, err
		}

		return starlark.MakeInt64(info.Size()), nil
	} else {
		return nil, nil
	}
}

func starFileCommonAttrNames(f common.StarFileIf) []string {
	return []string{"read", "read_archive", "read_compressed", "read_rpm_xml", "hash", "name", "base", "size"}
}

// Attr implements starlark.HasAttrs.
func (f *StarFile) Attr(name string) (starlark.Value, error) {
	return starFileCommonAttrs(f, name)
}

// AttrNames implements starlark.HasAttrs.
func (f *StarFile) AttrNames() []string {
	return starFileCommonAttrNames(f)
}

func (f *StarFile) String() string      { return fmt.Sprintf("File{%s}", f.name) }
func (*StarFile) Type() string          { return "File" }
func (*StarFile) Hash() (uint32, error) { return 0, fmt.Errorf("File is not hashable") }
func (*StarFile) Truth() starlark.Bool  { return starlark.True }
func (*StarFile) Freeze()               {}

var (
	_ starlark.Value    = &StarFile{}
	_ starlark.HasAttrs = &StarFile{}
	_ common.StarFileIf = &StarFile{}
)

func NewFile(
	source common.FileSource,
	name string,
	opener func() (io.ReadCloser, error),
	stat func() (common.FileInfo, error),
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
	}, func() (common.FileInfo, error) { return memtar.FileInfoFromEntry(ent) })

	it.index += 1

	return true
}

var (
	_ starlark.Iterator = &StarArchiveIterator{}
)

type StarArchive struct {
	source common.FileSource
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
			}, func() (common.FileInfo, error) { return memtar.FileInfoFromEntry(ent) }), true, nil
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
