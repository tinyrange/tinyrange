package build

import (
	"github.com/tinyrange/tinyrange/build/internal"
	"github.com/tinyrange/tinyrange/build/internal/common"
)

type Database = common.Database

func NewDatabase() (Database, error) {
	return internal.New()
}
