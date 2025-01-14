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
	hash.RegisterType(&starBuildDefinition{})
}

func SerializableValueToStarlark(ctx common.BuildContext1, val hash.SerializableValue) (starlark.Value, error) {
	switch val := val.(type) {
	case common.BuildDefinition1:
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
	case hash.SerializableBool:
		if val {
			return starlark.True, nil
		} else {
			return starlark.False, nil
		}
	case filesystem.ChildSource:
		if def, ok := val.Source.(common.BuildDefinition1); ok {
			result, err := ctx.BuildChild(def)
			if err != nil {
				return starlark.None, err
			}

			starVal, err := def.ToStarlark(ctx, result)
			if err != nil {
				return starlark.None, err
			}

			if ark, ok := starVal.(*filesystem.StarArchive); ok {
				ents, err := ark.Entries()
				if err != nil {
					return starlark.None, err
				}

				for _, ent := range ents {
					if ent.Name() == val.Name {
						return filesystem.NewStarFile(ent, ent.Name()), nil
					}
				}

				return nil, fmt.Errorf("file %s not found in %s", val.Name, ark)
			} else {
				return starlark.None, fmt.Errorf("SerializableValueToStarlark not implemented: %T %+v", starVal, starVal)
			}
		} else {
			return starlark.None, fmt.Errorf("SerializableValueToStarlark not implemented: %T %+v", val, val)
		}
	default:
		return starlark.None, fmt.Errorf("SerializableValueToStarlark not implemented: %T %+v", val, val)
	}
}

func StarlarkValueToSerializable(val starlark.Value) (hash.SerializableValue, error) {
	switch val := val.(type) {
	case common.BuildDefinition1:
		return val, nil
	case *filesystem.StarFile:
		return filesystem.SourceFromFile(val.File)
	case starlark.String:
		return hash.SerializableString(val), nil
	case starlark.Bool:
		return hash.SerializableBool(val), nil
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

type starBuildDefinition struct {
	params          StarParameters
	redistributable bool
}

// Redistributable implements common.RedistributableDefinition.
func (def *starBuildDefinition) Redistributable() bool {
	return def.redistributable
}

// Attr implements starlark.HasAttrs.
func (def *starBuildDefinition) Attr(name string) (starlark.Value, error) {
	if name == "set_redistributable" {
		return starlark.NewBuiltin("BuildDefinition.set_redistributable", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				value bool
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"value", &value,
			); err != nil {
				return starlark.None, err
			}

			def.redistributable = value

			return def, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (def *starBuildDefinition) AttrNames() []string {
	return []string{"set_redistributable"}
}

// Dependencies implements common.BuildDefinition.
func (def *starBuildDefinition) Dependencies(ctx common.BuildContext1) ([]common.DependencyNode, error) {
	var ret []common.DependencyNode

	for _, arg := range def.params.Arguments {
		if argDef, ok := arg.(common.BuildDefinition1); ok {
			ret = append(ret, argDef)
		}
	}

	return ret, nil
}

// implements common.BuildDefinition.
func (def *starBuildDefinition) Params() hash.SerializableValue { return def.params }
func (def *starBuildDefinition) SerializableType() string       { return "StarBuildDefinition" }
func (def *starBuildDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &starBuildDefinition{params: params.(StarParameters)}
}

// AsFragments implements common.Directive.
func (def *starBuildDefinition) AsFragments(ctx common.BuildContext1, special common.SpecialDirectiveHandlers) ([]config.Fragment, error) {
	res, err := ctx.BuildChild(def)
	if err != nil {
		return nil, err
	}

	digest, err := ctx.DigestFromFile(res)
	if err != nil {
		return nil, err
	}

	filename, err := ctx.FilenameFromDigest(digest)
	if err != nil {
		return nil, err
	}

	return []config.Fragment{
		{Archive: &config.ArchiveFragment{HostFilename: filename}},
	}, nil
}

// ToStarlark implements common.BuildDefinition.
func (def *starBuildDefinition) ToStarlark(ctx common.BuildContext1, result filesystem.File) (starlark.Value, error) {
	return filesystem.NewStarFile(result, def.Tag()), nil
}

// NeedsBuild implements BuildDefinition.
func (def *starBuildDefinition) NeedsBuild(ctx common.BuildContext1, cacheTime time.Time) (bool, error) {
	if ctx.ShouldRebuildUserDefinitions() {
		return true, nil
	}

	for _, arg := range def.params.Arguments {
		if argDef, ok := arg.(common.BuildDefinition1); ok {
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
func (def *starBuildDefinition) Tag() string {
	var parts []string

	parts = append(parts, def.params.ScriptFilename, def.params.BuilderName)

	for _, arg := range def.params.Arguments {
		parts = append(parts, fmt.Sprintf("%+v", arg))
	}

	return strings.Join(parts, "_")
}

func (def *starBuildDefinition) Build(ctx common.BuildContext1) (common.BuildResult, error) {
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

	if result, ok := res.(common.BuildDefinition1); ok {
		child, err := ctx.BuildChild(result)
		if err != nil {
			return nil, err
		}

		fh, err := child.Open()
		if err != nil {
			return nil, err
		}

		return &copyFileResult{fh: fh}, nil
	} else if result, ok := res.(common.BuildResult); ok {
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

func (def *starBuildDefinition) String() string { return def.Tag() }
func (*starBuildDefinition) Type() string       { return "BuildDefinition" }
func (*starBuildDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("BuildDefinition is not hashable")
}
func (*starBuildDefinition) Truth() starlark.Bool { return starlark.True }
func (*starBuildDefinition) Freeze()              {}

var (
	_ starlark.Value                   = &starBuildDefinition{}
	_ starlark.HasAttrs                = &starBuildDefinition{}
	_ common.BuildDefinition1          = &starBuildDefinition{}
	_ common.RedistributableDefinition = &starBuildDefinition{}
	_ common.Directive                 = &starBuildDefinition{}
)

func newStarBuildDefinition(filename string, builder string, args []hash.SerializableValue) (common.StarBuildDefinition1, error) {
	if filename == "" || builder == "" {
		return nil, fmt.Errorf("no filename or builder passed to NewStarBuildDefinition")
	}

	return &starBuildDefinition{
		params: StarParameters{
			ScriptFilename: filename,
			BuilderName:    builder,
			Arguments:      args,
		},
	}, nil
}
