package builder

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/xi2/xz"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&DecompressFileBuildDefinition{})
}

type DecompressFileBuildDefinition struct {
	params DecompressFileParameters

	r io.ReadCloser
}

// implements common.BuildDefinition.
func (def *DecompressFileBuildDefinition) Params() hash.SerializableValue { return def.params }
func (def *DecompressFileBuildDefinition) SerializableType() string {
	return "DecompressFileBuildDefinition"
}
func (def *DecompressFileBuildDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &DecompressFileBuildDefinition{params: params.(DecompressFileParameters)}
}

// ToStarlark implements common.BuildDefinition.
func (def *DecompressFileBuildDefinition) ToStarlark(ctx common.BuildContext, result filesystem.File) (starlark.Value, error) {
	return filesystem.NewStarFile(result, def.Tag()), nil
}

// NeedsBuild implements BuildDefinition.
func (def *DecompressFileBuildDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	build, err := ctx.NeedsBuild(def.params.Base)
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
	f, err := ctx.BuildChild(def.params.Base)
	if err != nil {
		return nil, err
	}

	fh, err := f.Open()
	if err != nil {
		return nil, err
	}

	switch def.params.Kind {
	case ".xz":
		reader, err := xz.NewReader(fh, xz.DefaultDictMax)
		if err != nil {
			return nil, err
		}

		def.r = io.NopCloser(reader)
	default:
		return nil, fmt.Errorf("DecompressFile with unknown kind: %s", def.params.Kind)
	}

	return def, nil
}

// Tag implements BuildDefinition.
func (def *DecompressFileBuildDefinition) Tag() string {
	return strings.Join([]string{"DecompressFile", def.params.Base.Tag(), def.params.Kind}, "_")
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
	return &DecompressFileBuildDefinition{
		params: DecompressFileParameters{Base: base, Kind: kind},
	}
}
