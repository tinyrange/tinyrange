package database

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/record"
	"go.starlark.net/starlark"
)

type ErrTemplateBuilt string

// Error implements error.
func (e ErrTemplateBuilt) Error() string { return "template built" }

var (
	_ error = ErrTemplateBuilt("")
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
	def      common.BuildDefinition
	hash     hash.Hash
	builder  *builder1
	parent   *buildContext
	status   *buildStatus
	children []*buildContext

	filename  string
	output    io.WriteCloser
	lastBuild time.Time
}

// CreateFile implements common.BuildContext.
func (b *buildContext) CreateFile(name string) (io.WriteCloser, error) {
	return nil, fmt.Errorf("not implemented")
}

// Describe implements common.BuildContext.
func (b *buildContext) Describe(format string, args ...interface{}) {
	slog.Info("describe", "hash", b.hash, "message", fmt.Sprintf(format, args...))
}

// Logf implements common.BuildContext.
func (b *buildContext) Logf(format string, args ...interface{}) {
	slog.Info("log", "hash", b.hash, "message", fmt.Sprintf(format, args...))
}

// WriteDefault implements common.BuildContext.
func (b *buildContext) WriteDefault(result common.BuildResult) error {
	defFile, err := b.CreateDefault()
	if err != nil {
		return fmt.Errorf("could not create default file for %T: %w", b.def, err)
	}

	return result.WriteResult(defFile)
}

// DefinitionHash implements common.BuildContext.
func (b *buildContext) DefinitionHash() hash.Hash {
	return b.hash
}

// ShouldRebuildUserDefinitions implements common.BuildContext.
func (b *buildContext) ShouldRebuildUserDefinitions() bool {
	return b.builder.rebuildUserDefinitions
}

// BuildDir implements common.BuildContext.
func (b *buildContext) BuildDir() string {
	return b.builder.buildDir
}

func (b *buildContext) RunVMM(name string, vmCfg config.TinyRangeConfig) (*exec.Cmd, error) {
	configFilename, out, err := b.createFile(".json")
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

	if name == "" {
		return nil, ErrTemplateBuilt(configFilename)
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

// LsatBuild implements common.BuildContext.
func (b *buildContext) LastBuild() time.Time {
	return b.lastBuild
}

// createFile implements common.BuildContext.
func (b *buildContext) createFile(name string) (string, io.WriteCloser, error) {
	out, err := os.Create(b.filename + name)
	if err != nil {
		return "", nil, err
	}

	return out.Name(), out, nil
}

// DigestFromFile implements common.BuildContext.
func (b *buildContext) DigestFromFile(file filesystem.File) (*filesystem.FileDigest, error) {
	filename, err := filesystem.GetHostFilename(file)
	if err != nil {
		return nil, err
	}

	return &filesystem.FileDigest{Hash: filename}, nil
}

// HostFilenameFromFile implements common.BuildContext.
func (b *buildContext) HostFilenameFromFile(file filesystem.File) (string, error) {
	return filesystem.GetHostFilename(file)
}

// FileFromDigest implements common.BuildContext.
func (b *buildContext) FileFromDigest(digest *filesystem.FileDigest) (filesystem.File, error) {
	if digest.Hash != "" {
		return filesystem.NewLocalFile(digest.Hash, nil), nil
	}

	return nil, fmt.Errorf("could not convert digest to hash")
}

// Database implements common.BuildContext.
func (b *buildContext) Database() common.PackageDatabase {
	return b.builder.database
}

func (b *buildContext) childContext(def common.BuildDefinition, status *buildStatus, filename string) *buildContext {
	ctx := &buildContext{
		parent:   b,
		filename: filename,
		output:   nil,
		status:   status,
		def:      def,
		builder:  b.builder,
	}

	b.children = append(b.children, ctx)

	return ctx
}

func (b *buildContext) CreateDefault() (io.WriteCloser, error) {
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

func (b *buildContext) BuildChild(def common.BuildDefinition) (common.BuildArtifact, error) {
	if b.status != nil {
		b.status.Children = append(b.status.Children, def)
	}

	return b.builder.build(b, def, common.BuildOptions{})
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
			f, err := b.CreateDefault()
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
