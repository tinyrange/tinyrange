package builder

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/record"
	"go.starlark.net/starlark"
)

type BuildContext struct {
	source   common.BuildSource
	database common.PackageDatabase
	parent   *BuildContext
	status   *common.BuildStatus
	children []*BuildContext

	filename  string
	output    io.WriteCloser
	inMemory  bool
	hasCached bool
}

func (b *BuildContext) DisplayTree() {
	var dumpContext func(ctx *BuildContext, prefix string)

	dumpContext = func(ctx *BuildContext, prefix string) {
		fmt.Printf("%s%s\n", prefix, ctx.source)

		for _, child := range ctx.children {
			dumpContext(child, prefix+"  ")
		}
	}

	fmt.Printf("\n")

	dumpContext(b, "")
}

// SetHasCached implements common.BuildContext.
func (b *BuildContext) SetHasCached() {
	b.hasCached = true
}

// HasCached implements common.BuildContext.
func (b *BuildContext) HasCached() bool {
	return b.hasCached
}

// CreateFile implements common.BuildContext.
func (b *BuildContext) CreateFile(name string) (string, io.WriteCloser, error) {
	if b.IsInMemory() {
		return "", nil, fmt.Errorf("creating files for in-memory items is not implemented")
	}

	out, err := os.Create(b.filename + name)
	if err != nil {
		return "", nil, err
	}

	return out.Name(), out, nil
}

// FilenameFromDigest implements common.BuildContext.
func (b *BuildContext) FilenameFromDigest(digest *filesystem.FileDigest) (string, error) {
	return digest.Hash, nil
}

// FileFromDigest implements common.BuildContext.
func (b *BuildContext) FileFromDigest(digest *filesystem.FileDigest) (filesystem.File, error) {
	if digest.Hash != "" {
		return filesystem.NewLocalFile(digest.Hash, nil), nil
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

// Database implements common.BuildContext.
func (b *BuildContext) Database() common.PackageDatabase {
	return b.database
}

func (b *BuildContext) ChildContext(source common.BuildSource, status *common.BuildStatus, filename string) common.BuildContext {
	ctx := &BuildContext{
		parent:   b,
		filename: filename,
		output:   nil,
		status:   status,
		source:   source,
		database: b.database,
		inMemory: b.inMemory,
	}

	b.children = append(b.children, ctx)

	return ctx
}

func (b *BuildContext) CreateOutput() (io.WriteCloser, error) {
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

	return b.database.Build(b, def, common.BuildOptions{})
}

func (b *BuildContext) NeedsBuild(def common.BuildDefinition) (bool, error) {
	if b.inMemory {
		return true, nil
	}

	hash, err := b.database.HashDefinition(def)
	if err != nil {
		return true, err
	}

	filename, err := b.database.FilenameFromHash(hash, ".bin")
	if err != nil {
		return true, err
	}

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
			f, err := b.CreateOutput()
			if err != nil {
				return nil, err
			}

			return record.NewWriter2(f), nil
		}), nil
	} else if name == "archive" {
		return starlark.NewBuiltin("BuildContext.archive", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				dir  *filesystem.StarDirectory
				kind string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"dir", &dir,
				"kind?", &kind,
			); err != nil {
				return starlark.None, err
			}

			if kind == "" {
				return &directoryToArchiveBuildResult{dir: dir}, nil
			} else {
				return starlark.None, fmt.Errorf("BuildContext.archive kind not implemented: %s", kind)
			}
		}), nil
	} else if name == "build" {
		return starlark.NewBuiltin("BuildContext.build", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				val starlark.Value
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"def", &val,
			); err != nil {
				return starlark.None, err
			}

			var buildDef common.BuildDefinition

			if def, ok := val.(common.BuildDefinition); ok {
				buildDef = def
			} else {
				return starlark.None, fmt.Errorf("could not convert %s to BuildDefinition", val.Type())
			}

			result, err := b.BuildChild(buildDef)
			if err != nil {
				return starlark.None, err
			}

			return buildDef.ToStarlark(b, result)
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (b *BuildContext) AttrNames() []string {
	return []string{"recordwriter", "add_package", "build"}
}

func (ctx *BuildContext) Call(filename string, builder string, args ...starlark.Value) (starlark.Value, error) {
	target, err := ctx.database.GetBuilder(filename, builder)
	if err != nil {
		return starlark.None, fmt.Errorf("failed to GetBuilder in BuildContext.Call: %s", err)
	}

	result, err := starlark.Call(ctx.database.NewThread(filename), target, append(starlark.Tuple{ctx}, args...), []starlark.Tuple{})
	if err != nil {
		if sErr, ok := err.(*starlark.EvalError); ok {
			slog.Error("got starlark error", "error", sErr, "backtrace", sErr.Backtrace())
		}
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
	return &BuildContext{source: source, database: db}
}
