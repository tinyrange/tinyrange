package database

import (
	"errors"
	"fmt"
	"time"

	"github.com/tinyrange/pkg2/v2/builder"
	"github.com/tinyrange/pkg2/v2/common"
	"github.com/tinyrange/pkg2/v2/emulator"
	"github.com/tinyrange/pkg2/v2/filesystem"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func (db *PackageDatabase) getGlobals(name string) starlark.StringDict {
	ret := starlark.StringDict{}

	ret["__name__"] = starlark.String(name)

	ret["json"] = starlarkjson.Module

	ret["db"] = db

	ret["define"] = &starlarkstruct.Module{
		Name: "define",
		Members: starlark.StringDict{
			"build": starlark.NewBuiltin("define.build", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				buildCallable := args[0]
				buildArgs := args[1:]

				return builder.NewStarBuildDefinition(thread.Name, buildCallable, buildArgs)
			}),
			"package_collection": starlark.NewBuiltin("define.package_collection", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				parser := args[0]

				var defs []common.BuildDefinition

				for _, arg := range args[1:] {
					def, ok := arg.(common.BuildDefinition)
					if !ok {
						return starlark.None, fmt.Errorf("could not convert %s to BuildDefinition", arg.Type())
					}

					defs = append(defs, def)
				}

				return NewPackageCollection(thread.Name, parser, defs)
			}),
			"container_builder": starlark.NewBuiltin("define.container_builder", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					name             string
					displayName      string
					baseDirectivesIt starlark.Iterable
					packages         *PackageCollection
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"name", &name,
					"display_name?", &displayName,
					"base_directives?", &baseDirectivesIt,
					"packages?", &packages,
				); err != nil {
					return starlark.None, err
				}

				iter := baseDirectivesIt.Iterate()
				defer iter.Done()

				var baseDirectives []common.Directive

				var val starlark.Value
				for iter.Next(&val) {
					dir, ok := val.(*common.StarDirective)
					if !ok {
						return nil, fmt.Errorf("could not convert %s to Directive", val.Type())
					}

					baseDirectives = append(baseDirectives, dir.Directive)
				}

				return NewContainerBuilder(name, displayName, baseDirectives, packages)
			}),
			"fetch_http": starlark.NewBuiltin("define.fetch_http", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					url        string
					expireTime int64
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"url", &url,
					"expire_time?", &expireTime,
				); err != nil {
					return starlark.None, err
				}

				return builder.NewFetchHttpBuildDefinition(url, time.Duration(expireTime)), nil
			}),
			"read_archive": starlark.NewBuiltin("define.read_archive", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					val  starlark.Value
					kind string
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"def", &val,
					"kind", &kind,
				); err != nil {
					return starlark.None, err
				}

				if def, ok := val.(common.BuildDefinition); ok {
					return builder.NewReadArchiveBuildDefinition(def, kind), nil
				} else if file, ok := val.(filesystem.File); ok {
					fileDef := builder.NewFileDefinition(file)

					return builder.NewReadArchiveBuildDefinition(fileDef, kind), nil
				} else {
					return starlark.None, fmt.Errorf("expected BuildDefinition got %s", val.Type())
				}
			}),
			"decompress_file": starlark.NewBuiltin("define.decompress_file", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					def  common.BuildDefinition
					kind string
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"def", &def,
					"kind", &kind,
				); err != nil {
					return starlark.None, err
				}

				decompressDef := builder.NewDecompressFileBuildDefinition(def, kind)

				return decompressDef, nil
			}),
		},
	}

	ret["directive"] = &starlarkstruct.Module{
		Name: "directive",
		Members: starlark.StringDict{
			"base_image": starlark.NewBuiltin("directive.base_image", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					image string
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"image", &image,
				); err != nil {
					return starlark.None, err
				}

				return &common.StarDirective{Directive: common.DirectiveBaseImage(image)}, nil
			}),
			"run_command": starlark.NewBuiltin("directive.run_command", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					command string
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"command", &command,
				); err != nil {
					return starlark.None, err
				}

				return &common.StarDirective{Directive: common.DirectiveRunCommand(command)}, nil
			}),
		},
	}

	ret["package"] = starlark.NewBuiltin("package", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			name          common.PackageName
			directiveList starlark.Iterable
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"name", &name,
			"directives", &directiveList,
		); err != nil {
			return starlark.None, err
		}

		iter := directiveList.Iterate()
		defer iter.Done()

		var directives []common.Directive

		var val starlark.Value
		for iter.Next(&val) {
			dir, ok := val.(*common.StarDirective)
			if !ok {
				return nil, fmt.Errorf("could not convert %s to Directive", val.Type())
			}

			directives = append(directives, dir.Directive)
		}

		return common.NewPackage(name, directives), nil
	})

	ret["name"] = starlark.NewBuiltin("name", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			name    string
			version string
			tags    starlark.Iterable
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"name", &name,
			"version", &version,
			"tags?", &tags,
		); err != nil {
			return starlark.None, err
		}

		var stringTags []string

		if tags != nil {
			var err error

			stringTags, err = common.ToStringList(tags)
			if err != nil {
				return starlark.None, err
			}
		}

		return db.NewName(name, version, stringTags)
	})

	ret["duration"] = starlark.NewBuiltin("duration", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			dur string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"dur", &dur,
		); err != nil {
			return starlark.None, err
		}

		d, err := time.ParseDuration(dur)
		if err != nil {
			return starlark.None, err
		}

		return starlark.MakeInt64(int64(d)), nil
	})

	ret["filesystem"] = starlark.NewBuiltin("filesystem", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			ark *filesystem.StarArchive
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"ark?", &ark,
		); err != nil {
			return starlark.None, err
		}

		dir := filesystem.NewMemoryDirectory()

		if ark != nil {
			if err := filesystem.ExtractArchive(ark, dir); err != nil {
				return starlark.None, err
			}
		}

		return filesystem.NewStarDirectory(dir, ""), nil
	})

	ret["file"] = starlark.NewBuiltin("file", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			contents   string
			executable bool
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"contents", &contents,
			"executable?", &executable,
		); err != nil {
			return starlark.None, err
		}

		f := filesystem.NewMemoryFile()

		return filesystem.NewStarFile(f, ""), nil
	})

	ret["emulator"] = starlark.NewBuiltin("emulator", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			root *filesystem.StarDirectory
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"root", &root,
		); err != nil {
			return starlark.None, err
		}

		return emulator.New(root), nil
	})

	ret["program"] = starlark.NewBuiltin("program", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			name     string
			callable starlark.Callable
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"name", &name,
			"callable", &callable,
		); err != nil {
			return starlark.None, err
		}

		return emulator.NewStarProgram(name, callable)
	})

	ret["error"] = starlark.NewBuiltin("error", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			message string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"message", &message,
		); err != nil {
			return starlark.None, err
		}

		return starlark.None, errors.New(message)
	})

	return ret
}
