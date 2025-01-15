package builder

import (
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/xi2/xz"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&decompressFileBuildDefinition{})
}

type decompressFileBuildDefinition struct {
	params DecompressFileParameters

	r io.ReadCloser
}

// implements common.BuildDefinition.
func (def *decompressFileBuildDefinition) Params() hash.SerializableValue { return def.params }
func (def *decompressFileBuildDefinition) SerializableType() string {
	return "DecompressFileBuildDefinition"
}
func (def *decompressFileBuildDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &decompressFileBuildDefinition{params: params.(DecompressFileParameters)}
}

// ToStarlark implements common.BuildDefinition.
func (def *decompressFileBuildDefinition) ToStarlark(ctx common.BuildContext1, artifact common.BuildArtifact) (starlark.Value, error) {
	result, err := artifact.Default()
	if err != nil {
		return nil, err
	}

	return filesystem.NewStarFile(result, artifact.Hash().String()), nil
}

// NeedsBuild implements BuildDefinition.
func (def *decompressFileBuildDefinition) NeedsBuild(ctx common.BuildContext1) (bool, error) {
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
func (def *decompressFileBuildDefinition) WriteResult(w io.Writer) error {
	if _, err := io.Copy(w, def.r); err != nil {
		return err
	}

	return nil
}

// Build implements BuildDefinition.
func (def *decompressFileBuildDefinition) Build(ctx common.BuildContext1) (common.BuildResult, error) {
	art, err := ctx.BuildChild(def.params.Base)
	if err != nil {
		return nil, err
	}

	f, err := art.Default()
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
	case ".gz":
		reader, err := gzip.NewReader(fh)
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
func (def *decompressFileBuildDefinition) Tag() string {
	return strings.Join([]string{"DecompressFile", def.params.Base.Tag(), def.params.Kind}, "_")
}

func (def *decompressFileBuildDefinition) String() string { return def.Tag() }
func (*decompressFileBuildDefinition) Type() string       { return "DecompressFileBuildDefinition" }
func (*decompressFileBuildDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("DecompressFileBuildDefinition is not hashable")
}
func (*decompressFileBuildDefinition) Truth() starlark.Bool { return starlark.True }
func (*decompressFileBuildDefinition) Freeze()              {}

var (
	_ starlark.Value          = &decompressFileBuildDefinition{}
	_ common.BuildDefinition1 = &decompressFileBuildDefinition{}
	_ common.BuildResult      = &decompressFileBuildDefinition{}
)

func newDecompressFileBuildDefinition(base common.BuildDefinition1, kind string) common.StarBuildDefinition1 {
	return &decompressFileBuildDefinition{
		params: DecompressFileParameters{Base: base, Kind: kind},
	}
}
