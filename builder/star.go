package builder

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/tinyrange/pkg2/v2/common"
	"github.com/tinyrange/pkg2/v2/filesystem"
	"go.starlark.net/starlark"
)

type StarBuildDefinition struct {
	Name        []string
	Builder     starlark.Callable
	BuilderArgs starlark.Tuple
}

func BuildResultToStarlark(ctx common.BuildContext, argDef common.BuildDefinition, result filesystem.File) (starlark.Value, error) {
	switch arg := argDef.(type) {
	case *ReadArchiveBuildDefinition:
		ark, err := filesystem.ReadArchiveFromFile(result)
		if err != nil {
			return starlark.None, err
		}

		return filesystem.NewStarArchive(ark, argDef.Tag()), nil
	case *FetchHttpBuildDefinition:
		return filesystem.NewStarFile(result, argDef.Tag()), nil
	case *StarBuildDefinition:
		return filesystem.NewStarFile(result, argDef.Tag()), nil
	case *DecompressFileBuildDefinition:
		return filesystem.NewStarFile(result, argDef.Tag()), nil
	case *BuildVmDefinition:
		return filesystem.NewStarFile(result, argDef.Tag()), nil
	case *FetchOciImageDefinition:
		if err := parseJsonFromFile(result, &arg); err != nil {
			return nil, err
		}

		fs := filesystem.NewMemoryDirectory()

		for _, layer := range arg.LayerArchives {
			layerFile, err := ctx.FileFromDigest(layer)
			if err != nil {
				return nil, err
			}

			ark, err := filesystem.ReadArchiveFromFile(layerFile)
			if err != nil {
				return starlark.None, err
			}

			if err := filesystem.ExtractArchive(ark, fs); err != nil {
				return starlark.None, err
			}
		}

		return filesystem.NewStarDirectory(fs, ""), nil
	default:
		return starlark.None, fmt.Errorf("BuildResultToStarlark not implemented for: %T %+v", arg, arg)
	}
}

// NeedsBuild implements BuildDefinition.
func (def *StarBuildDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	if ctx.Database().ShouldRebuildUserDefinitions() {
		return true, nil
	}

	for _, arg := range def.BuilderArgs {
		if argDef, ok := arg.(common.BuildDefinition); ok {
			needsBuild, err := ctx.NeedsBuild(argDef)
			if err != nil {
				return true, err
			}

			if needsBuild {
				return true, nil
			}
		}
	}

	return false, nil
}

// Tag implements BuildSource.
func (def *StarBuildDefinition) Tag() string {
	var parts []string

	parts = append(parts, def.Name...)

	for _, arg := range def.BuilderArgs {
		if src, ok := arg.(common.BuildSource); ok {
			parts = append(parts, src.Tag())
		} else if lst, ok := arg.(*starlark.List); ok {
			parts = append(parts, fmt.Sprintf("%+v", lst))
		} else if str, ok := starlark.AsString(arg); ok {
			parts = append(parts, str)
		} else {
			slog.Warn("could not convert to string", "type", arg.Type())
			continue
		}
	}

	return strings.Join(parts, "_")
}

func (def *StarBuildDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	var args starlark.Tuple
	for _, arg := range def.BuilderArgs {
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

	res, err := ctx.Call(def.Builder, args...)
	if err != nil {
		return nil, err
	}

	if result, ok := res.(common.BuildResult); ok {
		return result, nil
	} else {
		return nil, fmt.Errorf("could not convert %s to BuildResult", res.Type())
	}
}

func (def *StarBuildDefinition) String() string { return def.Tag() }
func (*StarBuildDefinition) Type() string       { return "BuildDefinition" }
func (*StarBuildDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("BuildDefinition is not hashable")
}
func (*StarBuildDefinition) Truth() starlark.Bool { return starlark.True }
func (*StarBuildDefinition) Freeze()              {}

var (
	_ starlark.Value         = &StarBuildDefinition{}
	_ common.BuildSource     = &StarBuildDefinition{}
	_ common.BuildDefinition = &StarBuildDefinition{}
)

func NewStarBuildDefinition(name string, builder starlark.Value, args starlark.Tuple) (*StarBuildDefinition, error) {
	f, ok := builder.(starlark.Callable)
	if !ok {
		return nil, fmt.Errorf("builder %s is not callable", builder.Type())
	}

	return &StarBuildDefinition{
		Name:        []string{name, f.Name()},
		Builder:     f,
		BuilderArgs: args,
	}, nil
}
