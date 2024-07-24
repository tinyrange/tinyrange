package builder

import (
	"fmt"
	"io"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"go.starlark.net/starlark"
)

type copyFileBuildResult struct {
	fh common.FileHandle
}

// WriteTo implements common.BuildResult.
func (def *copyFileBuildResult) WriteTo(w io.Writer) (n int64, err error) {
	return io.Copy(w, def.fh)
}

var (
	_ common.BuildResult = &copyFileBuildResult{}
)

type FileDefinition struct {
	f common.File
}

// Create implements common.BuildDefinition.
func (def *FileDefinition) Create(params common.BuildDefinitionParameters) common.BuildDefinition {
	panic("unimplemented")
}

// Params implements common.BuildDefinition.
func (def *FileDefinition) Params() common.BuildDefinitionParameters {
	panic("unimplemented")
}

// Build implements common.BuildDefinition.
func (def *FileDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	fh, err := def.f.Open()
	if err != nil {
		return nil, err
	}

	return &copyFileBuildResult{fh: fh}, nil
}

// NeedsBuild implements common.BuildDefinition.
func (def *FileDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	info, err := def.f.Stat()
	if err != nil {
		return true, err
	}

	return info.ModTime().After(cacheTime), nil
}

func (def *FileDefinition) String() string { return "FileDefinition" }
func (*FileDefinition) Type() string       { return "FileDefinition" }
func (*FileDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("FileDefinition is not hashable")
}
func (*FileDefinition) Truth() starlark.Bool { return starlark.True }
func (*FileDefinition) Freeze()              {}

var (
	_ starlark.Value         = &FileDefinition{}
	_ common.BuildDefinition = &FileDefinition{}
)

func NewFileDefinition(f common.File) *FileDefinition {
	return &FileDefinition{f: f}
}
