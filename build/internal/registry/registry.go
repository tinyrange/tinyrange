package registry

import (
	"sync"

	gproto "google.golang.org/protobuf/proto"

	"github.com/tinyrange/tinyrange/build/internal/common"
)

var registry = make(map[string]common.BuilderMetadata)
var registryMtx sync.Mutex

func Register(typeName string, definition gproto.Message, builder common.Builder) {
	registryMtx.Lock()
	defer registryMtx.Unlock()
	registry[typeName] = common.BuilderMetadata{
		Builder:    builder,
		TypeName:   typeName,
		Definition: definition,
	}
}

func GetAll() []common.BuilderMetadata {
	registryMtx.Lock()
	defer registryMtx.Unlock()
	builders := make([]common.BuilderMetadata, 0, len(registry))
	for _, b := range registry {
		builders = append(builders, b)
	}
	return builders
}

func Get(typeName string) (common.BuilderMetadata, bool) {
	registryMtx.Lock()
	defer registryMtx.Unlock()
	return registry[typeName], registry[typeName].Builder != nil
}
