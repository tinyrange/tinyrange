package database

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tinyrange/pkg2/v2/builder"
	"github.com/tinyrange/pkg2/v2/common"
	"github.com/tinyrange/pkg2/v2/filesystem"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

type scriptArguments struct {
	args map[string]starlark.Value
}

// Get implements starlark.Mapping.
func (s *scriptArguments) Get(k starlark.Value) (v starlark.Value, found bool, err error) {
	key, ok := starlark.AsString(k)
	if !ok {
		return nil, false, fmt.Errorf("expected string got %s", k.Type())
	}

	val, ok := s.args[key]
	if !ok {
		return nil, false, nil
	}

	return val, true, nil
}

func (*scriptArguments) String() string        { return "Arguments" }
func (*scriptArguments) Type() string          { return "Arguments" }
func (*scriptArguments) Hash() (uint32, error) { return 0, fmt.Errorf("Arguments is not hashable") }
func (*scriptArguments) Truth() starlark.Bool  { return starlark.True }
func (*scriptArguments) Freeze()               {}

var (
	_ starlark.Value   = &scriptArguments{}
	_ starlark.Mapping = &scriptArguments{}
)

type PackageDatabase struct {
	ContainerBuilders map[string]*ContainerBuilder

	RebuildUserDefinitions bool

	mirrors map[string][]string

	memoryCache map[string][]byte
}

// ShouldRebuildUserDefinitions implements common.PackageDatabase.
func (db *PackageDatabase) ShouldRebuildUserDefinitions() bool {
	return db.RebuildUserDefinitions
}

func (db *PackageDatabase) newThread(name string) *starlark.Thread {
	return &starlark.Thread{Name: name}
}

func (db *PackageDatabase) getFileOptions() *syntax.FileOptions {
	return &syntax.FileOptions{
		Set:             true,
		While:           true,
		TopLevelControl: true,
		Recursion:       true,
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

func (db *PackageDatabase) RunScript(filename string, files map[string]filesystem.File) error {
	thread := db.newThread(filename)

	globals := db.getGlobals("__main__")

	// Execute the script.
	decls, err := starlark.ExecFileOptions(db.getFileOptions(), thread, filename, nil, globals)
	if err != nil {
		return err
	}

	args := &scriptArguments{args: make(map[string]starlark.Value)}

	for k, v := range files {
		args.args[k] = filesystem.NewStarFile(v, k)
	}

	// Call the main function.
	mainFunc, ok := decls["main"]
	if !ok {
		return fmt.Errorf("main function not found")
	}
	_, err = starlark.Call(thread, mainFunc, starlark.Tuple{args}, []starlark.Tuple{})
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

	if ctx.IsInMemory() {
		// If this is in memory then check the in-memory cache.
		if contents, ok := db.memoryCache[hash]; ok {
			// TODO(joshua): Support needs build for memory cached items.
			f := filesystem.NewMemoryFile()

			if err := f.Overwrite(contents); err != nil {
				return nil, err
			}

			return f, nil
		}

		// Get a child context for the build.
		child := ctx.ChildContext(def, "")

		// Trigger the build
		result, err := def.Build(child)
		if err != nil {
			return nil, err
		}

		// Write the build result into a bytes buffer.
		buf := new(bytes.Buffer)
		if _, err := result.WriteTo(buf); err != nil {
			return nil, err
		}

		// Add the bytes buffer to the in-memory cache.
		db.memoryCache[hash] = buf.Bytes()

		// Create and return a in-memory file.
		f := filesystem.NewMemoryFile()

		if err := f.Overwrite(buf.Bytes()); err != nil {
			return nil, err
		}

		return f, nil
	} else {
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
	} else if name == "build" {
		return starlark.NewBuiltin("Database.build", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				def      common.BuildDefinition
				inMemory bool
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"def", &def,
				"memory?", &inMemory,
			); err != nil {
				return starlark.None, err
			}

			ctx := db.NewBuildContext(def)

			if inMemory {
				ctx.SetInMemory()
			}

			result, err := db.Build(ctx, def)
			if err != nil {
				return starlark.None, err
			}

			return builder.BuildResultToStarlark(ctx, def, result)
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
		memoryCache:       make(map[string][]byte),
	}
}
