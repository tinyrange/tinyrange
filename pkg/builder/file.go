package builder

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"go.starlark.net/starlark"
)

type FileDefinition struct {
	f filesystem.File

	fh filesystem.FileHandle
}

// WriteTo implements common.BuildResult.
func (def *FileDefinition) WriteTo(w io.Writer) (n int64, err error) {
	return io.Copy(w, def.fh)
}

// Build implements common.BuildDefinition.
func (def *FileDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	fh, err := def.f.Open()
	if err != nil {
		return nil, err
	}

	def.fh = fh

	return def, nil
}

// NeedsBuild implements common.BuildDefinition.
func (def *FileDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	info, err := def.f.Stat()
	if err != nil {
		return true, err
	}

	return info.ModTime().After(cacheTime), nil
}

// Tag implements common.BuildDefinition.
func (def *FileDefinition) Tag() string {
	info, err := def.f.Stat()
	if err != nil {
		return "<unknown>"
	}

	return strings.Join([]string{info.Name()}, "_")
}

func (def *FileDefinition) String() string { return def.Tag() }
func (*FileDefinition) Type() string       { return "FileDefinition" }
func (*FileDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("FileDefinition is not hashable")
}
func (*FileDefinition) Truth() starlark.Bool { return starlark.True }
func (*FileDefinition) Freeze()              {}

var (
	_ starlark.Value         = &FileDefinition{}
	_ common.BuildDefinition = &FileDefinition{}
	_ common.BuildResult     = &FileDefinition{}
)

func NewFileDefinition(f filesystem.File) *FileDefinition {
	return &FileDefinition{f: f}
}
