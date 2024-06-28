package builder

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tinyrange/pkg2/v2/common"
	"github.com/xi2/xz"
	"go.starlark.net/starlark"
)

type DecompressFileBuildDefinition struct {
	Base common.BuildDefinition
	Kind string

	r io.ReadCloser
}

// NeedsBuild implements BuildDefinition.
func (def *DecompressFileBuildDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
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
func (def *DecompressFileBuildDefinition) WriteTo(w io.Writer) (n int64, err error) {
	return io.Copy(w, def.r)
}

// Build implements BuildDefinition.
func (def *DecompressFileBuildDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
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

// Tag implements BuildDefinition.
func (def *DecompressFileBuildDefinition) Tag() string {
	return strings.Join([]string{"DecompressFile", def.Base.Tag(), def.Kind}, "_")
}

func (def *DecompressFileBuildDefinition) String() string { return def.Tag() }
func (*DecompressFileBuildDefinition) Type() string       { return "DecompressFileBuildDefinition" }
func (*DecompressFileBuildDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("DecompressFileBuildDefinition is not hashable")
}
func (*DecompressFileBuildDefinition) Truth() starlark.Bool { return starlark.True }
func (*DecompressFileBuildDefinition) Freeze()              {}

var (
	_ starlark.Value         = &DecompressFileBuildDefinition{}
	_ common.BuildDefinition = &DecompressFileBuildDefinition{}
	_ common.BuildResult     = &DecompressFileBuildDefinition{}
)

func NewDecompressFileBuildDefinition(base common.BuildDefinition, kind string) *DecompressFileBuildDefinition {
	return &DecompressFileBuildDefinition{Base: base, Kind: kind}
}
