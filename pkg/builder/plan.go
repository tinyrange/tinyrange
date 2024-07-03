package builder

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"go.starlark.net/starlark"
)

type PlanDefinition struct {
	builder string
	search  []common.PackageQuery
	tagList common.TagList

	Fragments []config.Fragment
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
	builder, err := ctx.Database().GetBuilder(def.builder)
	if err != nil {
		return nil, err
	}

	plan, err := builder.Plan(def.search, def.tagList)
	if err != nil {
		return nil, err
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

// Tag implements common.BuildDefinition.
func (def *PlanDefinition) Tag() string {
	return strings.Join([]string{
		"PlanDefinition",
		def.builder,
		fmt.Sprintf("%+v", def.search),
		def.tagList.String(),
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
	_ common.BuildDefinition = &PlanDefinition{}
	_ common.BuildResult     = &PlanDefinition{}
)

func NewPlanDefinition(builder string, search []common.PackageQuery, tagList common.TagList) *PlanDefinition {
	return &PlanDefinition{
		builder: builder,
		search:  search,
		tagList: tagList,
	}
}
