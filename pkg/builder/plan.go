package builder

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"go.starlark.net/starlark"
)

type PlanDefinition struct {
	Builder string
	Search  []common.PackageQuery
	TagList common.TagList
	Debug   bool

	Fragments []config.Fragment
}

// Create implements common.BuildDefinition.
func (def *PlanDefinition) Create(params common.BuildDefinitionParameters) common.BuildDefinition {
	panic("unimplemented")
}

// Params implements common.BuildDefinition.
func (def *PlanDefinition) Params() common.BuildDefinitionParameters {
	panic("unimplemented")
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
			dir := common.NewMemoryDirectory()

			for _, frag := range def.Fragments {
				if frag.Archive != nil {
					ark, err := common.ReadArchiveFromFile(
						common.NewLocalFile(frag.Archive.HostFilename),
					)
					if err != nil {
						return starlark.None, err
					}

					if err := common.ExtractArchive(ark, dir); err != nil {
						return starlark.None, err
					}
				} else {
					return starlark.None, fmt.Errorf("unimplemented fragment type: %+v", frag)
				}
			}

			return common.NewStarDirectory(dir, ""), nil
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
	builder, err := ctx.Database().GetBuilder(def.Builder)
	if err != nil {
		return nil, err
	}

	plan, err := builder.Plan(def.Search, def.TagList, common.PlanOptions{
		Debug: def.Debug,
	})
	if err != nil {
		return nil, err
	}

	if err := plan.WriteTree(); err != nil {
		return nil, err
	}

	if def.Debug {
		return def, nil
	}

	for _, dir := range plan.Directives() {
		frags, err := directiveToFragments(ctx, dir)
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

func (def *PlanDefinition) String() string { return "PlanDefinition" }
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
)

func NewPlanDefinition(builder string, search []common.PackageQuery, tagList common.TagList) *PlanDefinition {
	return &PlanDefinition{
		Builder: builder,
		Search:  search,
		TagList: tagList,
	}
}
