package database

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/emulator"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

var START_TIME = time.Now()

func asDirective(val starlark.Value) (common.Directive, error) {
	if starDir, ok := val.(*common.StarDirective); ok {
		return starDir.Directive, nil
	} else if directive, ok := val.(common.Directive); ok {
		return directive, nil
	} else if file, ok := val.(filesystem.File); ok {
		return builder.NewFileDefinition(file), nil
	} else {
		return nil, fmt.Errorf("could not convert %s to Directive", val.Type())
	}
}

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
				var (
					additionalPackagesIt starlark.Iterable
				)

				parser, ok := args[0].(starlark.Callable)
				if !ok {
					return starlark.None, fmt.Errorf("could not convert %s to Callable", args[0].Type())
				}

				if err := starlark.UnpackArgs(fn.Name(), starlark.Tuple{}, kwargs,
					"additional_packages?", &additionalPackagesIt,
				); err != nil {
					return starlark.None, err
				}

				var defs []common.BuildDefinition

				for _, arg := range args[1:] {
					def, ok := arg.(common.BuildDefinition)
					if !ok {
						return starlark.None, fmt.Errorf("could not convert %s to BuildDefinition", arg.Type())
					}

					defs = append(defs, def)
				}

				var additionalPackages []*common.Package

				if additionalPackagesIt != nil {
					var val starlark.Value

					dependencyIter := additionalPackagesIt.Iterate()
					defer dependencyIter.Done()

					for dependencyIter.Next(&val) {
						dep, ok := val.(*common.Package)
						if !ok {
							return nil, fmt.Errorf("could not convert %s to Package", val.Type())
						}

						additionalPackages = append(additionalPackages, dep)
					}
				}

				return NewPackageCollection(thread.Name, parser, defs, additionalPackages)
			}),
			"container_builder": starlark.NewBuiltin("define.container_builder", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					name         string
					displayName  string
					planCallback starlark.Callable
					packages     *PackageCollection
					metadata     starlark.Value
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"name", &name,
					"plan_callback", &planCallback,
					"packages", &packages,
					"display_name?", &displayName,
					"metadata?", &metadata,
				); err != nil {
					return starlark.None, err
				}

				return NewContainerBuilder(name, displayName, planCallback, packages, metadata)
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
			"fetch_oci_image": starlark.NewBuiltin("define.fetch_oci_image", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					registry     string
					image        string
					tag          string
					architecture string
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"image", &image,
					"registry?", &registry,
					"tag?", &tag,
					"arch?", &architecture,
				); err != nil {
					return starlark.None, err
				}

				return builder.NewFetchOCIImageDefinition(registry, image, tag, architecture), nil
			}),
			"build_vm": starlark.NewBuiltin("define.build_vm", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var val starlark.Value

				var (
					directiveList starlark.Iterable
					kernel        starlark.Value
					initramfs     starlark.Value
					output        string
					storageSize   int
					interaction   string
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"directives?", &directiveList,
					"kernel?", &kernel,
					"initramfs?", &initramfs,
					"output?", &output,
					"storage_size?", &storageSize,
					"interaction", &interaction,
				); err != nil {
					return starlark.None, err
				}

				var directives []common.Directive

				if directiveList != nil {
					directiveIter := directiveList.Iterate()
					defer directiveIter.Done()

					for directiveIter.Next(&val) {
						dir, err := asDirective(val)
						if err != nil {
							return nil, err
						}

						directives = append(directives, dir)
					}
				}

				var initramfsDef common.BuildDefinition

				if initramfs != nil {
					if f, ok := initramfs.(common.BuildDefinition); ok {
						initramfsDef = f
					} else {
						return starlark.None, fmt.Errorf("could not convert %s to BuildDefinition", initramfs.Type())
					}
				}

				var kernelDef common.BuildDefinition

				if kernel != nil {
					if f, ok := kernel.(common.BuildDefinition); ok {
						kernelDef = f
					} else {
						return starlark.None, fmt.Errorf("could not convert %s to BuildDefinition", initramfs.Type())
					}
				}

				return builder.NewBuildVmDefinition(
					directives,
					kernelDef,
					initramfsDef,
					output,
					storageSize,
					interaction,
				), nil
			}),
			"build_fs": starlark.NewBuiltin("define.build_fs", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var val starlark.Value

				var (
					directiveList starlark.Iterable
					kind          string
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"directives", &directiveList,
					"kind", &kind,
				); err != nil {
					return starlark.None, err
				}

				var directives []common.Directive

				directiveIter := directiveList.Iterate()
				defer directiveIter.Done()

				for directiveIter.Next(&val) {
					dir, err := asDirective(val)
					if err != nil {
						return nil, err
					}

					directives = append(directives, dir)
				}

				return builder.NewBuildFsDefinition(directives, kind), nil
			}),
			"archive": starlark.NewBuiltin("define.archive", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					dir starlark.Value
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"dir", &dir,
				); err != nil {
					return starlark.None, err
				}

				if v, ok := dir.(filesystem.Directory); ok {
					return builder.NewCreateArchiveDefinition(v), nil
				} else {
					return starlark.None, fmt.Errorf("could not convert %s to Directory", dir.Type())
				}
			}),
			"plan": starlark.NewBuiltin("define.plan", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					val starlark.Value
					err error
				)

				var (
					builderName  string
					searchListIt starlark.Iterable
					tagListIt    starlark.Iterable
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"builder", &builderName,
					"packages", &searchListIt,
					"tags", &tagListIt,
				); err != nil {
					return starlark.None, err
				}

				var search []common.PackageQuery

				{
					dependencyIter := searchListIt.Iterate()
					defer dependencyIter.Done()

					for dependencyIter.Next(&val) {
						dep, ok := val.(common.PackageQuery)
						if !ok {
							return nil, fmt.Errorf("could not convert %s to PackageQuery", val.Type())
						}

						search = append(search, dep)
					}
				}

				var tagList []string

				if tagListIt != nil {
					tagList, err = common.ToStringList(tagListIt)
					if err != nil {
						return starlark.None, err
					}
				}

				return builder.NewPlanDefinition(builderName, search, tagList), nil
			}),
		},
	}

	ret["directive"] = &starlarkstruct.Module{
		Name: "directive",
		Members: starlark.StringDict{
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
			"archive": starlark.NewBuiltin("directive.archive", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					ark    starlark.Value
					target string
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"ark", &ark,
					"target?", &target,
				); err != nil {
					return starlark.None, err
				}

				if def, ok := ark.(common.BuildDefinition); ok {
					return &common.StarDirective{Directive: common.DirectiveArchive{
						Definition: def,
						Target:     target,
					}}, nil
				} else {
					return starlark.None, fmt.Errorf("could not convert %s to BuildDefinition", ark.Type())
				}
			}),
			"add_file": starlark.NewBuiltin("directive.add_file", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					filename   string
					file       *filesystem.StarFile
					executable bool
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"filename", &filename,
					"file", &file,
					"executable?", &executable,
				); err != nil {
					return starlark.None, err
				}

				fh, err := file.Open()
				if err != nil {
					return starlark.None, err
				}
				defer fh.Close()

				contents, err := io.ReadAll(fh)
				if err != nil {
					return starlark.None, err
				}

				return &common.StarDirective{Directive: common.DirectiveAddFile{
					Filename:   filename,
					Contents:   contents,
					Executable: executable,
				}}, nil
			}),
		},
	}

	ret["installer"] = starlark.NewBuiltin("installer", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			val starlark.Value
			err error
		)

		var (
			tagListIt      starlark.Iterable
			directiveList  starlark.Iterable
			dependencyList starlark.Iterable
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"tags?", &tagListIt,
			"directives?", &directiveList,
			"dependencies?", &dependencyList,
		); err != nil {
			return starlark.None, err
		}

		var directives []common.Directive
		var dependencies []common.PackageQuery

		if directiveList != nil {
			directiveIter := directiveList.Iterate()
			defer directiveIter.Done()

			for directiveIter.Next(&val) {
				dir, err := asDirective(val)
				if err != nil {
					return nil, err
				}

				directives = append(directives, dir)
			}
		}

		if dependencyList != nil {
			dependencyIter := dependencyList.Iterate()
			defer dependencyIter.Done()

			for dependencyIter.Next(&val) {
				dep, ok := val.(common.PackageQuery)
				if !ok {
					return nil, fmt.Errorf("could not convert %s to PackageQuery", val.Type())
				}

				dependencies = append(dependencies, dep)
			}
		}

		var tagList []string

		if tagListIt != nil {
			tagList, err = common.ToStringList(tagListIt)
			if err != nil {
				return nil, err
			}
		}

		return common.NewInstaller(tagList, directives, dependencies), nil
	})

	ret["package"] = starlark.NewBuiltin("package", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var val starlark.Value

		var (
			name           common.PackageName
			installersList starlark.Iterable
			aliasList      starlark.Iterable
			raw            starlark.Value
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"name", &name,
			"installers", &installersList,
			"aliases?", &aliasList,
			"raw?", &raw,
		); err != nil {
			return starlark.None, err
		}

		var installers []*common.Installer
		var aliases []common.PackageName

		{
			iter := installersList.Iterate()
			defer iter.Done()

			for iter.Next(&val) {
				installer, ok := val.(*common.Installer)
				if !ok {
					return nil, fmt.Errorf("could not convert %s to Installer", val.Type())
				}

				installers = append(installers, installer)
			}
		}

		if aliasList != nil {
			iter := aliasList.Iterate()
			defer iter.Done()

			for iter.Next(&val) {
				alias, ok := val.(common.PackageName)
				if !ok {
					return nil, fmt.Errorf("could not convert %s to PackageName", val.Type())
				}

				aliases = append(aliases, alias)
			}
		}

		rawString := ""

		if raw != nil {
			formattedRaw, err := common.StarlarkJsonEncode(nil, starlark.Tuple{raw}, []starlark.Tuple{})
			if err != nil {
				return nil, err
			}

			rawString = string(formattedRaw.(starlark.String))
		}

		return common.NewPackage(name, installers, aliases, rawString), nil
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

	ret["query"] = starlark.NewBuiltin("query", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			name string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"name", &name,
		); err != nil {
			return starlark.None, err
		}

		q, err := common.ParsePackageQuery(name)
		if err != nil {
			return starlark.None, err
		}

		return q, nil
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
			contents   starlark.Value
			name       string
			executable bool
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"contents", &contents,
			"name?", &name,
			"executable?", &executable,
		); err != nil {
			return starlark.None, err
		}

		f := filesystem.NewMemoryFile(filesystem.TypeRegular)

		if str, ok := contents.(starlark.String); ok {
			f.Overwrite([]byte(str))
		} else if file, ok := contents.(filesystem.File); ok {
			fh, err := file.Open()
			if err != nil {
				return starlark.None, err
			}
			defer fh.Close()

			contents, err := io.ReadAll(fh)
			if err != nil {
				return starlark.None, err
			}

			f.Overwrite(contents)
		} else {
			return starlark.None, fmt.Errorf("could not convert %s to string", contents.Type())
		}

		if executable {
			if err := f.Chmod(fs.FileMode(0755)); err != nil {
				return starlark.None, err
			}
		}

		return filesystem.NewStarFile(f, name), nil
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

	ret["time"] = starlark.NewBuiltin("time", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		return starlark.Float(time.Since(START_TIME).Seconds()), nil
	})

	return ret
}
