package build

import (
	"google.golang.org/protobuf/proto"

	"github.com/tinyrange/tinyrange/build/internal"
	"github.com/tinyrange/tinyrange/build/internal/common"
	"github.com/tinyrange/tinyrange/build/internal/registry"
)

type Database = common.Database
type BuildCache = common.BuildCache
type Context = common.Context

func NewDatabase(cache BuildCache) (Database, error) {
	return internal.New(cache)
}

func RegisterBuilder(name string, definition proto.Message, builder common.Builder) {
	registry.Register(name, definition, builder)
}
