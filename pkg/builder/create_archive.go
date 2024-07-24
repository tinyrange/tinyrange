package builder

import (
	"fmt"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"go.starlark.net/starlark"
)

type CreateArchiveDefinition struct {
	Dir common.Directory
}

// Create implements common.BuildDefinition.
func (def *CreateArchiveDefinition) Create(params common.BuildDefinitionParameters) common.BuildDefinition {
	panic("unimplemented")
}

// Params implements common.BuildDefinition.
func (def *CreateArchiveDefinition) Params() common.BuildDefinitionParameters {
	panic("unimplemented")
}

// Build implements common.BuildDefinition.
func (def *CreateArchiveDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	return &directoryToArchiveBuildResult{dir: def.Dir}, nil
}

// NeedsBuild implements common.BuildDefinition.
func (def *CreateArchiveDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	if ctx.Database().ShouldRebuildUserDefinitions() {
		return true, nil
	}

	// TODO(joshua): Check if any of the child directives
	return false, nil
}

func (def *CreateArchiveDefinition) String() string { return "CreateArchiveDefinition" }
func (*CreateArchiveDefinition) Type() string       { return "CreateArchiveDefinition" }
func (*CreateArchiveDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("CreateArchiveDefinition is not hashable")
}
func (*CreateArchiveDefinition) Truth() starlark.Bool { return starlark.True }
func (*CreateArchiveDefinition) Freeze()              {}

var (
	_ starlark.Value         = &CreateArchiveDefinition{}
	_ common.BuildDefinition = &CreateArchiveDefinition{}
)

func NewCreateArchiveDefinition(dir common.Directory) *CreateArchiveDefinition {
	return &CreateArchiveDefinition{Dir: dir}
}
