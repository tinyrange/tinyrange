package star

import (
	"fmt"

	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/record"
	"go.starlark.net/starlark"
)

// Attr implements starlark.HasAttrs.
func BuildContextAttr(ctx common.BuildContext, name string) (starlark.Value, error) {
	if name == "recordwriter" {
		return starlark.NewBuiltin("BuildContext.recordwriter", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			f, err := ctx.CreateDefault()
			if err != nil {
				return nil, err
			}

			return record.NewWriter2(f), nil
		}), nil
	} else if name == "archive" {
		return starlark.NewBuiltin("BuildContext.archive", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				dir  *filesystem.StarDirectory
				kind string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"dir", &dir,
				"kind?", &kind,
			); err != nil {
				return starlark.None, err
			}

			if kind == "" {
				return builder.NewDirectoryToArchiveBuildResult(dir), nil
			} else {
				return starlark.None, fmt.Errorf("BuildContext.archive kind not implemented: %s", kind)
			}
		}), nil
	} else if name == "build" {
		return starlark.NewBuiltin("BuildContext.build", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				val starlark.Value
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"def", &val,
			); err != nil {
				return starlark.None, err
			}

			var buildDef common.BuildDefinition

			if def, ok := val.(common.BuildDefinition); ok {
				buildDef = def
			} else {
				return starlark.None, fmt.Errorf("could not convert %s to BuildDefinition", val.Type())
			}

			result, err := ctx.BuildChild(buildDef)
			if err != nil {
				return starlark.None, err
			}

			return buildDef.ToStarlark(result)
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func BuildContextAttrNames() []string {
	return []string{"recordwriter", "add_package", "build"}
}
