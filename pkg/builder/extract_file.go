package builder

import (
	"fmt"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&extractFileDefinition{})
}

type extractFileDefinition struct {
	params ExtractFileParameters
}

// Build implements common.BuildDefinition.
func (def *extractFileDefinition) Build(ctx common.BuildContext1) (common.BuildResult, error) {
	base, err := ctx.BuildChild(def.params.Base)
	if err != nil {
		return nil, err
	}

	baseFile, err := base.Default()
	if err != nil {
		return nil, err
	}

	ark, err := filesystem.ReadArchiveFromFile(baseFile)
	if err != nil {
		return nil, err
	}

	ents, err := ark.Entries()
	if err != nil {
		return nil, err
	}

	for _, ent := range ents {
		if ent.Name() == def.params.Name {
			fh, err := ent.Open()
			if err != nil {
				return nil, err
			}

			return &copyFileResult{fh: fh}, nil
		}
	}

	return nil, fmt.Errorf("file %s not found", def.params.Name)
}

// NeedsBuild implements common.BuildDefinition.
func (def *extractFileDefinition) NeedsBuild(ctx common.BuildContext1) (bool, error) {
	return ctx.NeedsBuild(def.params.Base)
}

// Tag implements common.BuildDefinition.
func (def *extractFileDefinition) Tag() string {
	return fmt.Sprintf("ExtractFile_%s_%s", def.params.Base.Tag(), def.params.Name)
}

// ToStarlark implements common.BuildDefinition.
func (def *extractFileDefinition) ToStarlark(ctx common.BuildContext1, artifact common.BuildArtifact) (starlark.Value, error) {
	return nil, fmt.Errorf("ExtractFileDefinition can not be converted into a Starlark value")
}

// implements common.BuildDefinition.
func (def *extractFileDefinition) Params() hash.SerializableValue { return def.params }
func (def *extractFileDefinition) SerializableType() string {
	return "ExtractFileDefinition"
}
func (def *extractFileDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &extractFileDefinition{params: params.(ExtractFileParameters)}
}

var (
	_ common.BuildDefinition1 = &extractFileDefinition{}
)

func newExtractFileDefinition(base common.BuildDefinition1, name string) common.BuildDefinition1 {
	return &extractFileDefinition{params: ExtractFileParameters{Base: base, Name: name}}
}
