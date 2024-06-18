package builder

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/tinyrange/pkg2/v2/common"
	"github.com/tinyrange/pkg2/v2/filesystem"
	"github.com/tinyrange/pkg2/v2/record"
	"go.starlark.net/starlark"
)

type BuildContext struct {
	Source   common.BuildSource
	database common.PackageDatabase
	parent   *BuildContext
	status   *common.BuildStatus

	filename string
	output   io.WriteCloser
	packages []*common.Package
	inMemory bool
}

// FileFromDigest implements common.BuildContext.
func (b *BuildContext) FileFromDigest(digest *filesystem.FileDigest) (filesystem.File, error) {
	if digest.Hash != "" {
		return &filesystem.LocalFile{Filename: digest.Hash}, nil
	}

	return nil, fmt.Errorf("could not convert digest to hash")
}

// IsInMemory implements common.BuildContext.
func (b *BuildContext) IsInMemory() bool {
	return b.inMemory
}

// SetInMemory implements common.BuildContext.
func (b *BuildContext) SetInMemory() {
	b.inMemory = true
}

// Packages implements common.BuildContext.
func (b *BuildContext) Packages() []*common.Package {
	return b.packages
}

// Database implements common.BuildContext.
func (b *BuildContext) Database() common.PackageDatabase {
	return b.database
}

func (b *BuildContext) ChildContext(source common.BuildSource, status *common.BuildStatus, filename string) common.BuildContext {
	return &BuildContext{
		parent:   b,
		filename: filename,
		output:   nil,
		status:   status,
		Source:   source,
		database: b.database,
		inMemory: b.inMemory,
	}
}

func (b *BuildContext) createOutput() (io.WriteCloser, error) {
	if b.IsInMemory() {
		return nil, fmt.Errorf("pre-creating output for in-memory items is not implemented")
	}

	if b.output != nil {
		return nil, fmt.Errorf("output already created")
	}

	out, err := os.Create(b.filename)
	if err != nil {
		return nil, err
	}

	b.output = out

	return b.output, nil
}

func (b *BuildContext) HasCreatedOutput() bool {
	return b.output != nil
}

func (b *BuildContext) BuildChild(def common.BuildDefinition) (filesystem.File, error) {
	if b.status != nil {
		b.status.Children = append(b.status.Children, def)
	}

	return b.database.Build(b, def)
}

func (b *BuildContext) NeedsBuild(def common.BuildDefinition) (bool, error) {
	if b.inMemory {
		return true, nil
	}

	// TODO(joshua): This code should be merged with the Build method.

	hash := common.GetSha256Hash([]byte(def.Tag()))

	filename := filepath.Join("local", "build", hash+".bin")

	// Get a child context for the build.
	child := b.ChildContext(def, b.status, filename+".tmp")

	// Check if the file already exists. If it does then return it.
	if info, err := os.Stat(filename); err == nil {
		// If the file has already been created then check if a rebuild is needed.
		needsRebuild, err := def.NeedsBuild(child, info.ModTime())
		if err != nil {
			return false, err
		}

		return needsRebuild, nil
	}

	return true, nil
}

// Attr implements starlark.HasAttrs.
func (b *BuildContext) Attr(name string) (starlark.Value, error) {
	if name == "recordwriter" {
		return starlark.NewBuiltin("BuildContext.recordwriter", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			f, err := b.createOutput()
			if err != nil {
				return nil, err
			}

			return record.NewWriter(f), nil
		}), nil
	} else if name == "add_package" {
		return starlark.NewBuiltin("BuildContext.add_package", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				pkg *common.Package
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"pkg", &pkg,
			); err != nil {
				return starlark.None, err
			}

			b.packages = append(b.packages, pkg)

			return starlark.None, nil
		}), nil
	} else if name == "build" {
		return starlark.NewBuiltin("BuildContext.build", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				def common.BuildDefinition
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"def", &def,
			); err != nil {
				return starlark.None, err
			}

			result, err := b.BuildChild(def)
			if err != nil {
				return starlark.None, err
			}

			return BuildResultToStarlark(b, def, result)
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (b *BuildContext) AttrNames() []string {
	return []string{"recordwriter", "add_package", "build"}
}

func (ctx *BuildContext) newThread() *starlark.Thread {
	return &starlark.Thread{Name: ctx.Source.Tag()}
}

func (ctx *BuildContext) Call(target starlark.Callable, args ...starlark.Value) (starlark.Value, error) {
	result, err := starlark.Call(ctx.newThread(), target, append(starlark.Tuple{ctx}, args...), []starlark.Tuple{})
	if err != nil {
		return starlark.None, err
	}

	return result, nil
}

func (*BuildContext) String() string        { return "BuildContext" }
func (*BuildContext) Type() string          { return "BuildContext" }
func (*BuildContext) Hash() (uint32, error) { return 0, fmt.Errorf("BuildContext is not hashable") }
func (*BuildContext) Truth() starlark.Bool  { return starlark.True }
func (*BuildContext) Freeze()               {}

var (
	_ starlark.Value      = &BuildContext{}
	_ starlark.HasAttrs   = &BuildContext{}
	_ common.BuildContext = &BuildContext{}
)

func NewBuildContext(source common.BuildSource, db common.PackageDatabase) *BuildContext {
	return &BuildContext{Source: source, database: db}
}
