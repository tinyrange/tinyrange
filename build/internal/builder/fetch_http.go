package builder

import (
	"fmt"

	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/internal/registry"
	"github.com/tinyrange/tinyrange/build/proto"
)

type fetchHttpBuilder struct {
}

// Build implements common.Builder.
func (f *fetchHttpBuilder) Build(ctx common.Context) error {
	var params proto.FetchHttpDefinition
	if err := ctx.Decode(&params); err != nil {
		return err
	}

	return fmt.Errorf("fetch http not implemented")
}

func init() {
	registry.Register(common.TYPE_NAME_FETCH_HTTP, &fetchHttpBuilder{})
}
