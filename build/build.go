package build

import (
	"github.com/tinyrange/tinyrange/build/internal"
	"github.com/tinyrange/tinyrange/build/internal/common"
)

type Database = common.Database
type BuildCache = common.BuildCache

func NewDatabase(cache BuildCache) (Database, error) {
	return internal.New(cache)
}
