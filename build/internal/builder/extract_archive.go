package builder

import (
	"fmt"

	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/internal/registry"
	"github.com/tinyrange/tinyrange/build/proto"
)

type extractArchiveBuilder struct {
}

// Build implements common.Builder.
func (e *extractArchiveBuilder) Build(ctx common.Context) error {
	var params proto.ExtractArchiveDefinition
	if err := ctx.Decode(&params); err != nil {
		return err
	}

	return fmt.Errorf("extract archive not implemented")
}

func init() {
	registry.Register(common.TYPE_NAME_EXTRACT_ARCHIVE, &extractArchiveBuilder{})
}
