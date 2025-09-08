package builder

import (
	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/internal/registry"
	"github.com/tinyrange/tinyrange/build/proto"
)

type writeFileBuilder struct{}

// Build implements common.Builder.
func (w *writeFileBuilder) Build(ctx common.Context) error {
	var params proto.WriteFileDefinition
	if err := ctx.Decode(&params); err != nil {
		return err
	}

	f, err := ctx.Create(common.FileType_Plain)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(params.Content); err != nil {
		return err
	}

	return nil
}

func init() {
	registry.Register(common.TYPE_NAME_WRITE_FILE, &writeFileBuilder{})
}
