package database

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/filesystem/star"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/macro"
	"github.com/tinyrange/tinyrange/stdlib"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

type macroContext struct {
	db        *packageDatabase
	builders  map[string]common.InstallationPlanBuilder
	variables map[string]string
}

// Variable implements macro.MacroContext.
func (m *macroContext) Variable(name string) string {
	return m.variables[name]
}

// AddVariable implements macro.MacroContext.
func (m *macroContext) AddVariable(name string, value string) {
	m.variables[name] = value
}

// AddBuilder implements macro.MacroContext.
func (m *macroContext) AddBuilder(name string, builder common.InstallationPlanBuilder) {
	m.builders[name] = builder
}

// Builder implements macro.MacroContext.
func (m *macroContext) Builder(name string) (common.InstallationPlanBuilder, error) {
	builder, ok := m.builders[name]
	if !ok {
		return nil, fmt.Errorf("builder %s not found", name)
	}

	return builder, nil
}

// Thread implements common.MacroContext.
func (m *macroContext) Thread() *starlark.Thread {
	return m.db.newThread("__macro__")
}

var (
	_ common.MacroContext = &macroContext{}
)

type packageDatabase struct {
	// keys are name-arch
	containerBuilders map[string]common.ContainerBuilder

	mirrors map[string][]string

	loadedFiles map[string]bool
	defs        map[string]starlark.Value

	builders map[string]starlark.Callable

	builder common.Builder
}

func (db *packageDatabase) getFileContents(name string, allowLocal bool) (string, error) {
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

	if !allowLocal {
		return "", fmt.Errorf("local files are not allowed in remote configs")
	}

	contents, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}

	return string(contents), nil
}

func (db *packageDatabase) onLoadFile(filename string, defs starlark.StringDict) error {
	for k, v := range defs {
		if callable, ok := v.(starlark.Callable); ok {
			db.builders[fmt.Sprintf("%s:%s", filename, k)] = callable
		}
	}

	return nil
}

func (db *packageDatabase) newThread(filename string) *starlark.Thread {
	return &starlark.Thread{
		Name: filename,
		Load: func(thread *starlark.Thread, module string) (starlark.StringDict, error) {
			globals := db.getGlobals(module)

			contents, err := db.getFileContents(module, true)
			if err != nil {
				return nil, err
			}

			newThread := db.newThread(module)

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

func (db *packageDatabase) getFileOptions() *syntax.FileOptions {
	return &syntax.FileOptions{
		Set:             true,
		While:           true,
		TopLevelControl: true,
		Recursion:       true,
	}
}

func (db *packageDatabase) HttpClient() (*http.Client, error) {
	return &http.Client{}, nil
}

func (db *packageDatabase) UrlsFor(urlStr string) ([]string, error) {
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

func (db *packageDatabase) AddMirror(name string, options []string) error {
	db.mirrors[name] = options
	return nil
}

func (db *packageDatabase) AddContainerBuilder(b common.ContainerBuilder) error {
	db.containerBuilders[b.Key()] = b

	return nil
}

func (db *packageDatabase) LoadFile(filename string, allowLocal bool) error {
	thread := db.newThread(filename)

	globals := db.getGlobals("__main__")

	// Execute the file.
	contents, err := db.getFileContents(filename, allowLocal)
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
		db.defs[fmt.Sprintf("%s:%s", filename, k)] = v
	}

	return nil
}

func (db *packageDatabase) RunScript(filename string, files map[string]filesystem.File, additionalArgs []string, outputFilename string) error {
	thread := db.newThread(filename)

	globals := db.getGlobals("__main__")

	// Execute the script.
	contents, err := db.getFileContents(filename, true)
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
		args.args[k] = star.NewStarFile(v, k)
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

func (db *packageDatabase) LoadAll(parallel bool) error {
	ctx := db.builder.MinimalContext()

	if parallel {
		var wg sync.WaitGroup
		done := make(chan bool)
		errors := make(chan error)

		for _, builder := range db.containerBuilders {
			wg.Add(1)

			go func(builder common.ContainerBuilder) {
				defer wg.Done()

				if err := builder.EnsureLoaded(ctx); err != nil {
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
		for _, builder := range db.containerBuilders {
			if err := builder.EnsureLoaded(ctx); err != nil {
				return err
			}
		}

		return nil
	}
}

func (db *packageDatabase) NewName(name string, version string, tags []string) (common.PackageName, error) {
	return common.PackageName{
		Name:    name,
		Version: version,
		Tags:    tags,
	}, nil
}

func (db *packageDatabase) getBuilder(filename string, builder string) (starlark.Callable, error) {
	if filename == "" {
		return nil, fmt.Errorf("no filename passed to GetBuilder")
	}

	callable, ok := db.builders[fmt.Sprintf("%s:%s", filename, builder)]
	if !ok {
		return nil, fmt.Errorf("callable %s:%s not found", filename, builder)
	}

	return callable, nil
}

func (db *packageDatabase) GetContainerBuilder(name string, arch config.CPUArchitecture) (common.ContainerBuilder, error) {
	builder, ok := db.containerBuilders[fmt.Sprintf("%s-%s", name, arch)]
	if !ok {
		return nil, fmt.Errorf("builder %s not found for arch %s", name, arch)
	}

	if err := builder.EnsureLoaded(db.Builder().MinimalContext()); err != nil {
		return nil, err
	}

	return builder, nil
}

func (db *packageDatabase) GetMacro(ctx common.MacroContext, name string, args []string) (common.Macro, error) {
	def, ok := db.defs[name]
	if !ok {
		return nil, fmt.Errorf("name %s not found", name)
	}

	f, ok := def.(*starlark.Function)
	if !ok {
		return nil, fmt.Errorf("%s is not a valid macro (has type %s)", name, def.Type())
	}

	return macro.ParseMacro(ctx, f, args)
}

func (db *packageDatabase) GetMacroByDeclaredName(ctx common.MacroContext, name string, allowLocal bool) (common.Macro, error) {
	filename, defName, ok := strings.Cut(name, ":")
	if !ok {
		return nil, fmt.Errorf("misformed declared name: %s", name)
	}

	if !strings.HasSuffix(filename, ".star") {
		filename = filename + ".star"
	}

	if _, ok := db.loadedFiles[filename]; !ok {
		slog.Debug("load file for macro", "filename", filename)
		if err := db.LoadFile(filename, allowLocal); err != nil {
			return nil, err
		}
	}

	var macroArgs []string

	if strings.Contains(defName, ",") {
		macroTokens := strings.Split(defName, ",")

		defName = macroTokens[0]
		macroArgs = macroTokens[1:]
	}

	def, ok := db.defs[fmt.Sprintf("%s:%s", filename, defName)]
	if !ok {
		return nil, fmt.Errorf("name %s not found in %s", defName, filename)
	}

	if macroFunc, ok := def.(*starlark.Function); ok {
		return macro.ParseMacro(ctx, macroFunc, macroArgs)
	} else if buildDef, ok := def.(common.BuildDefinition); ok {
		return macro.DefinitionMacro{BuildDefinition: buildDef}, nil
	} else if dir, ok := def.(*common.StarDirective); ok {
		return macro.DirectiveMacro{Directive: dir.Directive}, nil
	} else {
		return nil, fmt.Errorf("could not interpret %s as macro/directive/definition", def.Type())
	}
}

func (db *packageDatabase) GetMacroByShorthand(ctx common.MacroContext, shorthand string, allowLocal bool) (common.Macro, error) {
	if len(shorthand) == 64 && !strings.Contains(shorthand, ":") {
		if !allowLocal {
			return nil, fmt.Errorf("local definitions are not allowed in remote configs")
		}

		def, err := db.builder.GetDefinitionByHash(hash.Hash(shorthand))
		if err != nil {
			return nil, err
		}

		return macro.DefinitionMacro{BuildDefinition: def}, nil
	}

	return db.GetMacroByDeclaredName(ctx, shorthand, allowLocal)
}

func (db *packageDatabase) NewMacroContext() common.MacroContext {
	return &macroContext{
		db:        db,
		builders:  make(map[string]common.InstallationPlanBuilder),
		variables: make(map[string]string),
	}
}

func (db *packageDatabase) loadBuiltinBuilders() error {
	for _, builder := range []string{
		"//fetchers/scratch.star",
		"//fetchers/alpine.star",
		"//fetchers/rpm.star",
		"//fetchers/debian.star",
		"//fetchers/arch.star",
	} {
		if err := db.LoadFile(builder, true); err != nil {
			return err
		}
	}

	return nil
}

func (db *packageDatabase) GetContainerBuilders() map[string]common.ContainerBuilder {
	ret := make(map[string]common.ContainerBuilder, len(db.containerBuilders))

	for k, v := range db.containerBuilders {
		ret[k] = v
	}

	return ret
}

func (db *packageDatabase) Call(filename string, builder string, args ...starlark.Value) (starlark.Value, error) {
	target, err := db.getBuilder(filename, builder)
	if err != nil {
		return starlark.None, fmt.Errorf("failed to GetBuilder in BuildContext.Call: %s", err)
	}

	result, err := starlark.Call(db.newThread(filename), target, args, []starlark.Tuple{})
	if err != nil {
		if sErr, ok := err.(*starlark.EvalError); ok {
			slog.Error("got starlark error", "error", sErr, "backtrace", sErr.Backtrace())
		}
		return starlark.None, err
	}

	return result, nil
}

func (db *packageDatabase) Builder() common.Builder {
	return db.builder
}

var (
	_ common.PackageDatabase = &packageDatabase{}
)

func New(builderFactory common.BuilderFactor) (common.PackageDatabase, error) {
	db := &packageDatabase{
		containerBuilders: make(map[string]common.ContainerBuilder),
		mirrors:           make(map[string][]string),
		defs:              make(map[string]starlark.Value),
		loadedFiles:       make(map[string]bool),
		builders:          make(map[string]starlark.Callable),
	}

	builder, err := builderFactory(db)
	if err != nil {
		return nil, err
	}

	db.builder = builder

	if err := db.loadBuiltinBuilders(); err != nil {
		return nil, err
	}

	return db, nil
}
