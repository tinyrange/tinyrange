package builder

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/cpio"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	initExec "github.com/tinyrange/tinyrange/pkg/init"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&BuildFsDefinition{})
}

func toTarTypeFlag(flag filesystem.FileType) byte {
	switch flag {
	case filesystem.TypeDirectory:
		return tar.TypeDir
	case filesystem.TypeRegular:
		return tar.TypeReg
	case filesystem.TypeSymlink:
		return tar.TypeSymlink
	case filesystem.TypeLink:
		return tar.TypeLink
	default:
		panic(fmt.Sprintf("unimplemented type: %s", flag))
	}
}

type initRamFsBuilderResult struct {
	frags []config.Fragment
}

// WriteTo implements common.BuildResult.
func (i *initRamFsBuilderResult) WriteTo(w io.Writer) (n int64, err error) {
	writer := cpio.New()

	for _, frag := range i.frags {
		if frag.Archive != nil {
			f := filesystem.NewLocalFile(frag.Archive.HostFilename, nil)

			ark, err := filesystem.ReadArchiveFromFile(f)
			if err != nil {
				return 0, err
			}

			ents, err := ark.Entries()
			if err != nil {
				return 0, err
			}

			for _, ent := range ents {
				if err := writer.AddFromEntry(frag.Archive.Target, ent); err != nil {
					return 0, err
				}
			}
		} else if frag.FileContents != nil {
			c := frag.FileContents

			filename := strings.TrimPrefix(c.GuestFilename, "/")

			if err := writer.AddSimpleFile(filename, c.Contents, c.Executable); err != nil {
				return 0, fmt.Errorf("failed to add simple file: %s", c.GuestFilename)
			}
		} else if frag.Builtin != nil {
			c := frag.Builtin

			if c.Name == "init" {
				buf := initExec.INIT_EXECUTABLE

				if err := writer.AddSimpleFile(c.GuestFilename, buf, true); err != nil {
					return 0, fmt.Errorf("failed to add simple file: %s", c.GuestFilename)
				}
			} else {
				return 0, fmt.Errorf("unhandled builtin: %s", c.Name)
			}
		} else {
			return 0, fmt.Errorf("unhandled fragment type: %+v", frag)
		}
	}

	return writer.WriteTo(w)
}

var (
	_ common.BuildResult = &initRamFsBuilderResult{}
)

type tarBuilderResult struct {
	frags []config.Fragment
}

// WriteTo implements common.BuildResult.
func (i *tarBuilderResult) WriteTo(w io.Writer) (n int64, err error) {
	writer := tar.NewWriter(w)

	written := make(map[string]bool)

	for _, frag := range i.frags {
		if frag.Archive != nil {
			f := filesystem.NewLocalFile(frag.Archive.HostFilename, nil)

			ark, err := filesystem.ReadArchiveFromFile(f)
			if err != nil {
				return 0, err
			}

			ents, err := ark.Entries()
			if err != nil {
				return 0, err
			}

			for _, ent := range ents {
				name := ent.Name()

				if name == "" {
					name = "."
				}

				if _, ok := written[name]; ok {
					continue
				}

				if err := writer.WriteHeader(&tar.Header{
					Typeflag: toTarTypeFlag(ent.Typeflag()),
					Name:     name,
					Linkname: ent.Linkname(),
					Size:     ent.Size(),
					Mode:     int64(ent.Mode()),
					Uid:      ent.Uid(),
					Gid:      ent.Gid(),
					ModTime:  ent.ModTime(),
					Devmajor: ent.Devmajor(),
					Devminor: ent.Devminor(),
				}); err != nil {
					return 0, err
				}

				if ent.Typeflag() == filesystem.TypeRegular {
					fh, err := ent.Open()
					if err != nil {
						return 0, err
					}
					defer fh.Close()

					if _, err := io.Copy(writer, fh); err != nil {
						return 0, err
					}
				}

				written[ent.Name()] = true
			}
		} else if frag.Builtin != nil {
			c := frag.Builtin

			if c.Name == "init" {
				buf := initExec.INIT_EXECUTABLE

				if err := writer.WriteHeader(&tar.Header{
					Typeflag: tar.TypeReg,
					Name:     c.GuestFilename,
					Size:     int64(len(buf)),
					Mode:     0755,
					Uid:      0,
					Gid:      0,
					ModTime:  time.UnixMilli(0),
				}); err != nil {
					return 0, err
				}

				if _, err := writer.Write(buf); err != nil {
					return 0, err
				}
			} else {
				return 0, fmt.Errorf("unhandled builtin: %s", c.Name)
			}
		} else if frag.FileContents != nil {
			c := frag.FileContents

			buf := c.Contents

			var mode int64 = 0644

			if c.Executable {
				mode = 0755
			}

			if err := writer.WriteHeader(&tar.Header{
				Typeflag: tar.TypeReg,
				Name:     c.GuestFilename,
				Size:     int64(len(buf)),
				Mode:     mode,
				Uid:      0,
				Gid:      0,
				ModTime:  time.UnixMilli(0),
			}); err != nil {
				return 0, err
			}

			if _, err := writer.Write(buf); err != nil {
				return 0, err
			}
		} else {
			return 0, fmt.Errorf("unhandled fragment type: %+v", frag)
		}
	}

	return 0, nil
}

var (
	_ common.BuildResult = &tarBuilderResult{}
)

type FragmentsBuilderResult struct {
	Fragments []config.Fragment
}

// WriteTo implements common.BuildResult.
func (frags *FragmentsBuilderResult) WriteTo(w io.Writer) (n int64, err error) {
	buf := new(bytes.Buffer)

	enc := json.NewEncoder(buf)

	if err := enc.Encode(&frags); err != nil {
		return 0, err
	}

	return io.Copy(w, buf)
}

var (
	_ common.BuildResult = &FragmentsBuilderResult{}
)

func ParseFragmentsBuilderResult(f filesystem.File) (*FragmentsBuilderResult, error) {
	var ret FragmentsBuilderResult

	if err := ParseJsonFromFile(f, &ret); err != nil {
		return nil, err
	}

	return &ret, nil
}

type BuildFsDefinition struct {
	params BuildFsParameters

	frags []config.Fragment
}

// Dependencies implements common.BuildDefinition.
func (def *BuildFsDefinition) Dependencies(ctx common.BuildContext) ([]common.DependencyNode, error) {
	var ret []common.DependencyNode

	for _, directive := range def.params.Directives {
		ret = append(ret, directive)
	}

	return ret, nil
}

// implements common.BuildDefinition.
func (def *BuildFsDefinition) Params() hash.SerializableValue { return def.params }
func (def *BuildFsDefinition) SerializableType() string       { return "BuildFsDefinition" }
func (def *BuildFsDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &BuildFsDefinition{params: params.(BuildFsParameters)}
}

// ToStarlark implements common.BuildDefinition.
func (def *BuildFsDefinition) ToStarlark(ctx common.BuildContext, result filesystem.File) (starlark.Value, error) {
	return filesystem.NewStarFile(result, def.Tag()), nil
}

// Build implements common.BuildDefinition.
func (def *BuildFsDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	// Launch child builds for each directive.
	for _, directive := range def.params.Directives {
		frags, err := directive.AsFragments(ctx)
		if err != nil {
			return nil, err
		}

		for _, frag := range frags {
			if frag.RunCommand != nil && def.params.Kind != "fragments" {
				return nil, fmt.Errorf("build_fs does not support running commands")
			} else {
				def.frags = append(def.frags, frag)
			}
		}
	}

	if def.params.Kind == "initramfs" {
		return &initRamFsBuilderResult{frags: def.frags}, nil
	} else if def.params.Kind == "tar" {
		return &tarBuilderResult{frags: def.frags}, nil
	} else if def.params.Kind == "fragments" {
		return &FragmentsBuilderResult{Fragments: def.frags}, nil
	} else {
		return nil, fmt.Errorf("kind not implemented: %s", def.params.Kind)
	}
}

// NeedsBuild implements common.BuildDefinition.
func (def *BuildFsDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	if ctx.Database().ShouldRebuildUserDefinitions() {
		return true, nil
	}

	// TODO(joshua): Check if any of the child directives
	return false, nil
}

// Tag implements common.BuildDefinition.
func (def *BuildFsDefinition) Tag() string {
	out := []string{"BuildFs"}

	for _, dir := range def.params.Directives {
		out = append(out, dir.Tag())
	}

	out = append(out, def.params.Kind)

	return strings.Join(out, "_")
}

func (def *BuildFsDefinition) String() string { return def.Tag() }
func (*BuildFsDefinition) Type() string       { return "BuildFsDefinition" }
func (*BuildFsDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("BuildFsDefinition is not hashable")
}
func (*BuildFsDefinition) Truth() starlark.Bool { return starlark.True }
func (*BuildFsDefinition) Freeze()              {}

var (
	_ starlark.Value         = &BuildFsDefinition{}
	_ common.BuildDefinition = &BuildFsDefinition{}
)

func NewBuildFsDefinition(dir []common.Directive, kind string) *BuildFsDefinition {
	return &BuildFsDefinition{params: BuildFsParameters{Directives: dir, Kind: kind}}
}
