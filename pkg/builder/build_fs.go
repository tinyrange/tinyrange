package builder

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	hash.RegisterType(&buildFsDefinition{})
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
func (i *initRamFsBuilderResult) WriteResult(w io.Writer) error {
	writer := cpio.New()

	for _, frag := range i.frags {
		if frag.Archive != nil {
			f := filesystem.NewLocalFile(frag.Archive.HostFilename, nil)

			ark, err := filesystem.ReadArchiveFromFile(f)
			if err != nil {
				return err
			}

			ents, err := ark.Entries()
			if err != nil {
				return err
			}

			for _, ent := range ents {
				if err := writer.AddFromEntry(frag.Archive.Target, ent); err != nil {
					return err
				}
			}
		} else if frag.FileContents != nil {
			c := frag.FileContents

			filename := strings.TrimPrefix(c.GuestFilename, "/")

			if err := writer.AddSimpleFile(filename, c.Contents, c.Executable); err != nil {
				return fmt.Errorf("failed to add simple file: %s", c.GuestFilename)
			}
		} else if frag.Builtin != nil {
			c := frag.Builtin

			if c.Name == "init" {
				buf, err := initExec.GetInitExecutable(c.Architecture)
				if err != nil {
					return err
				}

				if err := writer.AddSimpleFile(c.GuestFilename, buf, true); err != nil {
					return fmt.Errorf("failed to add simple file: %s", c.GuestFilename)
				}
			} else {
				return fmt.Errorf("unhandled builtin: %s", c.Name)
			}
		} else {
			return fmt.Errorf("unhandled fragment type: %+v", frag)
		}
	}

	if _, err := writer.WriteTo(w); err != nil {
		return err
	}

	return nil
}

var (
	_ common.BuildResult = &initRamFsBuilderResult{}
)

type tarBuilderResult struct {
	frags []config.Fragment
}

// WriteTo implements common.BuildResult.
func (i *tarBuilderResult) WriteResult(w io.Writer) error {
	writer := tar.NewWriter(w)

	written := make(map[string]bool)

	var commands []string

	for _, frag := range i.frags {
		if frag.Archive != nil {
			f := filesystem.NewLocalFile(frag.Archive.HostFilename, nil)

			ark, err := filesystem.ReadArchiveFromFile(f)
			if err != nil {
				return err
			}

			ents, err := ark.Entries()
			if err != nil {
				return err
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
					return err
				}

				if ent.Typeflag() == filesystem.TypeRegular {
					fh, err := ent.Open()
					if err != nil {
						return err
					}
					defer fh.Close()

					if _, err := io.Copy(writer, fh); err != nil {
						return err
					}
				}

				written[ent.Name()] = true
			}
		} else if frag.Builtin != nil {
			c := frag.Builtin

			if c.Name == "init" {
				buf, err := initExec.GetInitExecutable(c.Architecture)
				if err != nil {
					return err
				}

				if err := writer.WriteHeader(&tar.Header{
					Typeflag: tar.TypeReg,
					Name:     c.GuestFilename,
					Size:     int64(len(buf)),
					Mode:     0755,
					Uid:      0,
					Gid:      0,
					ModTime:  time.UnixMilli(0),
				}); err != nil {
					return err
				}

				if _, err := writer.Write(buf); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("unhandled builtin: %s", c.Name)
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
				return err
			}

			if _, err := writer.Write(buf); err != nil {
				return err
			}
		} else if frag.RunCommand != nil {
			commands = append(commands, frag.RunCommand.Command)
		} else {
			return fmt.Errorf("unhandled fragment type: %+v", frag)
		}
	}

	commandsJson, err := json.Marshal(commands)
	if err != nil {
		return err
	}

	if err := writer.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     "/init.commands.json",
		Size:     int64(len(commandsJson)),
		Mode:     int64(os.ModePerm),
		Uid:      0,
		Gid:      0,
		ModTime:  time.UnixMilli(0),
	}); err != nil {
		return err
	}

	if _, err := writer.Write(commandsJson); err != nil {
		return err
	}

	return nil
}

var (
	_ common.BuildResult = &tarBuilderResult{}
)

type buildFsDefinition struct {
	params BuildFsParameters

	frags []config.Fragment
}

// implements common.BuildDefinition.
func (def *buildFsDefinition) Params() hash.SerializableValue { return def.params }
func (def *buildFsDefinition) SerializableType() string       { return "BuildFsDefinition" }
func (def *buildFsDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &buildFsDefinition{params: params.(BuildFsParameters)}
}

// ToStarlark implements common.BuildDefinition.
func (def *buildFsDefinition) ToStarlark(ctx common.BuildContext1, artifact common.BuildArtifact) (starlark.Value, error) {
	result, err := artifact.Default()
	if err != nil {
		return nil, err
	}

	return filesystem.NewStarFile(result, artifact.DefinitionHash().String()), nil
}

// Build implements common.BuildDefinition.
func (def *buildFsDefinition) Build(ctx common.BuildContext1) error {
	// Launch child builds for each directive.
	for _, directive := range def.params.Directives {
		frags, err := directive.AsFragments(ctx, common.SpecialDirectiveHandlers{})
		if err != nil {
			return err
		}

		def.frags = append(def.frags, frags...)
	}

	if def.params.Kind == "initramfs" {
		return ctx.WriteDefault(&initRamFsBuilderResult{frags: def.frags})
	} else if def.params.Kind == "tar" {
		return ctx.WriteDefault(&tarBuilderResult{frags: def.frags})
	} else {
		return fmt.Errorf("kind not implemented: %s", def.params.Kind)
	}
}

// NeedsBuild implements common.BuildDefinition.
func (def *buildFsDefinition) NeedsBuild(ctx common.BuildContext1) (bool, error) {
	if ctx.ShouldRebuildUserDefinitions() {
		return true, nil
	}

	// TODO(joshua): Check if any of the child directives
	return false, nil
}

func (def *buildFsDefinition) String() string { return "BuildFsDefinition" }
func (*buildFsDefinition) Type() string       { return "BuildFsDefinition" }
func (*buildFsDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("BuildFsDefinition is not hashable")
}
func (*buildFsDefinition) Truth() starlark.Bool { return starlark.True }
func (*buildFsDefinition) Freeze()              {}

var (
	_ starlark.Value          = &buildFsDefinition{}
	_ common.BuildDefinition1 = &buildFsDefinition{}
)

func newBuildFsDefinition(dir []common.Directive, kind string) common.StarBuildDefinition1 {
	return &buildFsDefinition{params: BuildFsParameters{Directives: dir, Kind: kind}}
}
