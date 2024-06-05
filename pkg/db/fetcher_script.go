package db

import (
	"fmt"

	"go.starlark.net/starlark"
)

type ScriptFetcher struct {
	db   *PackageDatabase
	Name string
	Func *starlark.Function
	Args starlark.Tuple
}

// Attr implements starlark.HasAttrs.
func (s *ScriptFetcher) Attr(name string) (starlark.Value, error) {
	return nil, nil
}

// AttrNames implements starlark.HasAttrs.
func (s *ScriptFetcher) AttrNames() []string {
	return []string{}
}

func (*ScriptFetcher) String() string { return "ScriptFetcher" }
func (*ScriptFetcher) Type() string   { return "ScriptFetcher" }
func (*ScriptFetcher) Hash() (uint32, error) {
	return 0, fmt.Errorf("ScriptFetcher is not hashable")
}
func (*ScriptFetcher) Truth() starlark.Bool { return starlark.True }
func (*ScriptFetcher) Freeze()              {}

var (
	_ starlark.Value    = &ScriptFetcher{}
	_ starlark.HasAttrs = &ScriptFetcher{}
)
