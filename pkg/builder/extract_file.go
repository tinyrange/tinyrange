package builder

import (
	"fmt"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

func init() {
	hash.RegisterType(&ExtractFileDefinition{})
}

type ExtractFileDefinition struct {
	params ExtractFileParameters
}

// Build implements common.BuildDefinition.
func (def *ExtractFileDefinition) Build(ctx common.BuildContext) (common.BuildResult, error) {
	base, err := ctx.BuildChild(def.params.Base)
	if err != nil {
		return nil, err
	}

	ark, err := filesystem.ReadArchiveFromFile(base)
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
func (def *ExtractFileDefinition) NeedsBuild(ctx common.BuildContext, cacheTime time.Time) (bool, error) {
	return ctx.NeedsBuild(def.params.Base)
}

// Tag implements common.BuildDefinition.
func (def *ExtractFileDefinition) Tag() string {
	return fmt.Sprintf("ExtractFile_%s_%s", def.params.Base.Tag(), def.params.Name)
}

// ToStarlark implements common.BuildDefinition.
func (def *ExtractFileDefinition) ToStarlark(ctx common.BuildContext, result filesystem.File) (starlark.Value, error) {
	return nil, fmt.Errorf("ExtractFileDefinition can not be converted into a Starlark value")
}

// implements common.BuildDefinition.
func (def *ExtractFileDefinition) Params() hash.SerializableValue { return def.params }
func (def *ExtractFileDefinition) SerializableType() string {
	return "ExtractFileDefinition"
}
func (def *ExtractFileDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &ExtractFileDefinition{params: params.(ExtractFileParameters)}
}

var (
	_ common.BuildDefinition = &ExtractFileDefinition{}
)

func NewExtractFileDefinition(base common.BuildDefinition, name string) *ExtractFileDefinition {
	return &ExtractFileDefinition{params: ExtractFileParameters{Base: base, Name: name}}
}
