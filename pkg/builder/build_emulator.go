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

func init() {
	hash.RegisterType(&buildEmulatorDefinition{})
}

type buildEmulatorDefinition struct {
	params BuildEmulatorParameters

	frags []config.Fragment
}

// implements common.BuildDefinition.
func (def *buildEmulatorDefinition) Params() hash.SerializableValue { return def.params }
func (def *buildEmulatorDefinition) SerializableType() string       { return "BuildEmulatorDefinition" }
func (def *buildEmulatorDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &buildVmDefinition{params: params.(BuildVmParameters)}
}

// ToStarlark implements common.BuildDefinition.
func (def *buildEmulatorDefinition) ToStarlark(ctx common.BuildContext1, result filesystem.File) (starlark.Value, error) {
	return filesystem.NewStarFile(result, def.Tag()), nil
}

// Build implements common.BuildDefinition.
func (def *buildEmulatorDefinition) Build(ctx common.BuildContext1) (common.BuildResult, error) {
	var commands []string

	// Launch child builds for each directive.
	for _, directive := range def.params.Directives {
		frags, err := directive.AsFragments(ctx, common.SpecialDirectiveHandlers{})
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
	_, err := ctx.Call(def.params.ScriptFilename, def.params.CreateName, emu)
	if err != nil {
		return nil, fmt.Errorf("failed to call emulator creation callback: %s", err)
	}

	// Run each command in the emulator.
	for _, command := range commands {
		slog.Debug("emulator", "run", command)
		if err := emu.RunShell(command); err != nil {
			return nil, fmt.Errorf("failed to run command in emulator [%+v]: %s", command, err)
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
func (def *buildEmulatorDefinition) NeedsBuild(ctx common.BuildContext1, cacheTime time.Time) (bool, error) {
	if ctx.ShouldRebuildUserDefinitions() {
		return true, nil
	}

	// TODO(joshua): Check if any of the child directives need to be built.
	return false, nil
}

// Tag implements common.BuildDefinition.
func (def *buildEmulatorDefinition) Tag() string {
	out := []string{"BuildEmulator"}

	for _, dir := range def.params.Directives {
		out = append(out, dir.Tag())
	}

	out = append(out, def.params.OutputFile)
	out = append(out, def.params.ScriptFilename)
	out = append(out, def.params.CreateName)

	return strings.Join(out, "_")
}

func (def *buildEmulatorDefinition) String() string { return def.Tag() }
func (*buildEmulatorDefinition) Type() string       { return "BuildEmulatorDefinition" }
func (*buildEmulatorDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("BuildEmulatorDefinition is not hashable")
}
func (*buildEmulatorDefinition) Truth() starlark.Bool { return starlark.True }
func (*buildEmulatorDefinition) Freeze()              {}

var (
	_ starlark.Value          = &buildEmulatorDefinition{}
	_ common.BuildDefinition1 = &buildEmulatorDefinition{}
)

func newBuildEmulatorDefinition(
	dir []common.Directive,
	output string,
	scriptFilename string,
	createCallbackName string,
) common.StarBuildDefinition1 {
	return &buildEmulatorDefinition{
		params: BuildEmulatorParameters{
			Directives:     dir,
			OutputFile:     output,
			ScriptFilename: scriptFilename,
			CreateName:     createCallbackName,
		},
	}
}
