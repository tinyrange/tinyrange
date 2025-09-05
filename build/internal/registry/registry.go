package registry

import (
	"sync"

	"github.com/tinyrange/tinyrange/build/internal/common"
)

var registry = make(map[string]common.Builder)
var registryMtx sync.Mutex

func Register(typeName string, builder common.Builder) {
	registryMtx.Lock()
	defer registryMtx.Unlock()
	registry[typeName] = builder
}

func Get(typeName string) common.Builder {
	registryMtx.Lock()
	defer registryMtx.Unlock()
	return registry[typeName]
}
