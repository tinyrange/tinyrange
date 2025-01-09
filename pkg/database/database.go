package database

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/schollz/progressbar/v3"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/macro"
	"github.com/tinyrange/tinyrange/stdlib"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

type buildStatusKind byte

const (
	buildStatusBuilt buildStatusKind = iota
	buildStatusCached
)

func (s buildStatusKind) String() string {
	switch s {
	case buildStatusBuilt:
		return "Built"
	case buildStatusCached:
		return "Cached"
	default:
		return "<unknown BuildStatusKind>"
	}
}

type buildStatus struct {
	Status   buildStatusKind
	Tag      string
	Children []common.BuildDefinition
}

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
	ContainerBuilders map[string]*containerBuilder

	rebuildUserDefinitions bool

	mirrors map[string][]string

	memoryCache map[string][]byte
	buildCache  map[hash.Hash]filesystem.File

	buildStatusMtx sync.Mutex
	buildStatuses  map[common.BuildDefinition]*buildStatus

	loadedFiles map[string]bool
	defs        map[string]starlark.Value

	builders map[string]starlark.Callable

	defDb *hash.DefinitionDatabase

	buildDir           string
	distributionServer string
}

// HashDefinition implements common.PackageDatabase.
func (db *packageDatabase) HashDefinition(def common.BuildDefinition) (hash.Hash, error) {
	return db.defDb.HashDefinition(def)
}

// SetRebuildUserDefinitions implements common.PackageDatabase.
func (db *packageDatabase) SetRebuildUserDefinitions(rebuild bool) {
	db.rebuildUserDefinitions = rebuild
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
	builder, ok := b.(*containerBuilder)
	if !ok {
		return fmt.Errorf("expected containerBuilder, got %T", b)
	}

	db.ContainerBuilders[builder.Key()] = builder

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

func (db *packageDatabase) LoadAll(parallel bool) error {
	ctx := db.newBuildContext(nil)

	if parallel {
		var wg sync.WaitGroup
		done := make(chan bool)
		errors := make(chan error)

		for _, builder := range db.ContainerBuilders {
			wg.Add(1)

			go func(builder *containerBuilder) {
				defer wg.Done()

				if err := builder.ensureLoaded(ctx); err != nil {
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
			if err := builder.ensureLoaded(ctx); err != nil {
				return err
			}
		}

		return nil
	}
}

func (db *packageDatabase) newBuildContext(def common.BuildDefinition) *buildContext {
	return &buildContext{def: def, database: db}
}

func (db *packageDatabase) NewBuildContext(def common.BuildDefinition) common.BuildContext {
	return db.newBuildContext(def)
}

func (db *packageDatabase) updateBuildStatus(def common.BuildDefinition, status *buildStatus) {
	db.buildStatusMtx.Lock()
	defer db.buildStatusMtx.Unlock()

	db.buildStatuses[def] = status
}

func (db *packageDatabase) filenameFromHash(hash hash.Hash, suffix string) (string, error) {
	return filepath.Join(db.buildDir, string(hash)+suffix), nil
}

func (db *packageDatabase) downloadFromDistributionServer(hash hash.Hash, def common.BuildDefinition) (bool, error) {
	if redistributable, ok := def.(common.RedistributableDefinition); !ok || !redistributable.Redistributable() {
		return false, nil // not redistributable
	}

	client, err := db.HttpClient()
	if err != nil {
		return false, err
	}

	url := fmt.Sprintf("%s/result/%s", db.distributionServer, hash)

	resp, err := client.Get(url)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	} else if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("bad status %s", resp.Status)
	}

	filename, err := db.filenameFromHash(hash, ".bin")
	if err != nil {
		return false, err
	}

	tmpFilename := filename + ".tmp"

	f, err := os.Create(tmpFilename)
	if err != nil {
		return false, err
	}

	pb := progressbar.DefaultBytes(resp.ContentLength, url)
	defer pb.Close()

	if _, err := io.Copy(io.MultiWriter(f, pb), resp.Body); err != nil {
		f.Close()
		os.Remove(tmpFilename)
		return false, err
	}

	if err := f.Close(); err != nil {
		return false, err
	}

	if err := os.Rename(tmpFilename, filename); err != nil {
		return false, err
	}

	downloadedTag, err := db.filenameFromHash(hash, ".downloaded")
	if err != nil {
		return false, err
	}

	if err := os.WriteFile(downloadedTag, []byte(""), os.ModePerm); err != nil {
		return false, err
	}

	return true, nil
}

func (db *packageDatabase) build(c common.BuildContext, def common.BuildDefinition, opts common.BuildOptions) (filesystem.File, error) {
	tag := def.Tag()

	hash, err := db.HashDefinition(def)
	if err != nil {
		return nil, err
	}

	if f, ok := db.buildCache[hash]; ok {
		return f, nil
	}

	status := &buildStatus{Tag: tag}

	filename, err := db.filenameFromHash(hash, ".bin")
	if err != nil {
		return nil, err
	}

	downloadedTag, err := db.filenameFromHash(hash, ".downloaded")
	if err != nil {
		return nil, err
	}

	tmpFilename := filename + ".tmp"

	ctx, ok := c.(*buildContext)
	if !ok {
		return nil, fmt.Errorf("expected buildContext, got %T", c)
	}

	// Get a child context for the build.
	child := ctx.childContext(def, status, tmpFilename)

	if !opts.AlwaysRebuild {
		// Check if the file already exists. If it does then return it.
		if info, err := os.Stat(filename); err == nil {
			var needsRebuild = false

			// Only check for rebuilds if the child is not downloaded.
			if exists, _ := common.Exists(downloadedTag); !exists {
				// If the file has already been created then check if a rebuild is needed.
				needsRebuild, err = def.NeedsBuild(child, info.ModTime())
				if err != nil {
					return nil, err
				}
			} else {
				// Redistributed results are considered user definitions.
				if db.rebuildUserDefinitions {
					needsRebuild = true
				}
			}

			// If no rebuild is necessary then skip it.
			if !needsRebuild {
				status.Status = buildStatusCached

				// Write the build status.
				db.updateBuildStatus(def, status)

				slog.Debug("cached", "Tag", def.Tag(), "filename", filename)

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

	defFilename, err := db.filenameFromHash(hash, ".def")
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(defFilename, defValue, os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to write definition: %s", err)
	}

	if db.distributionServer != "" {
		// If we have a distribution server then check it first.
		ok, err := db.downloadFromDistributionServer(hash, def)
		if err != nil {
			return nil, err
		}

		if ok {
			status.Status = buildStatusBuilt

			db.updateBuildStatus(def, status)

			// This definition is redistributable so write a manifest.
			redistributableTag, err := db.filenameFromHash(hash, ".redistributable")
			if err != nil {
				return nil, err
			}

			if err := os.WriteFile(redistributableTag, []byte(""), os.ModePerm); err != nil {
				return nil, err
			}

			f := filesystem.NewLocalFile(filename, def)

			db.buildCache[hash] = f

			// Return the file.
			return f, nil
		}
	}

	// If the downloaded tag exists then remove it.

	// If not then trigger the build.
	result, err := def.Build(child)
	if err != nil {
		return nil, err
	}

	// If the result is nil then the builder is telling us to use the cached version.
	if result == nil {
		status.Status = buildStatusCached

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
		if err := result.WriteResult(outFile); err != nil {
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
		if err := result.WriteResult(nil); err != nil {
			os.Remove(tmpFilename)
			return nil, err
		}
	}

	// Finally rename the temporary file to the final filename.
	if err := os.Rename(tmpFilename, filename); err != nil {
		os.Remove(tmpFilename)
		return nil, err
	}

	status.Status = buildStatusBuilt

	// Write the build status.
	db.updateBuildStatus(def, status)

	if redistributable, ok := def.(common.RedistributableDefinition); ok && redistributable.Redistributable() {
		// This definition is redistributable so write a manifest.

		redistributableTag, err := db.filenameFromHash(hash, ".redistributable")
		if err != nil {
			return nil, err
		}

		if err := os.WriteFile(redistributableTag, []byte(""), os.ModePerm); err != nil {
			return nil, err
		}
	}

	f := filesystem.NewLocalFile(filename, def)

	db.buildCache[hash] = f

	// Return the file.
	return f, nil
}

func (db *packageDatabase) Build(def common.BuildDefinition, opts common.BuildOptions) (filesystem.File, error) {
	return db.build(db.NewBuildContext(def), def, opts)
}

func (db *packageDatabase) getBuildStatus(def common.BuildDefinition) (*buildStatus, error) {
	status, ok := db.buildStatuses[def]
	if !ok {
		return nil, fmt.Errorf("build status not found")
	}
	return status, nil
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

func (db *packageDatabase) GetContainerBuilder(c common.BuildContext, name string, arch config.CPUArchitecture) (common.ContainerBuilder, error) {
	ctx, ok := c.(*buildContext)
	if !ok {
		return nil, fmt.Errorf("expected buildContext, got %T", ctx)
	}

	builder, ok := db.ContainerBuilders[fmt.Sprintf("%s-%s", name, arch)]
	if !ok {
		return nil, fmt.Errorf("builder %s not found for arch %s", name, arch)
	}

	if err := builder.ensureLoaded(ctx); err != nil {
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

func (db *packageDatabase) missDefinitionCache(hash hash.Hash) (io.ReadCloser, error) {
	filename, err := db.filenameFromHash(hash, ".def")
	if err != nil {
		return nil, err
	}

	return os.Open(filename)
}

func (db *packageDatabase) GetDefinitionByHash(hash hash.Hash) (common.BuildDefinition, error) {
	def, err := db.defDb.GetDefinitionByHash(hash)
	if err != nil {
		return nil, err
	}

	return def.(common.BuildDefinition), nil
}

func (db *packageDatabase) GetMacroByShorthand(ctx common.MacroContext, shorthand string, allowLocal bool) (common.Macro, error) {
	if len(shorthand) == 64 && !strings.Contains(shorthand, ":") {
		if !allowLocal {
			return nil, fmt.Errorf("local definitions are not allowed in remote configs")
		}

		def, err := db.GetDefinitionByHash(hash.Hash(shorthand))
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

func (db *packageDatabase) GetAllHashes() ([]hash.Hash, error) {
	var ret []hash.Hash

	ents, err := os.ReadDir(db.buildDir)
	if err != nil {
		return nil, err
	}

	for _, ent := range ents {
		ext := filepath.Ext(ent.Name())
		if ext == ".def" {
			ret = append(ret, hash.Hash(strings.TrimSuffix(ent.Name(), ext)))
		}
	}

	return ret, nil
}

func (db *packageDatabase) Inspect(def common.BuildDefinition, out io.Writer) error {
	defBytes, err := db.defDb.MarshalDefinition(def)
	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)

	if err := json.Indent(buf, defBytes, "", "  "); err != nil {
		return err
	}

	fmt.Fprintf(out, "definition JSON:\n%s\n\n", buf.String())

	hash, err := db.HashDefinition(def)
	if err != nil {
		return err
	}

	filename, err := db.filenameFromHash(hash, ".bin")
	if err != nil {
		return err
	}

	_, err = os.Stat(filename)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(out, "built definition does not exist at: %s\n", filename)
		return nil
	} else if err != nil {
		return err
	}

	// assume it's an archive.
	fmt.Fprintf(out, "archive entries:\n")

	ark, err := filesystem.ReadArchiveFromFile(filesystem.NewLocalFile(filename, nil))
	if err != nil {
		return err
	}

	ents, err := ark.Entries()
	if err != nil {
		return err
	}

	for _, ent := range ents {
		switch ent.Typeflag() {
		case filesystem.TypeDirectory:
			fmt.Fprintf(out, "D %04d:%04d % 10d %s %s\n", ent.Uid(), ent.Gid(), ent.Size(), ent.ModTime(), ent.Name())
		case filesystem.TypeRegular:
			fmt.Fprintf(out, "R %04d:%04d % 10d %s %s\n", ent.Uid(), ent.Gid(), ent.Size(), ent.ModTime(), ent.Name())
		case filesystem.TypeSymlink:
			fmt.Fprintf(out, "S %04d:%04d % 10d %s %s -> %s\n", ent.Uid(), ent.Gid(), ent.Size(), ent.Name(), ent.ModTime(), ent.Linkname())
		}
	}

	return nil
}

func (db *packageDatabase) loadBuiltinBuilders() error {
	for _, builder := range []string{
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

func (db *packageDatabase) SetDistributionServer(server string) error {
	client, err := db.HttpClient()
	if err != nil {
		return err
	}

	resp, err := client.Get(server + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if !slices.Equal(content, []byte("OK")) {
		return fmt.Errorf("bad response from distribution server")
	}

	db.distributionServer = server

	return nil
}

func (db *packageDatabase) GetContainerBuilders() map[string]common.ContainerBuilder {
	ret := make(map[string]common.ContainerBuilder, len(db.ContainerBuilders))

	for k, v := range db.ContainerBuilders {
		ret[k] = v
	}

	return ret
}

var (
	_ common.PackageDatabase = &packageDatabase{}
)

func New(buildDir string) (common.PackageDatabase, error) {
	db := &packageDatabase{
		ContainerBuilders: make(map[string]*containerBuilder),
		mirrors:           make(map[string][]string),
		memoryCache:       make(map[string][]byte),
		buildCache:        make(map[hash.Hash]filesystem.File),
		buildStatuses:     make(map[common.BuildDefinition]*buildStatus),
		buildDir:          buildDir,
		defs:              make(map[string]starlark.Value),
		loadedFiles:       make(map[string]bool),
		builders:          make(map[string]starlark.Callable),
	}

	db.defDb = hash.NewDefinitionDatabase(db.missDefinitionCache)

	// Check with Exists first so it doesn't have issues if the build dir is behind a symlink.
	if ok, _ := common.Exists(buildDir); !ok {
		if err := common.Ensure(buildDir, os.ModePerm); err != nil {
			return nil, err
		}
	}

	if err := db.loadBuiltinBuilders(); err != nil {
		return nil, err
	}

	return db, nil
}
