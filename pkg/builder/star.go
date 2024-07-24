package builder

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"go.starlark.net/starlark"
)

type StarBuildDefinition struct {
	Name      []string
	builder   starlark.Callable
	Arguments starlark.Tuple
}

// Create implements common.BuildDefinition.
func (def *StarBuildDefinition) Create(params common.BuildDefinitionParameters) common.BuildDefinition {
	panic("unimplemented")
}

// Params implements common.BuildDefinition.
func (def *StarBuildDefinition) Params() common.BuildDefinitionParameters {
	panic("unimplemented")
}

func BuildResultToStarlark(ctx common.BuildContext, argDef common.BuildDefinition, result common.File) (starlark.Value, error) {
	switch arg := argDef.(type) {
	case *ReadArchiveBuildDefinition:
		ark, err := common.ReadArchiveFromFile(result)
		if err != nil {
			return starlark.None, err
		}

		return common.NewStarArchive(ark, argDef.Type()), nil
	case *CreateArchiveDefinition:
		ark, err := common.ReadArchiveFromFile(result)
		if err != nil {
			return starlark.None, err
		}

		return common.NewStarArchive(ark, argDef.Type()), nil
	case *PlanDefinition:
		var plan *PlanDefinition

		if err := ParseJsonFromFile(result, &plan); err != nil {
			return nil, err
		}

		return plan, nil
	case *FetchHttpBuildDefinition:
		return common.NewStarFile(result, argDef.Type()), nil
	case *StarBuildDefinition:
		return common.NewStarFile(result, argDef.Type()), nil
	case *DecompressFileDefinition:
		return common.NewStarFile(result, argDef.Type()), nil
	case *BuildVmDefinition:
		return common.NewStarFile(result, argDef.Type()), nil
	case *BuildFsDefinition:
		return common.NewStarFile(result, argDef.Type()), nil
	case *FetchOciImageDefinition:
		if err := ParseJsonFromFile(result, &arg); err != nil {
			return nil, err
		}

		fs := common.NewMemoryDirectory()

		for _, layer := range arg.LayerArchives {
			layerFile, err := ctx.FileFromDigest(layer)
			if err != nil {
				return nil, err
			}

			ark, err := common.ReadArchiveFromFile(layerFile)
			if err != nil {
				return starlark.None, err
			}

			if err := common.ExtractArchive(ark, fs); err != nil {
				return starlark.None, err
			}
		}

		return common.NewStarDirectory(fs, ""), nil
	default:
		return starlark.None, fmt.Errorf("BuildResultToStarlark not implemented for: %T %+v", arg, arg)
	}
}

// NeedsBuild implements BuildDefinition.
func (def *StarBuildDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	if ctx.Database().ShouldRebuildUserDefinitions() {
		return true, nil
	}

	for _, arg := range def.Arguments {
		if argDef, ok := arg.(common.BuildDefinition); ok {
			needsBuild, err := ctx.NeedsBuild(argDef)
			if err != nil {
				return true, err
			}

			if needsBuild {
				slog.Info("forcing rebuild", "def", argDef)
				return true, nil
			}
		}
	}

	return false, nil
}

func (def *StarBuildDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	var args starlark.Tuple
	for _, arg := range def.Arguments {
		if argDef, ok := arg.(common.BuildDefinition); ok {
			res, err := ctx.BuildChild(argDef)
			if err != nil {
				return nil, err
			}

			val, err := BuildResultToStarlark(ctx, argDef, res)
			if err != nil {
				return nil, err
			}

			args = append(args, val)
		} else {
			args = append(args, arg)
		}
	}

	res, err := ctx.Call(def.builder, args...)
	if err != nil {
		return nil, err
	}

	if result, ok := res.(common.BuildResult); ok {
		return result, nil
	} else if f, ok := res.(common.File); ok {
		fh, err := f.Open()
		if err != nil {
			return nil, err
		}

		// Just using the build result part of it.
		return &copyFileBuildResult{fh: fh}, nil
	} else {
		return nil, fmt.Errorf("could not convert %s to BuildResult", res.Type())
	}
}

func (def *StarBuildDefinition) String() string { return "StarBuildDefinition" }
func (*StarBuildDefinition) Type() string       { return "StarBuildDefinition" }
func (*StarBuildDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("StarBuildDefinition is not hashable")
}
func (*StarBuildDefinition) Truth() starlark.Bool { return starlark.True }
func (*StarBuildDefinition) Freeze()              {}

var (
	_ starlark.Value         = &StarBuildDefinition{}
	_ common.BuildDefinition = &StarBuildDefinition{}
)

func NewStarBuildDefinition(name string, builder starlark.Value, args starlark.Tuple) (*StarBuildDefinition, error) {
	f, ok := builder.(starlark.Callable)
	if !ok {
		return nil, fmt.Errorf("builder %s is not callable", builder.Type())
	}

	return &StarBuildDefinition{
		Name:      []string{name, f.Name()},
		builder:   f,
		Arguments: args,
	}, nil
}
