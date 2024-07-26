package builder

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"go.starlark.net/starlark"
)

type StarBuildDefinition struct {
	params      StarParameters
	Name        []string
	Builder     starlark.Callable
	BuilderArgs starlark.Tuple
}

// implements common.BuildDefinition.
func (def *StarBuildDefinition) Params() common.SerializableValue { return def.params }
func (def *StarBuildDefinition) SerializableType() string         { return "StarBuildDefinition" }
func (def *StarBuildDefinition) Create(params common.SerializableValue) common.Definition {
	return &StarBuildDefinition{params: *params.(*StarParameters)}
}

// AsFragments implements common.Directive.
func (def *StarBuildDefinition) AsFragments(ctx common.BuildContext) ([]config.Fragment, error) {
	res, err := ctx.BuildChild(def)
	if err != nil {
		return nil, err
	}

	digest := res.Digest()

	filename, err := ctx.FilenameFromDigest(digest)
	if err != nil {
		return nil, err
	}

	return []config.Fragment{
		{Archive: &config.ArchiveFragment{HostFilename: filename}},
	}, nil
}

// ToStarlark implements common.BuildDefinition.
func (def *StarBuildDefinition) ToStarlark(ctx common.BuildContext, result filesystem.File) (starlark.Value, error) {
	return filesystem.NewStarFile(result, def.Tag()), nil
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
				slog.Info("forcing rebuild", "def", argDef)
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

			val, err := argDef.ToStarlark(ctx, res)
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
	} else if f, ok := res.(filesystem.File); ok {
		fh, err := f.Open()
		if err != nil {
			return nil, err
		}

		return &copyFileResult{fh: fh}, nil
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
	_ common.Directive       = &StarBuildDefinition{}
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
