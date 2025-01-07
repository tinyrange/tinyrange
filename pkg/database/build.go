package database

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/record"
	"go.starlark.net/starlark"
)

func runVMM(exe string, buildDir string, configFilename string) (*exec.Cmd, error) {
	persistPath := filepath.Join(buildDir, "persist")

	if err := common.Ensure(persistPath, os.ModePerm); err != nil {
		return nil, err
	}

	cmd := exec.Command(exe, "-build-dir", buildDir, "-persist-path", persistPath, configFilename)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	slog.Debug("executing VMM", "args", cmd.Args)

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd, nil
}

type buildContext struct {
	source   common.BuildSource
	database *packageDatabase
	parent   *buildContext
	status   *common.BuildStatus
	children []*buildContext

	filename  string
	output    io.WriteCloser
	inMemory  bool
	hasCached bool
}

// ShouldRebuildUserDefinitions implements common.BuildContext.
func (b *buildContext) ShouldRebuildUserDefinitions() bool {
	return b.database.rebuildUserDefinitions
}

// BuildDir implements common.BuildContext.
func (b *buildContext) BuildDir() string {
	return b.database.buildDir
}

func (b *buildContext) RunVMM(name string, vmCfg config.TinyRangeConfig) (*exec.Cmd, error) {
	configFilename, out, err := b.CreateFile(".json")
	if err != nil {
		return nil, err
	}

	enc := json.NewEncoder(out)

	if err := enc.Encode(&vmCfg); err != nil {
		out.Close()
		return nil, err
	}

	if err := out.Close(); err != nil {
		return nil, err
	}

	var exe string

	if name == "qemu" {
		exe, err = common.GetAdjacentExecutable("tinyrange_qemu", "tinyqemu/tinyrange_qemu")
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("unknown VMM: %s", name)
	}

	return runVMM(exe, b.BuildDir(), configFilename)
}

// SetHasCached implements common.BuildContext.
func (b *buildContext) SetHasCached() {
	b.hasCached = true
}

// HasCached implements common.BuildContext.
func (b *buildContext) HasCached() bool {
	return b.hasCached
}

// CreateFile implements common.BuildContext.
func (b *buildContext) CreateFile(name string) (string, io.WriteCloser, error) {
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
func (b *buildContext) FilenameFromDigest(digest *filesystem.FileDigest) (string, error) {
	return digest.Hash, nil
}

// FileFromDigest implements common.BuildContext.
func (b *buildContext) FileFromDigest(digest *filesystem.FileDigest) (filesystem.File, error) {
	if digest.Hash != "" {
		return filesystem.NewLocalFile(digest.Hash, nil), nil
	}

	return nil, fmt.Errorf("could not convert digest to hash")
}

// IsInMemory implements common.BuildContext.
func (b *buildContext) IsInMemory() bool {
	return b.inMemory
}

// SetInMemory implements common.BuildContext.
func (b *buildContext) SetInMemory() {
	b.inMemory = true
}

// Database implements common.BuildContext.
func (b *buildContext) Database() common.PackageDatabase {
	return b.database
}

func (b *buildContext) childContext(source common.BuildSource, status *common.BuildStatus, filename string) *buildContext {
	ctx := &buildContext{
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

func (b *buildContext) CreateOutput() (io.WriteCloser, error) {
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

func (b *buildContext) HasCreatedOutput() bool {
	return b.output != nil
}

func (b *buildContext) BuildChild(def common.BuildDefinition) (filesystem.File, error) {
	if b.status != nil {
		b.status.Children = append(b.status.Children, def)
	}

	return b.database.build(b, def, common.BuildOptions{})
}

func (b *buildContext) NeedsBuild(def common.BuildDefinition) (bool, error) {
	if b.inMemory {
		return true, nil
	}

	hash, err := b.database.HashDefinition(def)
	if err != nil {
		return true, err
	}

	filename, err := b.database.filenameFromHash(hash, ".bin")
	if err != nil {
		return true, err
	}

	// Check if the file already exists. If it does then return it.
	if info, err := os.Stat(filename); err == nil {
		// Get a child context for the build.
		child := b.childContext(def, b.status, filename+".tmp")

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
func (b *buildContext) Attr(name string) (starlark.Value, error) {
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
				return builder.NewDirectoryToArchiveBuildResult(dir), nil
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
func (b *buildContext) AttrNames() []string {
	return []string{"recordwriter", "add_package", "build"}
}

func (ctx *buildContext) Call(filename string, builder string, args ...starlark.Value) (starlark.Value, error) {
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

func (*buildContext) String() string        { return "BuildContext" }
func (*buildContext) Type() string          { return "BuildContext" }
func (*buildContext) Hash() (uint32, error) { return 0, fmt.Errorf("BuildContext is not hashable") }
func (*buildContext) Truth() starlark.Bool  { return starlark.True }
func (*buildContext) Freeze()               {}

var (
	_ starlark.Value      = &buildContext{}
	_ starlark.HasAttrs   = &buildContext{}
	_ common.BuildContext = &buildContext{}
)
