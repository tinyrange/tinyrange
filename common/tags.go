package common

import (
	"fmt"

	"go.starlark.net/starlark"
)

type TagList []string

// Attr implements starlark.HasAttrs.
func (lst TagList) Attr(name string) (starlark.Value, error) {
	if name == "contains" {
		return starlark.NewBuiltin("TagList.contains", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				search string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"search", &search,
			); err != nil {
				return starlark.None, err
			}

			for _, name := range lst {
				if search == name {
					return starlark.True, nil
				}
			}

			return starlark.False, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (lst TagList) AttrNames() []string {
	return []string{"contains"}
}

func (lst TagList) Matches(other TagList) bool {
	otherMap := make(map[string]bool)
	for _, k := range other {
		otherMap[k] = true
	}

	for _, k := range lst {
		if _, ok := otherMap[k]; !ok {
			return false
		}
	}

	return true
}

func (lst TagList) String() string { return fmt.Sprintf("TagList{%+v}", []string(lst)) }
func (TagList) Type() string       { return "TagList" }
func (TagList) Hash() (uint32, error) {
	return 0, fmt.Errorf("TagList is not hashable")
}
func (TagList) Truth() starlark.Bool { return starlark.True }
func (TagList) Freeze()              {}

var (
	_ starlark.Value    = TagList{}
	_ starlark.HasAttrs = TagList{}
)
