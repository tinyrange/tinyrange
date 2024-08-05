package builder

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/emulator"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

type BuildEmulatorDefinition struct {
	params BuildEmulatorParameters

	frags []config.Fragment
}

// implements common.BuildDefinition.
func (def *BuildEmulatorDefinition) Params() hash.SerializableValue { return def.params }
func (def *BuildEmulatorDefinition) SerializableType() string       { return "BuildVmDefinition" }
func (def *BuildEmulatorDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &BuildVmDefinition{params: params.(BuildVmParameters)}
}

// ToStarlark implements common.BuildDefinition.
func (def *BuildEmulatorDefinition) ToStarlark(ctx common.BuildContext, result filesystem.File) (starlark.Value, error) {
	return filesystem.NewStarFile(result, def.Tag()), nil
}

// Build implements common.BuildDefinition.
func (def *BuildEmulatorDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	var commands []string

	// Get the creation callback.
	createFunc, err := ctx.Database().GetBuilder(def.params.ScriptFilename, def.params.CreateName)
	if err != nil {
		return nil, err
	}

	// Launch child builds for each directive.
	for _, directive := range def.params.Directives {
		frags, err := directive.AsFragments(ctx)
		if err != nil {
			return nil, err
		}

		for _, frag := range frags {
			if frag.RunCommand != nil {
				commands = append(commands, frag.RunCommand.Command)
			} else {
				def.frags = append(def.frags, frag)
			}
		}
	}

	// Create the filesystem from the fragment list.
	dir := filesystem.NewMemoryDirectory()

	for _, frag := range def.frags {
		if frag.Archive != nil {
			ark, err := filesystem.ReadArchiveFromFile(
				filesystem.NewLocalFile(frag.Archive.HostFilename, nil),
			)
			if err != nil {
				return nil, err
			}

			if err := filesystem.ExtractArchive(ark, dir); err != nil {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("unimplemented fragment type: %+v", frag)
		}
	}

	// Create the emulator from the filesystem.
	emu := emulator.New(def.params.ScriptFilename, dir)

	if err := emu.AddBuiltinPrograms(); err != nil {
		return nil, err
	}

	// Call the creation callback.
	_, err = starlark.Call(
		ctx.Database().NewThread(def.params.ScriptFilename),
		createFunc, starlark.Tuple{emu}, []starlark.Tuple{},
	)
	if err != nil {
		return nil, err
	}

	// Run each command in the emulator.
	for _, command := range commands {
		slog.Debug("emulator", "run", command)
		if err := emu.RunShell(command); err != nil {
			return nil, err
		}
	}

	// Open the output file.
	ent, err := filesystem.OpenPath(emu.Root(), def.params.OutputFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open output %s: %s", def.params.OutputFile, err)
	}

	fh, err := ent.Open()
	if err != nil {
		return nil, err
	}

	return &copyFileResult{fh: fh}, nil
}

// NeedsBuild implements common.BuildDefinition.
func (def *BuildEmulatorDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	if ctx.Database().ShouldRebuildUserDefinitions() {
		return true, nil
	}

	// TODO(joshua): Check if any of the child directives need to be built.
	return false, nil
}

// Tag implements common.BuildDefinition.
func (def *BuildEmulatorDefinition) Tag() string {
	out := []string{"BuildEmulator"}

	for _, dir := range def.params.Directives {
		out = append(out, dir.Tag())
	}

	out = append(out, def.params.OutputFile)
	out = append(out, def.params.ScriptFilename)
	out = append(out, def.params.CreateName)

	return strings.Join(out, "_")
}

func (def *BuildEmulatorDefinition) String() string { return def.Tag() }
func (*BuildEmulatorDefinition) Type() string       { return "BuildVmDefinition" }
func (*BuildEmulatorDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("BuildVmDefinition is not hashable")
}
func (*BuildEmulatorDefinition) Truth() starlark.Bool { return starlark.True }
func (*BuildEmulatorDefinition) Freeze()              {}

var (
	_ starlark.Value         = &BuildEmulatorDefinition{}
	_ common.BuildDefinition = &BuildEmulatorDefinition{}
)

func NewBuildEmulatorDefinition(
	dir []common.Directive,
	output string,
	scriptFilename string,
	createCallbackName string,
) *BuildEmulatorDefinition {
	return &BuildEmulatorDefinition{
		params: BuildEmulatorParameters{
			Directives:     dir,
			OutputFile:     output,
			ScriptFilename: scriptFilename,
			CreateName:     createCallbackName,
		},
	}
}
