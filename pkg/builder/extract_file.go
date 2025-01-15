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

// Dependencies implements common.BuildDefinition.
func (def *extractFileDefinition) Dependencies() ([]common.BuildDefinition, error) {
	return []common.BuildDefinition{def.params.Base}, nil
}

// Build implements common.BuildDefinition.
func (def *extractFileDefinition) Build(ctx common.BuildContext) error {
	base, err := ctx.BuildChild(def.params.Base)
	if err != nil {
		return err
	}

	baseFile, err := base.Default()
	if err != nil {
		return err
	}

	ark, err := filesystem.ReadArchiveFromFile(baseFile)
	if err != nil {
		return err
	}

	ents, err := ark.Entries()
	if err != nil {
		return err
	}

	for _, ent := range ents {
		if ent.Name() == def.params.Name {
			fh, err := ent.Open()
			if err != nil {
				return err
			}

			return ctx.WriteDefault(&copyFileResult{fh: fh})
		}
	}

	return fmt.Errorf("file %s not found", def.params.Name)
}

// NeedsBuild implements common.BuildDefinition.
func (def *extractFileDefinition) NeedsBuild(ctx common.BuildContext) (bool, error) {
	return false, nil
}

// ToStarlark implements common.BuildDefinition.
func (def *extractFileDefinition) ToStarlark(ctx common.BuildContext, artifact common.BuildArtifact) (starlark.Value, error) {
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

// String implements the fmt.Stringer interface.
func (def *extractFileDefinition) String() string {
	return fmt.Sprintf("ExtractFile{%s}", def.params.Name)
}

var (
	_ common.BuildDefinition = &extractFileDefinition{}
)

func newExtractFileDefinition(base common.BuildDefinition, name string) common.BuildDefinition {
	return &extractFileDefinition{params: ExtractFileParameters{Base: base, Name: name}}
}
