package database

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"golang.org/x/exp/rand"
)

var START_TIME = time.Now()

func asDirective(val starlark.Value) (common.Directive, error) {
	if starDir, ok := val.(*common.StarDirective); ok {
		return starDir.Directive, nil
	} else if directive, ok := val.(common.Directive); ok {
		return directive, nil
	} else if file, ok := val.(filesystem.File); ok {
		def, err := builder.NewDefinitionFromFile(file)
		if err != nil {
			return nil, err
		}

		if dir, ok := def.(common.Directive); ok {
			return dir, nil
		} else {
			return nil, fmt.Errorf("could not convert %T to Directive", def)
		}
	} else {
		return nil, fmt.Errorf("could not convert %s to Directive", val.Type())
	}
}

func (db *PackageDatabase) getGlobals(name string) starlark.StringDict {
	ret := starlark.StringDict{}

	ret["__name__"] = starlark.String(name)

	ret["json"] = starlarkjson.Module

	ret["db"] = db

	ret["load_fetcher"] = starlark.NewBuiltin("load_fetcher", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			filename string
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"filename", &filename,
		); err != nil {
			return starlark.None, err
		}

		if err := db.LoadFile(filename); err != nil {
			return starlark.None, err
		}

		return starlark.None, nil
	})

	ret["define"] = &starlarkstruct.Module{
		Name: "define",
		Members: starlark.StringDict{
			"build": starlark.NewBuiltin("define.build", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				builderVal := args[0]
				buildArgsVal := args[1:]

				builderFunc, ok := builderVal.(starlark.Callable)
				if !ok {
					return starlark.None, fmt.Errorf("could not convert %s to Callable", builderVal.Type())
				}

				var buildArgs []hash.SerializableValue

				for _, arg := range buildArgsVal {
					val, err := builder.StarlarkValueToSerializable(arg)
					if err != nil {
						return starlark.None, err
					}

					buildArgs = append(buildArgs, val)
				}

				filename := thread.CallFrame(1).Pos.Filename()

				return builder.NewStarBuildDefinition(filename, builderFunc.Name(), buildArgs)
			}),
			"package_collection": starlark.NewBuiltin("define.package_collection", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				parser, ok := args[0].(starlark.Callable)
				if !ok {
					return starlark.None, fmt.Errorf("could not convert %s to Callable", args[0].Type())
				}

				install, ok := args[1].(starlark.Callable)
				if !ok {
					return starlark.None, fmt.Errorf("could not convert %s to Callable", args[1].Type())
				}

				if err := starlark.UnpackArgs(fn.Name(), starlark.Tuple{}, kwargs); err != nil {
					return starlark.None, err
				}

				var defs []common.BuildDefinition

				for _, arg := range args[2:] {
					def, ok := arg.(common.BuildDefinition)
					if !ok {
						return starlark.None, fmt.Errorf("could not convert %s to BuildDefinition", arg.Type())
					}

					defs = append(defs, def)
				}

				filename := thread.CallFrame(1).Pos.Filename()

				return NewPackageCollection(filename, parser.Name(), install.Name(), defs)
			}),
			"container_builder": starlark.NewBuiltin("define.container_builder", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					name                string
					displayName         string
					planCallback        starlark.Callable
					packages            *PackageCollection
					defaultPackagesList starlark.Iterable
					metadata            starlark.Value
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"name", &name,
					"plan_callback", &planCallback,
					"packages", &packages,
					"default_packages?", &defaultPackagesList,
					"display_name?", &displayName,
					"metadata?", &metadata,
				); err != nil {
					return starlark.None, err
				}

				var defaultPackages []common.PackageQuery

				if defaultPackagesList != nil {
					directiveIter := defaultPackagesList.Iterate()
					defer directiveIter.Done()

					var val starlark.Value

					for directiveIter.Next(&val) {
						dir, ok := val.(common.PackageQuery)
						if !ok {
							return starlark.None, fmt.Errorf("expected PackageQuery got %s", val.Type())
						}

						defaultPackages = append(defaultPackages, dir)
					}
				}

				filename := thread.CallFrame(1).Pos.Filename()

				return NewContainerBuilder(name, displayName, filename, planCallback.Name(), defaultPackages, packages, metadata)
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
					fileDef, err := builder.NewDefinitionFromFile(file)
					if err != nil {
						return starlark.None, err
					}

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
					cpuCores      int
					memoryMb      int
					storageSize   int
					interaction   string
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"directives?", &directiveList,
					"kernel?", &kernel,
					"initramfs?", &initramfs,
					"output?", &output,
					"cpu_cores", &cpuCores,
					"memory_mb", &memoryMb,
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
					cpuCores,
					memoryMb,
					storageSize,
					interaction,
					false,
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
			"build_emulator": starlark.NewBuiltin("define.build_emulator", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var val starlark.Value

				var (
					directiveList  starlark.Iterable
					outputFilename string
					createCallback starlark.Callable
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"directives", &directiveList,
					"output_filename", &outputFilename,
					"create", &createCallback,
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

				filename := thread.CallFrame(1).Pos.Filename()

				return builder.NewBuildEmulatorDefinition(
					directives,
					outputFilename,
					filename,
					createCallback.Name(),
				), nil
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

				return &common.StarDirective{Directive: common.DirectiveRunCommand{
					Command: command,
				}}, nil
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
					val        starlark.Value
					executable bool
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"filename", &filename,
					"file", &val,
					"executable?", &executable,
				); err != nil {
					return starlark.None, err
				}

				if file, ok := val.(*filesystem.StarFile); ok {
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
				} else if def, ok := val.(common.BuildDefinition); ok {
					return &common.StarDirective{Directive: common.DirectiveAddFile{
						Filename:   filename,
						Definition: def,
						Executable: executable,
					}}, nil
				} else {
					return starlark.None, fmt.Errorf("could not convert %s to File", val.Type())
				}
			}),
			"export_port": starlark.NewBuiltin("directive.export_port", func(
				thread *starlark.Thread,
				fn *starlark.Builtin,
				args starlark.Tuple,
				kwargs []starlark.Tuple,
			) (starlark.Value, error) {
				var (
					name string
					port int
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"name", &name,
					"port", &port,
				); err != nil {
					return starlark.None, err
				}

				return &common.StarDirective{Directive: common.DirectiveExportPort{
					Name: name,
					Port: port,
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
			name      common.PackageName
			aliasList starlark.Iterable
			raw       starlark.Value
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"name", &name,
			"aliases?", &aliasList,
			"raw?", &raw,
		); err != nil {
			return starlark.None, err
		}

		var aliases []common.PackageName

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

		return common.NewPackage(name, aliases, raw), nil
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

	ret["shuffle"] = starlark.NewBuiltin("shuffle", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			values starlark.Iterable
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"values", &values,
		); err != nil {
			return starlark.None, err
		}

		it := values.Iterate()
		defer it.Done()

		var valueList []starlark.Value
		var val starlark.Value

		for it.Next(&val) {
			valueList = append(valueList, val)
		}

		for i := range valueList {
			j := rand.Intn(i + 1)
			valueList[i], valueList[j] = valueList[j], valueList[i]
		}

		return starlark.NewList(valueList), nil
	})

	ret["sleep"] = starlark.NewBuiltin("sleep", func(
		thread *starlark.Thread,
		fn *starlark.Builtin,
		args starlark.Tuple,
		kwargs []starlark.Tuple,
	) (starlark.Value, error) {
		var (
			dur int64
		)

		if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
			"dur", &dur,
		); err != nil {
			return starlark.None, err
		}

		time.Sleep(time.Duration(dur))

		return starlark.None, nil
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
