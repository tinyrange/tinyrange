package database

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	initExec "github.com/tinyrange/tinyrange/pkg/init"
	"github.com/tinyrange/tinyrange/stdlib"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

type outputFile struct {
	f io.Writer
}

// Attr implements starlark.HasAttrs.
func (o *outputFile) Attr(name string) (starlark.Value, error) {
	if name == "write" {
		return starlark.NewBuiltin("OutputFile.write", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				contents string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"contents", &contents,
			); err != nil {
				return starlark.None, err
			}

			if _, err := fmt.Fprintf(o.f, "%s", contents); err != nil {
				return starlark.None, err
			}

			return starlark.None, nil
		}), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (o *outputFile) AttrNames() []string {
	return []string{"write"}
}

func (*outputFile) String() string        { return "OutputFile" }
func (*outputFile) Type() string          { return "OutputFile" }
func (*outputFile) Hash() (uint32, error) { return 0, fmt.Errorf("OutputFile is not hashable") }
func (*outputFile) Truth() starlark.Bool  { return starlark.True }
func (*outputFile) Freeze()               {}

var (
	_ starlark.Value    = &outputFile{}
	_ starlark.HasAttrs = &outputFile{}
)

type scriptArguments struct {
	args           map[string]starlark.Value
	outputFilename string
	additionalArgs []string
}

// Attr implements starlark.HasAttrs.
func (s *scriptArguments) Attr(name string) (starlark.Value, error) {
	if name == "output" {
		return starlark.NewBuiltin("Arguments.output", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			if s.outputFilename == "" {
				return starlark.None, fmt.Errorf("no output file specified. please specify one using the -o flag")
			}

			f, err := os.Create(s.outputFilename)
			if err != nil {
				return starlark.None, nil
			}

			return &outputFile{f: f}, nil
		}), nil
	} else if name == "args" {
		var ret []starlark.Value

		for _, arg := range s.additionalArgs {
			ret = append(ret, starlark.String(arg))
		}

		return starlark.NewList(ret), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (s *scriptArguments) AttrNames() []string {
	return []string{"output", "args"}
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
	_ starlark.Value    = &scriptArguments{}
	_ starlark.Mapping  = &scriptArguments{}
	_ starlark.HasAttrs = &scriptArguments{}
)

type PackageDatabase struct {
	ContainerBuilders map[string]*ContainerBuilder

	RebuildUserDefinitions bool

	mirrors map[string][]string

	memoryCache map[string][]byte

	buildStatusMtx sync.Mutex
	buildStatuses  map[common.BuildDefinition]*common.BuildStatus

	defs map[string]starlark.Value

	builders map[string]starlark.Callable

	defDb *hash.DefinitionDatabase

	buildDir string
}

// HashDefinition implements common.PackageDatabase.
func (db *PackageDatabase) HashDefinition(def common.BuildDefinition) (string, error) {
	return db.defDb.HashDefinition(def)
}

// ShouldRebuildUserDefinitions implements common.PackageDatabase.
func (db *PackageDatabase) ShouldRebuildUserDefinitions() bool {
	return db.RebuildUserDefinitions
}

func (db *PackageDatabase) getFileContents(name string) (string, error) {
	if strings.HasPrefix(name, "//") {
		f, err := stdlib.STDLIB.Open(strings.TrimPrefix(name, "//"))
		if err != nil {
			return "", err
		}
		defer f.Close()

		contents, err := io.ReadAll(f)
		if err != nil {
			return "", err
		}

		return string(contents), nil
	}

	contents, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}

	return string(contents), nil
}

func (db *PackageDatabase) onLoadFile(filename string, defs starlark.StringDict) error {
	for k, v := range defs {
		if callable, ok := v.(starlark.Callable); ok {
			db.builders[fmt.Sprintf("%s:%s", filename, k)] = callable
		}
	}

	return nil
}

func (db *PackageDatabase) NewThread(filename string) *starlark.Thread {
	return &starlark.Thread{
		Name: filename,
		Load: func(thread *starlark.Thread, module string) (starlark.StringDict, error) {
			globals := db.getGlobals(module)

			contents, err := db.getFileContents(module)
			if err != nil {
				return nil, err
			}

			newThread := db.NewThread(module)

			ret, err := starlark.ExecFileOptions(db.getFileOptions(), newThread, module, contents, globals)
			if err != nil {
				if sErr, ok := err.(*starlark.EvalError); ok {
					slog.Error("got starlark error", "error", sErr, "backtrace", sErr.Backtrace())
				}
				return nil, err
			}

			if err := db.onLoadFile(module, ret); err != nil {
				return nil, err
			}

			return ret, nil
		},
	}
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
	thread := db.NewThread(filename)

	globals := db.getGlobals("__main__")

	// Execute the file.
	contents, err := db.getFileContents(filename)
	if err != nil {
		return err
	}

	defs, err := starlark.ExecFileOptions(db.getFileOptions(), thread, filename, contents, globals)
	if err != nil {
		return err
	}

	if err := db.onLoadFile(filename, defs); err != nil {
		return err
	}

	for k, v := range defs {
		db.defs[k] = v
	}

	return nil
}

func (db *PackageDatabase) RunScript(filename string, files map[string]filesystem.File, additionalArgs []string, outputFilename string) error {
	thread := db.NewThread(filename)

	globals := db.getGlobals("__main__")

	// Execute the script.
	contents, err := db.getFileContents(filename)
	if err != nil {
		return err
	}

	decls, err := starlark.ExecFileOptions(db.getFileOptions(), thread, filename, contents, globals)
	if err != nil {
		return err
	}

	if err := db.onLoadFile(filename, decls); err != nil {
		return err
	}

	args := &scriptArguments{
		args:           make(map[string]starlark.Value),
		outputFilename: outputFilename,
		additionalArgs: additionalArgs,
	}

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
		if sErr, ok := err.(*starlark.EvalError); ok {
			slog.Error("got starlark error", "error", sErr, "backtrace", sErr.Backtrace())
		}
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

func (db *PackageDatabase) updateBuildStatus(def common.BuildDefinition, status *common.BuildStatus) {
	db.buildStatusMtx.Lock()
	defer db.buildStatusMtx.Unlock()

	db.buildStatuses[def] = status
}

func (db *PackageDatabase) FilenameFromHash(hash string, suffix string) (string, error) {
	return filepath.Join(db.buildDir, hash+suffix), nil
}

func (db *PackageDatabase) Build(ctx common.BuildContext, def common.BuildDefinition, opts common.BuildOptions) (filesystem.File, error) {
	tag := def.Tag()

	hash, err := db.HashDefinition(def)
	if err != nil {
		return nil, err
	}

	status := &common.BuildStatus{Tag: tag}

	if ctx.IsInMemory() {
		// If this is in memory then check the in-memory cache.
		if contents, ok := db.memoryCache[hash]; ok {
			// TODO(joshua): Support needs build for memory cached items.
			f := filesystem.NewMemoryFile(filesystem.TypeRegular)

			if err := f.Overwrite(contents); err != nil {
				return nil, err
			}

			status.Status = common.BuildStatusCached

			// Write the build status.
			db.updateBuildStatus(def, status)

			return f, nil
		}

		// Get a child context for the build.
		child := ctx.ChildContext(def, status, "")

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
		f := filesystem.NewMemoryFile(filesystem.TypeRegular)

		if err := f.Overwrite(buf.Bytes()); err != nil {
			return nil, err
		}

		status.Status = common.BuildStatusBuilt

		// Write the build status.
		db.updateBuildStatus(def, status)

		return f, nil
	} else {
		filename, err := db.FilenameFromHash(hash, ".bin")
		if err != nil {
			return nil, err
		}

		tmpFilename := filename + ".tmp"

		// Get a child context for the build.
		child := ctx.ChildContext(def, status, tmpFilename)

		if !opts.AlwaysRebuild {
			// Check if the file already exists. If it does then return it.
			if info, err := os.Stat(filename); err == nil {
				// If the file has already been created then check if a rebuild is needed.
				needsRebuild, err := def.NeedsBuild(child, info.ModTime())
				if err != nil {
					return nil, err
				}

				// If no rebuild is necessary then skip it.
				if !needsRebuild {
					status.Status = common.BuildStatusCached

					// Write the build status.
					db.updateBuildStatus(def, status)

					return filesystem.NewLocalFile(filename, def), nil
				}

				child.SetHasCached()

				slog.Debug("rebuild requested", "Tag", def.Tag())
			} else {
				slog.Debug("building", "Tag", def.Tag())
			}
		} else {
			slog.Debug("building", "Tag", def.Tag())
		}

		defValue, err := db.defDb.MarshalDefinition(def)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal definition: %s", err)
		}

		defFilename, err := db.FilenameFromHash(hash, ".def")
		if err != nil {
			return nil, err
		}

		if err := os.WriteFile(defFilename, defValue, os.ModePerm); err != nil {
			return nil, fmt.Errorf("failed to write definition: %s", err)
		}

		// If not then trigger the build.
		result, err := def.Build(child)
		if err != nil {
			return nil, err
		}

		// If the result is nil then the builder is telling us to use the cached version.
		if result == nil {
			status.Status = common.BuildStatusCached

			// Write the build status.
			db.updateBuildStatus(def, status)

			return filesystem.NewLocalFile(filename, def), nil
		}

		// If the build has already been written then don't write it again.
		if !child.HasCreatedOutput() {
			// Once the build is complete then write it to disk.
			outFile, err := os.Create(tmpFilename)
			if err != nil {
				return nil, err
			}

			// Write the build result to disk. If any of these steps fail then remove the temporary file.
			if _, err := result.WriteTo(outFile); err != nil {
				outFile.Close()
				os.Remove(tmpFilename)
				return nil, err
			}

			if err := outFile.Close(); err != nil {
				os.Remove(tmpFilename)
				return nil, err
			}
		} else {
			// Let the result close the file on it's own.
			if _, err := result.WriteTo(nil); err != nil {
				os.Remove(tmpFilename)
				return nil, err
			}
		}

		// Finally rename the temporary file to the final filename.
		if err := os.Rename(tmpFilename, filename); err != nil {
			os.Remove(tmpFilename)
			return nil, err
		}

		status.Status = common.BuildStatusBuilt

		// Write the build status.
		db.updateBuildStatus(def, status)

		// Return the file.
		return filesystem.NewLocalFile(filename, def), nil
	}
}

func (db *PackageDatabase) GetBuildStatus(def common.BuildDefinition) (*common.BuildStatus, error) {
	status, ok := db.buildStatuses[def]
	if !ok {
		return nil, fmt.Errorf("build status not found")
	}
	return status, nil
}

func (db *PackageDatabase) NewName(name string, version string, tags []string) (common.PackageName, error) {
	return common.PackageName{
		Name:    name,
		Version: version,
		Tags:    tags,
	}, nil
}

func (db *PackageDatabase) GetBuilder(filename string, builder string) (starlark.Callable, error) {
	if filename == "" {
		return nil, fmt.Errorf("no filename passed to GetBuilder")
	}

	callable, ok := db.builders[fmt.Sprintf("%s:%s", filename, builder)]
	if !ok {
		return nil, fmt.Errorf("callable %s:%s not found", filename, builder)
	}

	return callable, nil
}

func (db *PackageDatabase) GetContainerBuilder(name string) (common.ContainerBuilder, error) {
	builder, ok := db.ContainerBuilders[name]
	if !ok {
		return nil, fmt.Errorf("builder %s not found", name)
	}

	if !builder.Loaded() {
		start := time.Now()
		if err := builder.Load(db); err != nil {
			return nil, err
		}
		slog.Debug("loaded", "builder", builder.DisplayName, "took", time.Since(start))
	}

	return builder, nil
}

func (db *PackageDatabase) GetDefinitionByDeclaredName(name string) (common.BuildDefinition, error) {
	def, ok := db.defs[name]
	if !ok {
		return nil, fmt.Errorf("name %s not found", name)
	}

	buildDef, ok := def.(common.BuildDefinition)
	if !ok {
		return nil, fmt.Errorf("value %s is not a BuildDefinition", def.Type())
	}

	return buildDef, nil
}

func (db *PackageDatabase) missDefinitionCache(hash string) (io.ReadCloser, error) {
	filename, err := db.FilenameFromHash(hash, ".def")
	if err != nil {
		return nil, err
	}

	return os.Open(filename)
}

func (db *PackageDatabase) GetDefinitionByHash(hash string) (common.BuildDefinition, error) {
	def, ok := db.defDb.GetDefinitionByHash(hash)
	if ok {
		if buildDef, ok := def.(common.BuildDefinition); ok {
			return buildDef, nil
		} else {
			return nil, fmt.Errorf("could not convert %T to BuildDefinition", def)
		}
	}

	filename, err := db.FilenameFromHash(hash, ".def")
	if err != nil {
		return nil, err
	}

	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	def, err = db.defDb.UnmarshalDefinition(f)
	if err != nil {
		return nil, err
	}

	if buildDef, ok := def.(common.BuildDefinition); ok {
		return buildDef, nil
	} else {
		return nil, fmt.Errorf("could not convert %T to BuildDefinition", def)
	}
}

func (db *PackageDatabase) GetAllHashes() ([]string, error) {
	var ret []string

	ents, err := os.ReadDir(db.buildDir)
	if err != nil {
		return nil, err
	}

	for _, ent := range ents {
		ext := filepath.Ext(ent.Name())
		if ext == ".def" {
			ret = append(ret, strings.TrimSuffix(ent.Name(), ext))
		}
	}

	return ret, nil
}

func (db *PackageDatabase) LoadBuiltinBuilders() error {
	for _, builder := range []string{
		"//fetchers/alpine.star",
		"//fetchers/rpm.star",
		"//fetchers/debian.star",
		"//fetchers/arch.star",
	} {
		if err := db.LoadFile(builder); err != nil {
			return err
		}
	}

	return nil
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
				def           common.BuildDefinition
				inMemory      bool
				alwaysRebuild bool
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"def", &def,
				"memory?", &inMemory,
				"always_rebuild?", &alwaysRebuild,
			); err != nil {
				return starlark.None, err
			}

			ctx := db.NewBuildContext(def)

			if inMemory {
				ctx.SetInMemory()
			}

			result, err := db.Build(ctx, def, common.BuildOptions{
				AlwaysRebuild: alwaysRebuild,
			})
			if err != nil {
				return starlark.None, err
			}

			return def.ToStarlark(ctx, result)
		}), nil
	} else if name == "builder" {
		return starlark.NewBuiltin("Database.builder", func(
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

			builder, err := db.GetContainerBuilder(name)
			if err != nil {
				return starlark.None, err
			}

			return builder, nil
		}), nil
	} else if name == "get_builtin_executable" {
		return starlark.NewBuiltin("Database.get_builtin_executable", func(
			thread *starlark.Thread,
			fn *starlark.Builtin,
			args starlark.Tuple,
			kwargs []starlark.Tuple,
		) (starlark.Value, error) {
			var (
				name string
				arch string
			)

			if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
				"name", &name,
				"arch", &arch,
			); err != nil {
				return starlark.None, err
			}

			if name == "init" {
				if common.CPUArchitecture(arch).IsNative() {
					f := filesystem.NewMemoryFile(filesystem.TypeRegular)
					f.Overwrite(initExec.INIT_EXECUTABLE)
					return filesystem.NewStarFile(f, "init"), nil
				} else {
					return starlark.None, fmt.Errorf("invalid architecture for init: %s", arch)
				}
			} else if name == "tinyrange2" {
				// Assume that the user wants a Linux executable.
				if common.CPUArchitecture(arch).IsNative() && runtime.GOOS == "linux" {
					local, err := os.Executable()
					if err != nil {
						return nil, err
					}

					return filesystem.NewStarFile(filesystem.NewLocalFile(local, nil), "tinyrange2"), nil
				} else {
					return starlark.None, fmt.Errorf("invalid architecture for tinyrange: %s", arch)
				}
			} else if name == "tinyrange_qemu.star" {
				local, err := common.GetAdjacentExecutable("tinyrange_qemu.star")
				if err != nil {
					return nil, err
				}

				return filesystem.NewStarFile(filesystem.NewLocalFile(local, nil), "tinyrange_qemu.star"), nil
			} else {
				return starlark.None, fmt.Errorf("unknown builtin executable: %s", name)
			}
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

func New(buildDir string) *PackageDatabase {
	db := &PackageDatabase{
		ContainerBuilders: make(map[string]*ContainerBuilder),
		mirrors:           make(map[string][]string),
		memoryCache:       make(map[string][]byte),
		buildStatuses:     make(map[common.BuildDefinition]*common.BuildStatus),
		buildDir:          buildDir,
		defs:              map[string]starlark.Value{},

		builders: make(map[string]starlark.Callable),
	}

	db.defDb = hash.NewDefinitionDatabase(db.missDefinitionCache)

	return db
}
