package builder

import (
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem/star"
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

// Dependencies implements common.BuildDefinition.
func (def *decompressFileBuildDefinition) Dependencies() ([]common.BuildDefinition, error) {
	return []common.BuildDefinition{def.params.Base}, nil
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
func (def *decompressFileBuildDefinition) ToStarlark(artifact common.BuildArtifact) (starlark.Value, error) {
	result, err := artifact.Default()
	if err != nil {
		return nil, err
	}

	return star.NewStarFile(result, artifact.DefinitionHash().String()), nil
}

// NeedsBuild implements BuildDefinition.
func (def *decompressFileBuildDefinition) NeedsBuild(ctx common.BuildContext) (bool, error) {
	return false, nil
}

// WriteTo implements BuildResult.
func (def *decompressFileBuildDefinition) WriteResult(w io.Writer) error {
	if _, err := io.Copy(w, def.r); err != nil {
		return err
	}

	return nil
}

// Build implements BuildDefinition.
func (def *decompressFileBuildDefinition) Build(ctx common.BuildContext) error {
	art, err := ctx.BuildChild(def.params.Base)
	if err != nil {
		return err
	}

	f, err := art.Default()
	if err != nil {
		return err
	}

	fh, err := f.Open()
	if err != nil {
		return err
	}

	switch def.params.Kind {
	case ".xz":
		reader, err := xz.NewReader(fh, xz.DefaultDictMax)
		if err != nil {
			return err
		}

		def.r = io.NopCloser(reader)
	case ".gz":
		reader, err := gzip.NewReader(fh)
		if err != nil {
			return err
		}

		def.r = io.NopCloser(reader)
	case ".zlib":
		reader, err := zlib.NewReader(fh)
		if err != nil {
			return err
		}

		def.r = io.NopCloser(reader)
	default:
		return fmt.Errorf("DecompressFile with unknown kind: %s", def.params.Kind)
	}

	return ctx.WriteDefault(def)
}

func (def *decompressFileBuildDefinition) String() string {
	return fmt.Sprintf("DecompressFile_%s_%s", def.params.Base.String(), def.params.Kind)
}
func (*decompressFileBuildDefinition) Type() string { return "DecompressFileBuildDefinition" }
func (*decompressFileBuildDefinition) Hash() (uint32, error) {
	return 0, fmt.Errorf("DecompressFileBuildDefinition is not hashable")
}
func (*decompressFileBuildDefinition) Truth() starlark.Bool { return starlark.True }
func (*decompressFileBuildDefinition) Freeze()              {}

var (
	_ starlark.Value         = &decompressFileBuildDefinition{}
	_ common.BuildDefinition = &decompressFileBuildDefinition{}
	_ common.BuildResult     = &decompressFileBuildDefinition{}
)

func newDecompressFileBuildDefinition(base common.BuildDefinition, kind string) common.StarBuildDefinition {
	return &decompressFileBuildDefinition{
		params: DecompressFileParameters{Base: base, Kind: kind},
	}
}
