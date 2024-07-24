package builder

import (
	"fmt"
	"io"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/xi2/xz"
	"go.starlark.net/starlark"
)

type DecompressFileDefinition struct {
	Base common.BuildDefinition
	Kind string

	r io.ReadCloser
}

// Create implements common.BuildDefinition.
func (def *DecompressFileDefinition) Create(params common.BuildDefinitionParameters) common.BuildDefinition {
	panic("unimplemented")
}

// Params implements common.BuildDefinition.
func (def *DecompressFileDefinition) Params() common.BuildDefinitionParameters {
	panic("unimplemented")
}

// NeedsBuild implements BuildDefinition.
func (def *DecompressFileDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	build, err := ctx.NeedsBuild(def.Base)
	if err != nil {
		return true, err
	}

	if build {
		return true, nil
	} else {
		return false, nil // compressed files don't need to be re-extracted unless the underlying file changes.
	}
}

// WriteTo implements BuildResult.
func (def *DecompressFileDefinition) WriteTo(w io.Writer) (n int64, err error) {
	return io.Copy(w, def.r)
}

// Build implements BuildDefinition.
func (def *DecompressFileDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	f, err := ctx.BuildChild(def.Base)
	if err != nil {
		return nil, err
	}

	fh, err := f.Open()
	if err != nil {
		return nil, err
	}

	switch def.Kind {
	case ".xz":
		reader, err := xz.NewReader(fh, xz.DefaultDictMax)
		if err != nil {
			return nil, err
		}

		def.r = io.NopCloser(reader)
	default:
		return nil, fmt.Errorf("DecompressFile with unknown kind: %s", def.Kind)
	}

	return def, nil
}

func (def *DecompressFileDefinition) String() string { return "DecompressFileBuildDefinition" }
func (*DecompressFileDefinition) Type() string       { return "DecompressFileBuildDefinition" }
func (*DecompressFileDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("DecompressFileBuildDefinition is not hashable")
}
func (*DecompressFileDefinition) Truth() starlark.Bool { return starlark.True }
func (*DecompressFileDefinition) Freeze()              {}

var (
	_ starlark.Value         = &DecompressFileDefinition{}
	_ common.BuildDefinition = &DecompressFileDefinition{}
	_ common.BuildResult     = &DecompressFileDefinition{}
)

func NewDecompressFileBuildDefinition(base common.BuildDefinition, kind string) *DecompressFileDefinition {
	return &DecompressFileDefinition{Base: base, Kind: kind}
}
