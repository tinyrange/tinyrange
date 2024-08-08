package builder

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&StarBuildDefinition{})
}

func SerializableValueToStarlark(ctx common.BuildContext, val hash.SerializableValue) (starlark.Value, error) {
	switch val := val.(type) {
	case common.BuildDefinition:
		result, err := ctx.BuildChild(val)
		if err != nil {
			return starlark.None, err
		}

		return val.ToStarlark(ctx, result)
	case hash.SerializableString:
		return starlark.String(val), nil
	case hash.SerializableList:
		var ret []starlark.Value

		for _, child := range val {
			item, err := SerializableValueToStarlark(ctx, child)
			if err != nil {
				return starlark.None, err
			}

			ret = append(ret, item)
		}

		return starlark.NewList(ret), nil
	default:
		return starlark.None, fmt.Errorf("SerializableValueToStarlark not implemented: %T %+v", val, val)
	}
}

func StarlarkValueToSerializable(val starlark.Value) (hash.SerializableValue, error) {
	switch val := val.(type) {
	case common.BuildDefinition:
		return val, nil
	case starlark.String:
		return hash.SerializableString(val), nil
	case *starlark.List:
		var ret hash.SerializableList

		for i := 0; i < val.Len(); i++ {
			child, err := StarlarkValueToSerializable(val.Index(i))
			if err != nil {
				return nil, err
			}

			ret = append(ret, child)
		}

		return ret, nil
	default:
		return nil, fmt.Errorf("StarlarkValueToSerializable not implemented: %T %+v", val, val)
	}
}

type StarBuildDefinition struct {
	params StarParameters
}

// Dependencies implements common.BuildDefinition.
func (def *StarBuildDefinition) Dependencies(ctx common.BuildContext) ([]common.DependencyNode, error) {
	var ret []common.DependencyNode

	for _, arg := range def.params.Arguments {
		if argDef, ok := arg.(common.BuildDefinition); ok {
			ret = append(ret, argDef)
		}
	}

	return ret, nil
}

// implements common.BuildDefinition.
func (def *StarBuildDefinition) Params() hash.SerializableValue { return def.params }
func (def *StarBuildDefinition) SerializableType() string       { return "StarBuildDefinition" }
func (def *StarBuildDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &StarBuildDefinition{params: params.(StarParameters)}
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

	for _, arg := range def.params.Arguments {
		if argDef, ok := arg.(common.BuildDefinition); ok {
			needsBuild, err := ctx.NeedsBuild(argDef)
			if err != nil {
				return true, err
			}

			if needsBuild {
				slog.Debug("forcing rebuild", "def", argDef)
				return true, nil
			}
		}
	}

	return false, nil
}

// Tag implements BuildSource.
func (def *StarBuildDefinition) Tag() string {
	var parts []string

	parts = append(parts, def.params.ScriptFilename, def.params.BuilderName)

	for _, arg := range def.params.Arguments {
		parts = append(parts, fmt.Sprintf("%+v", arg))
	}

	return strings.Join(parts, "_")
}

func (def *StarBuildDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	var args starlark.Tuple
	for _, arg := range def.params.Arguments {
		val, err := SerializableValueToStarlark(ctx, arg)
		if err != nil {
			return nil, err
		}

		args = append(args, val)
	}

	res, err := ctx.Call(def.params.ScriptFilename, def.params.BuilderName, args...)
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

func NewStarBuildDefinition(filename string, builder string, args []hash.SerializableValue) (*StarBuildDefinition, error) {
	if filename == "" || builder == "" {
		return nil, fmt.Errorf("no filename or builder passed to NewStarBuildDefinition")
	}

	return &StarBuildDefinition{
		params: StarParameters{
			ScriptFilename: filename,
			BuilderName:    builder,
			Arguments:      args,
		},
	}, nil
}
