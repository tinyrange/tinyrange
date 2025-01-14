package database

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	initExec "github.com/tinyrange/tinyrange/pkg/init"
	"go.starlark.net/starlark"
)

type outputFile struct {
	f io.Writer
}

// Attr implements starlark.HasAttrs.
func (o *outputFile) Attr(name string) (starlark.Value, error) {
	if name == "write" {
		return starlark.NewBuiltin("OutputFile.write", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"contents", &contents,
			); err != nil {
				return starlark.None, err
			}

			if _, err := fmt.Fprintf(o.f, "%s", contents); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (o *outputFile) AttrNames() []string {
	return []string{"write"}
}

func (*outputFile) String() string        { return "OutputFile" }
func (*outputFile) Type() string          { return "OutputFile" }
func (*outputFile) Hash() (uint32, error) { return 0, fmt.Errorf("OutputFile is not hashable") }
func (*outputFile) Truth() starlark.Bool  { return starlark.True }
func (*outputFile) Freeze()               {}

var (
	_ starlark.Value    = &outputFile{}
	_ starlark.HasAttrs = &outputFile{}
)

type scriptArguments struct {
	args           map[string]starlark.Value
	outputFilename string
	additionalArgs []string
}

// Attr implements starlark.HasAttrs.
func (s *scriptArguments) Attr(name string) (starlark.Value, error) {
	if name == "output" {
		return starlark.NewBuiltin("Arguments.output", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			if s.outputFilename == "" {
				return starlark.None, fmt.Errorf("no output file specified. please specify one using the -o flag")
			}

			f, err := os.Create(s.outputFilename)
			if err != nil {
				return starlark.None, err
			}

			return &outputFile{f: f}, nil
		}), nil
	} else if name == "create_output" {
		return starlark.NewBuiltin("Arguments.create_output", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"name", &name,
			); err != nil {
				return starlark.None, err
			}

			if strings.ContainsAny(name, "/\\") {
				return starlark.None, fmt.Errorf("name for create_output can not contain path separators")
			}

			p := filepath.Join(s.outputFilename, name)

			f, err := os.Create(p)
			if err != nil {
				return starlark.None, err
			}

			return &outputFile{f: f}, nil
		}), nil
	} else if name == "args" {
		var ret []starlark.Value

		for _, arg := range s.additionalArgs {
			ret = append(ret, starlark.String(arg))
		}

		return starlark.NewList(ret), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (s *scriptArguments) AttrNames() []string {
	return []string{"output", "args"}
}

// Get implements starlark.Mapping.
func (s *scriptArguments) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	key, ok := starlark.AsString(k)
	if !ok {
		return nil, false, fmt.Errorf("expected string got %s", k.Type())
	}

	val, ok := s.args[key]
	if !ok {
		return nil, false, nil
	}

	return val, true, nil
}

func (*scriptArguments) String() string        { return "Arguments" }
func (*scriptArguments) Type() string          { return "Arguments" }
func (*scriptArguments) Hash() (uint32, error) { return 0, fmt.Errorf("Arguments is not hashable") }
func (*scriptArguments) Truth() starlark.Bool  { return starlark.True }
func (*scriptArguments) Freeze()               {}

var (
	_ starlark.Value    = &scriptArguments{}
	_ starlark.Mapping  = &scriptArguments{}
	_ starlark.HasAttrs = &scriptArguments{}
)

type packageDatabaseValue struct {
	*packageDatabase
}

// Attr implements starlark.HasAttrs.
func (db *packageDatabaseValue) Attr(name string) (starlark.Value, error) {
	if name == "add_mirror" {
		return starlark.NewBuiltin("Database.add_mirror", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name       string
				mirrorsVal starlark.Iterable
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"name", &name,
				"mirrors", &mirrorsVal,
			); err != nil {
				return starlark.None, err
			}

			mirrors, err := common.ToStringList(mirrorsVal)
			if err != nil {
				return starlark.None, err
			}

			return starlark.None, db.AddMirror(name, mirrors)
		}), nil
	} else if name == "add_container_builder" {
		return starlark.NewBuiltin("Database.add_container_builder", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				builder common.ContainerBuilder
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"builder", &builder,
			); err != nil {
				return starlark.None, err
			}

			return starlark.None, db.AddContainerBuilder(builder)
		}), nil
	} else if name == "build" {
		return starlark.NewBuiltin("Database.build", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				def           common.BuildDefinition1
				alwaysRebuild bool
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"def", &def,
				"always_rebuild?", &alwaysRebuild,
			); err != nil {
				return starlark.None, err
			}

			ctx := db.NewBuildContext(def)

			result, err := db.build(ctx, def, common.BuildOptions{
				AlwaysRebuild: alwaysRebuild,
			})
			if err != nil {
				return starlark.None, err
			}

			return def.ToStarlark(ctx, result)
		}), nil
	} else if name == "builder" {
		return starlark.NewBuiltin("Database.builder", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name       string
				archString string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"name", &name,
				"arch", &archString,
			); err != nil {
				return starlark.None, err
			}

			arch, err := config.ArchitectureFromString(archString)
			if err != nil {
				return starlark.None, err
			}

			builder, err := db.GetContainerBuilder(name, arch)
			if err != nil {
				return starlark.None, err
			}

			return builder, nil
		}), nil
	} else if name == "get_builtin_executable" {
		return starlark.NewBuiltin("Database.get_builtin_executable", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name string
				arch string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"name", &name,
				"arch", &arch,
			); err != nil {
				return starlark.None, err
			}

			if name == "init" {
				if config.CPUArchitecture(arch).IsNative() {
					f := filesystem.NewMemoryFile(filesystem.TypeRegular)
					f.Overwrite(initExec.INIT_EXECUTABLE)
					return filesystem.NewStarFile(f, "init"), nil
				} else {
					return starlark.None, fmt.Errorf("invalid architecture for init: %s", arch)
				}
			} else if name == "tinyrange" {
				// Assume that the user wants a Linux executable.
				if config.CPUArchitecture(arch).IsNative() && runtime.GOOS == "linux" {
					local, err := os.Executable()
					if err != nil {
						return nil, err
					}

					return filesystem.NewStarFile(filesystem.NewLocalFile(local, nil), "tinyrange"), nil
				} else {
					return starlark.None, fmt.Errorf("invalid architecture for tinyrange: %s", arch)
				}
			} else if name == "tinyrange_qemu" {
				local, err := common.GetAdjacentExecutable("tinyrange_qemu", "tinyqemu/tinyrange_qemu")
				if err != nil {
					return nil, err
				}

				return filesystem.NewStarFile(filesystem.NewLocalFile(local, nil), "tinyrange_qemu"), nil
			} else {
				return starlark.None, fmt.Errorf("unknown builtin executable: %s", name)
			}
		}), nil
	} else if name == "urls_for" {
		return starlark.NewBuiltin("Database.urls_for", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				url string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"url", &url,
			); err != nil {
				return starlark.None, err
			}

			urls, err := db.UrlsFor(url)
			if err != nil {
				return starlark.None, err
			}

			return starlark.String(urls[0]), nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (db *packageDatabaseValue) AttrNames() []string {
	return []string{"add_mirror", "add_container_builder", "build", "builder", "get_builtin_executable", "urls_for"}
}

func (*packageDatabaseValue) String() string        { return "Database" }
func (*packageDatabaseValue) Type() string          { return "Database" }
func (*packageDatabaseValue) Hash() (uint32, error) { return 0, fmt.Errorf("Database is not hashable") }
func (*packageDatabaseValue) Truth() starlark.Bool  { return starlark.True }
func (*packageDatabaseValue) Freeze()               {}

var (
	_ starlark.Value    = &packageDatabaseValue{}
	_ starlark.HasAttrs = &packageDatabaseValue{}
)
