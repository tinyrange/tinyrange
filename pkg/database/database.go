package database

import (
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
	"time"

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

type tempArtifact struct {
	hash        hash.Hash
	defaultFile filesystem.File
}

// DigestFromFile implements common.BuildContext.
func (c *tempArtifact) DigestFromFile(file filesystem.File) (*filesystem.FileDigest, error) {
	filename, err := filesystem.GetHostFilename(file)
	if err != nil {
		return nil, err
	}

	return &filesystem.FileDigest{Hash: filename}, nil
}

// FileFromDigest implements common.BuildContext.
func (c *tempArtifact) FileFromDigest(digest *filesystem.FileDigest) (filesystem.File, error) {
	if digest.Hash != "" {
		return filesystem.NewLocalFile(digest.Hash, nil), nil
	}

	return nil, fmt.Errorf("could not convert digest to hash")
}

// HostFilenameFromFile implements common.BuildContext.
func (c *tempArtifact) HostFilenameFromFile(file filesystem.File) (string, error) {
	return filesystem.GetHostFilename(file)
}

// Default implements common.BuildArtifact.
func (t *tempArtifact) Default() (filesystem.File, error) {
	return t.defaultFile, nil
}

// Hash implements common.BuildArtifact.
func (t *tempArtifact) DefinitionHash() hash.Hash {
	return t.hash
}

// OpenFile implements common.BuildArtifact.
func (t *tempArtifact) OpenFile(name string) (filesystem.FileHandle, error) {
	return nil, fmt.Errorf("unimplemented")
}

// Receipt implements common.BuildArtifact.
func (t *tempArtifact) Receipt() common.BuildReceipt {
	return common.BuildReceipt{}
}

var (
	_ common.BuildArtifact = &tempArtifact{}
)

type builder1 struct {
	database *packageDatabase

	rebuildUserDefinitions bool

	distributionServer string

	buildCache map[hash.Hash]common.BuildArtifact

	buildStatusMtx sync.Mutex
	buildStatuses  map[common.BuildDefinition]*buildStatus

	defDb *hash.DefinitionDatabase

	buildDir string
}

// MinimalContext implements common.Builder.
func (db *builder1) MinimalContext() common.MinimalBuildContext {
	return db.newBuildContext(nil)
}

// SetRebuildUserDefinitions implements common.PackageDatabase.
func (db *builder1) SetRebuildUserDefinitions(rebuild bool) {
	db.rebuildUserDefinitions = rebuild
}

// HashDefinition implements common.PackageDatabase.
func (db *builder1) HashDefinition(def common.BuildDefinition) (hash.Hash, error) {
	return db.defDb.HashDefinition(def)
}

func (db *builder1) newBuildContext(def common.BuildDefinition) *buildContext {
	return &buildContext{def: def, builder: db}
}

func (db *builder1) updateBuildStatus(def common.BuildDefinition, status *buildStatus) {
	db.buildStatusMtx.Lock()
	defer db.buildStatusMtx.Unlock()

	db.buildStatuses[def] = status
}

func (db *builder1) filenameFromHash(hash hash.Hash, suffix string) (string, error) {
	return filepath.Join(db.buildDir, string(hash)+suffix), nil
}

func (db *builder1) downloadFromDistributionServer(hash hash.Hash, def common.BuildDefinition) (bool, error) {
	if redistributable, ok := def.(common.RedistributableDefinition); !ok || !redistributable.Redistributable() {
		return false, nil // not redistributable
	}

	client, err := db.database.HttpClient()
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

func (db *builder1) getBuildStatus(def common.BuildDefinition) (*buildStatus, error) {
	status, ok := db.buildStatuses[def]
	if !ok {
		return nil, fmt.Errorf("build status not found")
	}
	return status, nil
}

func (db *builder1) build(c common.BuildContext, def common.BuildDefinition, opts common.BuildOptions) (common.BuildArtifact, error) {
	hash, err := db.HashDefinition(def)
	if err != nil {
		return nil, err
	}

	if f, ok := db.buildCache[hash]; ok {
		return f, nil
	}

	status := &buildStatus{}

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

	ctx.hash = hash

	// Get a child context for the build.
	child := ctx.childContext(def, status, tmpFilename)

	if !opts.AlwaysRebuild {
		// Check if the file already exists. If it does then return it.
		if info, err := os.Stat(filename); err == nil {
			var needsRebuild = false

			child.lastBuild = info.ModTime()

			// Only check for rebuilds if the child is not downloaded.
			if exists, _ := common.Exists(downloadedTag); !exists {
				// If the file has already been created then check if a rebuild is needed.
				needsRebuild, err = def.NeedsBuild(child)
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

				slog.Debug("cached", "Hash", hash.String(), "filename", filename)

				return &tempArtifact{
					hash:        hash,
					defaultFile: filesystem.NewLocalFile(filename, def),
				}, nil
			}

			slog.Debug("rebuild requested", "Hash", hash.String())
		} else {
			slog.Debug("building", "Hash", hash.String())
		}
	} else {
		slog.Debug("building", "Hash", hash.String())
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

			art := &tempArtifact{
				hash:        hash,
				defaultFile: filesystem.NewLocalFile(filename, def),
			}

			db.buildCache[hash] = art

			// Return the file.
			return art, nil
		}
	}

	// If the downloaded tag exists then remove it.

	// If not then trigger the build.
	err = def.Build(child)
	if err == common.ErrUseExistingBuild {
		status.Status = buildStatusCached

		// Write the build status.
		db.updateBuildStatus(def, status)

		return &tempArtifact{
			hash:        hash,
			defaultFile: filesystem.NewLocalFile(filename, def),
		}, nil
	} else if err != nil {
		return nil, err
	}

	// If the build has already been written then don't write it again.
	if !child.HasCreatedOutput() {
		return nil, fmt.Errorf("output not created")
	} else {
		// Close the output file.
		// Ignore errors since it may have already been closed.
		child.output.Close()
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

	art := &tempArtifact{
		hash:        hash,
		defaultFile: filesystem.NewLocalFile(filename, def),
	}

	db.buildCache[hash] = art

	// Return the file.
	return art, nil
}

func (db *builder1) Build(def common.BuildDefinition, opts common.BuildOptions) (common.BuildArtifact, error) {
	return db.build(db.newBuildContext(def), def, opts)
}

func (db *builder1) missDefinitionCache(hash hash.Hash) (io.ReadCloser, error) {
	filename, err := db.filenameFromHash(hash, ".def")
	if err != nil {
		return nil, err
	}

	return os.Open(filename)
}

func (db *builder1) GetDefinitionByHash(hash hash.Hash) (common.BuildDefinition, error) {
	def, err := db.defDb.GetDefinitionByHash(hash)
	if err != nil {
		return nil, err
	}

	return def.(common.BuildDefinition), nil
}

func (db *builder1) SetDistributionServer(server string) error {
	client, err := db.database.HttpClient()
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

// GarbageCollect implements common.Builder.
func (db *builder1) GarbageCollect(olderThan time.Time) ([]hash.Hash, error) {
	return nil, fmt.Errorf("unimplemented")
}

var (
	_ common.Builder = &builder1{}
)

type packageDatabase struct {
	// keys are name-arch
	ContainerBuilders map[string]*containerBuilder

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
	ctx := db.builder.MinimalContext()

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
	builder, ok := db.ContainerBuilders[fmt.Sprintf("%s-%s", name, arch)]
	if !ok {
		return nil, fmt.Errorf("builder %s not found for arch %s", name, arch)
	}

	if err := builder.ensureLoaded(db.Builder().MinimalContext()); err != nil {
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
	ret := make(map[string]common.ContainerBuilder, len(db.ContainerBuilders))

	for k, v := range db.ContainerBuilders {
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

func New(buildDir string) (common.PackageDatabase, error) {
	db := &packageDatabase{
		ContainerBuilders: make(map[string]*containerBuilder),
		mirrors:           make(map[string][]string),
		defs:              make(map[string]starlark.Value),
		loadedFiles:       make(map[string]bool),
		builders:          make(map[string]starlark.Callable),
	}

	builder := &builder1{
		database:      db,
		buildStatuses: make(map[common.BuildDefinition]*buildStatus),
		buildCache:    make(map[hash.Hash]common.BuildArtifact),
		buildDir:      buildDir,
	}

	builder.defDb = hash.NewDefinitionDatabase(builder.missDefinitionCache)

	db.builder = builder

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
