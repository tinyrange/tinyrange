package database

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tinyrange/pkg2/v2/builder"
	"github.com/tinyrange/pkg2/v2/common"
	"github.com/tinyrange/pkg2/v2/filesystem"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

type PackageDatabase struct {
	ContainerBuilders map[string]*ContainerBuilder

	RebuildUserDefinitions bool

	mirrors map[string][]string
}

// ShouldRebuildUserDefinitions implements common.PackageDatabase.
func (db *PackageDatabase) ShouldRebuildUserDefinitions() bool {
	return db.RebuildUserDefinitions
}

func (db *PackageDatabase) newThread(name string) *starlark.Thread {
	return &starlark.Thread{Name: name}
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
					def  common.BuildDefinition
					kind string
				)

				if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
					"def", &def,
					"kind", &kind,
				); err != nil {
					return starlark.None, err
				}

				archiveDef := builder.NewReadArchiveBuildDefinition(def, kind)

				return archiveDef, nil
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

func (db *PackageDatabase) getFileOptions() *syntax.FileOptions {
	return &syntax.FileOptions{
		Set:             true,
		While:           true,
		TopLevelControl: true,
	}
}

func (db *PackageDatabase) HttpClient() (*http.Client, error) {
	return &http.Client{}, nil
}

func (db *PackageDatabase) UrlsFor(urlStr string) ([]string, error) {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	if parsed.Scheme != "mirror" {
		return []string{urlStr}, nil
	}

	mirror := parsed.Hostname()
	suffix := strings.TrimPrefix(urlStr, fmt.Sprintf("mirror://%s", mirror))

	urls, ok := db.mirrors[mirror]
	if !ok {
		return nil, fmt.Errorf("mirror %s not defined", mirror)
	}

	var ret []string

	for _, url := range urls {
		ret = append(ret, url+suffix)
	}

	return ret, nil
}

func (db *PackageDatabase) AddMirror(name string, options []string) error {
	db.mirrors[name] = options
	return nil
}

func (db *PackageDatabase) AddContainerBuilder(builder *ContainerBuilder) error {
	db.ContainerBuilders[builder.Name] = builder

	return nil
}

func (db *PackageDatabase) LoadFile(filename string) error {
	thread := db.newThread(filename)

	globals := db.getGlobals("__main__")

	// Execute the file.
	if _, err := starlark.ExecFileOptions(db.getFileOptions(), thread, filename, nil, globals); err != nil {
		return err
	}

	return nil
}

func (db *PackageDatabase) RunScript(filename string) error {
	thread := db.newThread(filename)

	globals := db.getGlobals("__main__")

	// Execute the script.
	decls, err := starlark.ExecFileOptions(db.getFileOptions(), thread, filename, nil, globals)
	if err != nil {
		return err
	}

	// Call the main function.
	mainFunc, ok := decls["main"]
	if !ok {
		return fmt.Errorf("main function not found")
	}
	_, err = starlark.Call(thread, mainFunc, starlark.Tuple{}, []starlark.Tuple{})
	if err != nil {
		return err
	}

	return nil
}

func (db *PackageDatabase) LoadAll(parallel bool) error {
	if parallel {
		var wg sync.WaitGroup
		done := make(chan bool)
		errors := make(chan error)

		for _, builder := range db.ContainerBuilders {
			wg.Add(1)

			go func(builder *ContainerBuilder) {
				defer wg.Done()

				if err := builder.Load(db); err != nil {
					errors <- err
				}
			}(builder)
		}

		go func() {
			wg.Wait()

			done <- true
		}()

		select {
		case err := <-errors:
			return err
		case <-done:
			return nil
		}
	} else {
		for _, builder := range db.ContainerBuilders {
			if err := builder.Load(db); err != nil {
				return err
			}
		}

		return nil
	}
}

func (db *PackageDatabase) NewBuildContext(source common.BuildSource) common.BuildContext {
	return builder.NewBuildContext(source, db)
}

func (db *PackageDatabase) Build(ctx common.BuildContext, def common.BuildDefinition) (filesystem.File, error) {
	hash := common.GetSha256Hash([]byte(def.Tag()))

	filename := filepath.Join("local", "build", hash+".bin")

	// Get a child context for the build.
	child := ctx.ChildContext(def, filename+".tmp")

	// Check if the file already exists. If it does then return it.
	if info, err := os.Stat(filename); err == nil {
		// If the file has already been created then check if a rebuild is needed.
		needsRebuild, err := def.NeedsBuild(child, info.ModTime())
		if err != nil {
			return nil, err
		}

		// If no rebuild is necessary then skip it.
		if !needsRebuild {
			return &filesystem.LocalFile{Filename: filename}, nil
		}

		slog.Info("rebuild requested", "Tag", def.Tag())
	} else {
		slog.Info("building", "Tag", def.Tag())
	}

	// If not then trigger the build.
	result, err := def.Build(child)
	if err != nil {
		return nil, err
	}

	// If the build has already been written then don't write it again.
	if !child.HasCreatedOutput() {
		// Once the build is complete then write it to disk.
		outFile, err := os.Create(filename + ".tmp")
		if err != nil {
			return nil, err
		}

		// Write the build result to disk. If any of these steps fail then remove the temporary file.
		if _, err := result.WriteTo(outFile); err != nil {
			outFile.Close()
			os.Remove(filename + ".tmp")
			return nil, err
		}

		if err := outFile.Close(); err != nil {
			os.Remove(filename + ".tmp")
			return nil, err
		}
	} else {
		// Let the result close the file on it's own.
		if _, err := result.WriteTo(nil); err != nil {
			os.Remove(filename + ".tmp")
			return nil, err
		}
	}

	// Finally rename the temporary file to the final filename.
	if err := os.Rename(filename+".tmp", filename); err != nil {
		os.Remove(filename + ".tmp")
		return nil, err
	}

	// Return the file.
	return &filesystem.LocalFile{Filename: filename}, nil
}

func (db *PackageDatabase) NewName(name string, version string, tags []string) (common.PackageName, error) {
	return common.PackageName{
		Name:    name,
		Version: version,
		Tags:    tags,
	}, nil
}

func (db *PackageDatabase) GetBuilder(name string) (*ContainerBuilder, error) {
	builder, ok := db.ContainerBuilders[name]
	if !ok {
		return nil, fmt.Errorf("builder %s not found", name)
	}

	if !builder.Loaded() {
		if err := builder.Load(db); err != nil {
			return nil, err
		}
	}

	return builder, nil
}

// Attr implements starlark.HasAttrs.
func (db *PackageDatabase) Attr(name string) (starlark.Value, error) {
	if name == "add_mirror" {
		return starlark.NewBuiltin("Database.add_mirror", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name       string
				mirrorsVal starlark.Iterable
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"name", &name,
				"mirrors", &mirrorsVal,
			); err != nil {
				return starlark.None, err
			}

			mirrors, err := common.ToStringList(mirrorsVal)
			if err != nil {
				return starlark.None, err
			}

			return starlark.None, db.AddMirror(name, mirrors)
		}), nil
	} else if name == "add_container_builder" {
		return starlark.NewBuiltin("Database.add_container_builder", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				builder *ContainerBuilder
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"builder", &builder,
			); err != nil {
				return starlark.None, err
			}

			return starlark.None, db.AddContainerBuilder(builder)
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (db *PackageDatabase) AttrNames() []string {
	return []string{"add_mirror"}
}

func (*PackageDatabase) String() string        { return "Database" }
func (*PackageDatabase) Type() string          { return "Database" }
func (*PackageDatabase) Hash() (uint32, error) { return 0, fmt.Errorf("Database is not hashable") }
func (*PackageDatabase) Truth() starlark.Bool  { return starlark.True }
func (*PackageDatabase) Freeze()               {}

var (
	_ starlark.Value         = &PackageDatabase{}
	_ starlark.HasAttrs      = &PackageDatabase{}
	_ common.PackageDatabase = &PackageDatabase{}
)

func New() *PackageDatabase {
	return &PackageDatabase{
		ContainerBuilders: make(map[string]*ContainerBuilder),
		mirrors:           make(map[string][]string),
	}
}
