package db

import (
	"encoding/gob"
	"fmt"
	"path"

	"go.starlark.net/starlark"
)

func init() {
	gob.Register(ArchiveDirective{})
	gob.Register(DependencyDirective{})
	gob.Register(ScriptDirective{})
	gob.Register(OutputDirective{})
}

type BuilderDirective interface {
}

type ArchiveDirective struct {
	Kind           string // "Archive"
	Source         FileSource
	TargetFilename string
}

type DependencyDirective struct {
	Kind string // "Dependency"
	Name PackageName
}

type ScriptDirective struct {
	Kind             string // "Script"
	Contents         string
	WorkingDirectory string
}

type OutputDirective struct {
	Kind             string // "Output"
	Filename         string
	WorkingDirectory string
}

var (
	_ BuilderDirective = ArchiveDirective{}
	_ BuilderDirective = DependencyDirective{}
	_ BuilderDirective = ScriptDirective{}
	_ BuilderDirective = OutputDirective{}
)

type Builder struct {
	Name PackageName

	Directives []BuilderDirective
}

// Attr implements starlark.HasAttrs.
func (f *Builder) Attr(name string) (starlark.Value, error) {
	if name == "add_archive" {
		return starlark.NewBuiltin(name, func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				archive  *StarArchive
				filename string
			)

			if err := starlark.UnpackArgs("Builder.add_archive", args, kwargs,
				"archive", &archive,
				"filename?", &filename,
			); err != nil {
				return starlark.None, err
			}

			if filename == "" {
				filename = path.Base(archive.name)
			}

			filename = path.Join("/root", filename)

			f.Directives = append(f.Directives, ArchiveDirective{
				Kind:           "Archive",
				Source:         archive.source,
				TargetFilename: filename,
			})

			return starlark.String(filename), nil
		}), nil
	} else if name == "add_dependency" {
		return starlark.NewBuiltin(name, func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name PackageName
			)

			if err := starlark.UnpackArgs("Builder.add_dependency", args, kwargs,
				"name", &name,
			); err != nil {
				return starlark.None, err
			}

			f.Directives = append(f.Directives, DependencyDirective{
				Kind: "Dependency",
				Name: name,
			})

			return starlark.None, nil
		}), nil
	} else if name == "add_script" {
		return starlark.NewBuiltin(name, func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				script string
				cwd    string
			)

			if err := starlark.UnpackArgs("Builder.add_script", args, kwargs,
				"script", &script,
				"cwd?", &cwd,
			); err != nil {
				return starlark.None, err
			}

			f.Directives = append(f.Directives, ScriptDirective{
				Kind:             "Script",
				Contents:         script,
				WorkingDirectory: cwd,
			})

			return starlark.None, nil
		}), nil
	} else if name == "add_output" {
		return starlark.NewBuiltin(name, func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				filename string
				cwd      string
			)

			if err := starlark.UnpackArgs("Builder.add_output", args, kwargs,
				"filename", &filename,
				"cwd?", &cwd,
			); err != nil {
				return starlark.None, err
			}

			f.Directives = append(f.Directives, OutputDirective{
				Kind:             "Output",
				Filename:         filename,
				WorkingDirectory: cwd,
			})

			return starlark.None, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (f *Builder) AttrNames() []string {
	return []string{"add_archive", "add_dependency", "add_script", "add_output"}
}

func (f *Builder) String() string      { return fmt.Sprintf("Builder{%s}", f.Name) }
func (*Builder) Type() string          { return "Builder" }
func (*Builder) Hash() (uint32, error) { return 0, fmt.Errorf("Builder is not hashable") }
func (*Builder) Truth() starlark.Bool  { return starlark.True }
func (*Builder) Freeze()               {}

var (
	_ starlark.Value    = &Builder{}
	_ starlark.HasAttrs = &Builder{}
)

func NewBuilder(name PackageName) *Builder {
	return &Builder{Name: name}
}
