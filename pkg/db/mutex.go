package db

import (
	"fmt"
	"sync"

	"go.starlark.net/starlark"
)

type StarMutex struct {
	mtx sync.Mutex
}

// Attr implements starlark.HasAttrs.
func (f *StarMutex) Attr(name string) (starlark.Value, error) {
	if name == "lock" {
		return starlark.NewBuiltin("lock", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			f.mtx.Lock()

			return starlark.None, nil
		}), nil
	} else if name == "unlock" {
		return starlark.NewBuiltin("unlock", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			f.mtx.Unlock()

			return starlark.None, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (f *StarMutex) AttrNames() []string {
	return []string{"lock", "unlock"}
}

func (*StarMutex) String() string        { return "Mutex" }
func (*StarMutex) Type() string          { return "Mutex" }
func (*StarMutex) Hash() (uint32, error) { return 0, fmt.Errorf("mutex is not hashable") }
func (*StarMutex) Truth() starlark.Bool  { return starlark.True }
func (*StarMutex) Freeze()               {}

var (
	_ starlark.Value    = &StarMutex{}
	_ starlark.HasAttrs = &StarMutex{}
)
