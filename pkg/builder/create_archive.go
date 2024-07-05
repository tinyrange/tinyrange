package builder

import (
	"fmt"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"go.starlark.net/starlark"
)

type CreateArchiveDefinition struct {
	Dir filesystem.Directory
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

// Tag implements common.BuildDefinition.
func (def *CreateArchiveDefinition) Tag() string {
	// TODO(joshua): This needs to be better implemented.
	return fmt.Sprintf("CreateArchive_%+v", def.Dir)
}

func (def *CreateArchiveDefinition) String() string { return def.Tag() }
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

func NewCreateArchiveDefinition(dir filesystem.Directory) *CreateArchiveDefinition {
	return &CreateArchiveDefinition{Dir: dir}
}
