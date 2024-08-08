package builder

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&PlanDefinition{})
}

type PlanDefinition struct {
	params PlanParameters

	Fragments []config.Fragment
}

// Dependencies implements common.BuildDefinition.
func (def *PlanDefinition) Dependencies(ctx common.BuildContext) ([]common.DependencyNode, error) {
	// The builder is a dynamic dependency.
	return []common.DependencyNode{}, nil
}

// implements common.BuildDefinition.
func (def *PlanDefinition) Params() hash.SerializableValue { return def.params }
func (def *PlanDefinition) SerializableType() string       { return "PlanDefinition" }
func (def *PlanDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &PlanDefinition{params: params.(PlanParameters)}
}

// AsFragments implements common.Directive.
func (def *PlanDefinition) AsFragments(ctx common.BuildContext) ([]config.Fragment, error) {
	res, err := ctx.BuildChild(def)
	if err != nil {
		return nil, err
	}

	if err := ParseJsonFromFile(res, &def); err != nil {
		return nil, err
	}

	return def.Fragments, nil
}

// ToStarlark implements common.BuildDefinition.
func (def *PlanDefinition) ToStarlark(ctx common.BuildContext, result filesystem.File) (starlark.Value, error) {
	var plan *PlanDefinition

	if err := ParseJsonFromFile(result, &plan); err != nil {
		return nil, err
	}

	return plan, nil
}

// Attr implements starlark.HasAttrs.
func (def *PlanDefinition) Attr(name string) (starlark.Value, error) {
	if name == "filesystem" {
		return starlark.NewBuiltin("PlanDefinition.filesystem", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			dir := filesystem.NewMemoryDirectory()

			for _, frag := range def.Fragments {
				if frag.Archive != nil {
					ark, err := filesystem.ReadArchiveFromFile(
						filesystem.NewLocalFile(frag.Archive.HostFilename, nil),
					)
					if err != nil {
						return starlark.None, err
					}

					if err := filesystem.ExtractArchive(ark, dir); err != nil {
						return starlark.None, err
					}
				} else {
					return starlark.None, fmt.Errorf("unimplemented fragment type: %+v", frag)
				}
			}

			return filesystem.NewStarDirectory(dir, ""), nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (def *PlanDefinition) AttrNames() []string {
	return []string{"filesystem"}
}

// WriteTo implements common.BuildResult.
func (def *PlanDefinition) WriteTo(w io.Writer) (n int64, err error) {
	bytes, err := json.Marshal(&def)
	if err != nil {
		return 0, err
	}

	childN, err := w.Write(bytes)
	return int64(childN), err
}

// Build implements common.BuildDefinition.
func (def *PlanDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	builder, err := ctx.Database().GetContainerBuilder(ctx, def.params.Builder)
	if err != nil {
		return nil, err
	}

	plan, err := builder.Plan(ctx, def.params.Search, def.params.TagList, common.PlanOptions{})
	if err != nil {
		plan, _ = builder.Plan(ctx, def.params.Search, def.params.TagList, common.PlanOptions{
			Debug: true,
		})

		plan.WriteTree()

		return nil, err
	}

	if err := plan.WriteTree(); err != nil {
		return nil, err
	}

	for _, dir := range plan.Directives() {
		frags, err := dir.AsFragments(ctx)
		if err != nil {
			return nil, err
		}

		def.Fragments = append(def.Fragments, frags...)
	}

	return def, nil
}

// NeedsBuild implements common.BuildDefinition.
func (def *PlanDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	if ctx.Database().ShouldRebuildUserDefinitions() {
		return true, nil
	}

	return false, nil
}

// Tag implements common.BuildDefinition.
func (def *PlanDefinition) Tag() string {
	return strings.Join([]string{
		"PlanDefinition",
		def.params.Builder,
		fmt.Sprintf("%+v", def.params.Search),
		def.params.TagList.String(),
	}, "_")
}

func (def *PlanDefinition) String() string { return def.Tag() }
func (*PlanDefinition) Type() string       { return "PlanDefinition" }
func (*PlanDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("PlanDefinition is not hashable")
}
func (*PlanDefinition) Truth() starlark.Bool { return starlark.True }
func (*PlanDefinition) Freeze()              {}

var (
	_ starlark.Value         = &PlanDefinition{}
	_ starlark.HasAttrs      = &PlanDefinition{}
	_ common.BuildDefinition = &PlanDefinition{}
	_ common.BuildResult     = &PlanDefinition{}
	_ common.Directive       = &PlanDefinition{}
)

func NewPlanDefinition(builder string, search []common.PackageQuery, tagList common.TagList) *PlanDefinition {
	return &PlanDefinition{
		params: PlanParameters{
			Builder: builder,
			Search:  search,
			TagList: tagList,
		},
	}
}
