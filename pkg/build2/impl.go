package build2

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	cryptoHash "hash"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	builderFactory "github.com/tinyrange/tinyrange/pkg/builder"
	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/log"
	"github.com/tinyrange/tinyrange/pkg/path"
	"github.com/tinyrange/tinyrange/pkg/star"
	"go.starlark.net/starlark"
)

const (
	defaultSuffix = "default"
)

type buildArtifact struct {
	*buildContext
}

// ReferenceForDefault implements common.BuildArtifact.
func (a *buildArtifact) ReferenceForDefault() (config.DatabaseReference, error) {
	return a.ReferenceForFile(defaultSuffix)
}

// ReferenceForFile implements common.BuildArtifact.
func (a *buildArtifact) ReferenceForFile(name string) (config.DatabaseReference, error) {
	if a.hash.String() == "" {
		return config.DatabaseReference{}, fmt.Errorf("hash is not set")
	}

	if name == "" {
		return config.DatabaseReference{}, fmt.Errorf("filename is empty")
	}

	return config.DatabaseReference{
		Hash:     a.hash.String(),
		Filename: name,
	}, nil
}

// File implements common.BuildArtifact.
func (a *buildArtifact) File(name string) (filesystem.File, error) {
	if _, ok := a.recept.Files[name]; !ok {
		return nil, fmt.Errorf("file %s not found", name)
	}

	f, err := a.buildDir.File(name)
	if err != nil {
		return nil, err
	}

	return filesystem.NewSourceWrapper(f, a.def), nil
}

// OpenFile implements BuildArtifact.
func (a *buildArtifact) OpenFile(name string) (filesystem.FileHandle, error) {
	if _, ok := a.recept.Files[name]; !ok {
		return nil, fmt.Errorf("file %s not found", name)
	}

	f, err := a.buildDir.File(name)
	if err != nil {
		return nil, err
	}

	return f.Open()
}

// Default implements BuildArtifact.
func (a *buildArtifact) Default() (filesystem.File, error) {
	if _, ok := a.recept.Files[defaultSuffix]; !ok {
		return nil, fmt.Errorf("file default not found")
	}

	f, err := a.buildDir.File(defaultSuffix)
	if err != nil {
		return nil, err
	}

	return filesystem.NewSourceWrapper(f, a.def), nil
}

// Receipt implements BuildArtifact.
func (a *buildArtifact) Receipt() common.BuildReceipt {
	return *a.recept
}

var (
	_ common.BuildArtifact = &buildArtifact{}
)

type contextFile struct {
	writer common.OutputFileHandle
	multi  io.Writer
	hash   cryptoHash.Hash
}

// Write implements io.Writer.
func (f *contextFile) Write(p []byte) (n int, err error) {
	return f.multi.Write(p)
}

// Close implements io.Closer.
func (f *contextFile) Close() error {
	if err := f.writer.Close(); err != nil {
		return err
	}

	return nil
}

var (
	_ io.WriteCloser = &contextFile{}
)

type buildContextState uint32

const (
	buildContextStateNew buildContextState = iota
	buildContextStateBuilding
	buildContextStateBuilt
	buildContextStateUsedCache
)

func runVMM(exe string, buildDir string, configFilename string) (*exec.Cmd, error) {
	persistPath := path.Native.Join(buildDir, "persist")

	if err := common.Ensure(persistPath, os.ModePerm); err != nil {
		return nil, err
	}

	cmd := exec.Command(exe, "-build-dir", buildDir, "-persist-path", persistPath, configFilename)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Debug("executing VMM", "args", cmd.Args)

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd, nil
}

type buildContext struct {
	builder      *builder
	parent       *buildContext
	hash         hash.Hash
	def          common.BuildDefinition
	buildDir     common.BuildCacheDirectory
	options      common.BuildOptions
	recept       *common.BuildReceipt
	requirements map[hash.Hash]struct{}
	err          error
	state        buildContextState
	wg           sync.WaitGroup
	logger       Logger
	token        *token
	files        map[string]*contextFile
}

// DatabaseConfig implements common.BuildContext.
func (b *buildContext) DatabaseConfig() ([]filesystem.BuildDatabaseConfig, error) {
	return b.builder.buildDir.DatabaseConfig()
}

// Factory implements common.BuildContext.
func (b *buildContext) Factory() common.DefinitionFactory {
	return builderFactory.Factory
}

// PrenotifyChildren implements common.BuildContext.
func (c *buildContext) PrenotifyChildren(children []common.BuildDefinition) error {
	for _, child := range children {
		c.builder.contextForDefinition(c, child, common.BuildOptions{})
	}

	return nil
}

// HostFilenameFromFile implements common.BuildContext.
func (c *buildContext) HostFilenameFromFile(file filesystem.File) (string, error) {
	return filesystem.GetHostFilename(file)
}

// CreateDefault implements common.BuildContext.
func (c *buildContext) CreateDefault() (io.WriteCloser, error) {
	return c.CreateFile(defaultSuffix)
}

// RunVMM implements common.BuildContext.
func (c *buildContext) RunVMM(name string, config config.TinyRangeConfig) (*exec.Cmd, error) {
	out, err := c.CreateFile("config.json")
	if err != nil {
		return nil, err
	}

	configFilename, err := out.(*contextFile).writer.GetHostFilename()
	if err != nil {
		out.Close()
		return nil, err
	}

	enc := json.NewEncoder(out)

	if err := enc.Encode(&config); err != nil {
		out.Close()
		return nil, err
	}

	if err := out.Close(); err != nil {
		return nil, err
	}

	if name == "" {
		return nil, common.ErrTemplateBuilt(configFilename)
	}

	var exe string

	if name == "qemu" {
		exe, err = common.GetAdjacentExecutable("tinyrange_qemu", "tinyqemu/tinyrange_qemu")
		if err != nil {
			return nil, err
		}
	} else if name == "vz" && runtime.GOOS == "darwin" {
		exe, err = common.GetAdjacentExecutable("tinyrange_vz")
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("unknown VMM: %s", name)
	}

	buildDirPath, err := c.builder.buildDir.GetHostFilename()
	if err != nil {
		return nil, err
	}

	return runVMM(exe, buildDirPath, configFilename)
}

// Database implements common.BuildContext.
func (c *buildContext) Database() common.PackageDatabase {
	return c.builder.database
}

// ShouldRebuildUserDefinitions implements common.BuildContext.
func (c *buildContext) ShouldRebuildUserDefinitions() bool {
	return c.builder.rebuildUserDefinitions
}

// Precondition: The definition has to be rebuilt.
func (c *buildContext) build(writable common.WritableBuildCacheDirectory) error {
	// c.logger.Describe(ColorYellow, "%s : waiting for token", c.def.String())
	defer c.logger.Close()

	// Update the state.
	atomic.StoreUint32((*uint32)(&c.state), uint32(buildContextStateBuilding))
	c.logger.Describe(ColorYellow, "starting build")

	// Save the definition to the build directory.
	def, err := c.builder.defDb.MarshalDefinition(c.def)
	if err != nil {
		return err
	}
	if err := writable.WriteDefinition(def); err != nil {
		return err
	}

	startTime := time.Now()

	c.recept = &common.BuildReceipt{
		Files: make(map[string]string),
	}

	// Build the dependencies.
	deps, err := c.def.Dependencies()
	if err != nil {
		return err
	}

	for _, dep := range deps {
		c.addDependency(dep)
	}

	// Call the user builder function.
	err = c.def.Build(c)
	if errors.Is(err, common.ErrNonFatal{}) {
		log.Warn("non-fatal error building", "def", c.def.String(), "err", err)

		attempts := 0

		// If the error is non-fatal, retry the build.
		for {
			attempts += 1
			if attempts > 10 {
				return fmt.Errorf("error building %s after %d: %w", c.def.String(), attempts, err)
			}

			log.Warn("non-fatal error building", "def", c.def.String(), "err", err, "attempts", attempts)

			time.Sleep(1 * time.Second)

			err = c.def.Build(c)
			if err == nil || !errors.Is(err, common.ErrNonFatal{}) {
				break
			}
		}
	}
	if errors.Is(err, common.ErrUseExistingBuild) {
		// The user has requested to use the existing build.
		// This is a special case where the user has determined that the build is not needed.
		// We can skip the rest of the build process.

		currentRecept, err := c.loadRecept()
		if err != nil {
			return err
		}

		c.logger.Describe(ColorGreen, "skipped build, using existing build")

		currentRecept.StartTime = c.recept.StartTime

		c.recept = currentRecept
	} else if err != nil {
		return fmt.Errorf("failed to build %s: %w", c.def.String(), err)
	} else {
		c.recept.StartTime = startTime
		c.recept.Duration = time.Since(c.recept.StartTime)

		// Set all the hashes in the recept.
		for name, file := range c.files {
			c.recept.Files[name] = fmt.Sprintf("%x", file.hash.Sum(nil))
		}
	}

	// Save the recept to the build directory.
	recept, err := json.Marshal(c.recept)
	if err != nil {
		return err
	}
	if err := writable.WriteReceipt(recept); err != nil {
		return err
	}

	// Update the state.
	atomic.StoreUint32((*uint32)(&c.state), uint32(buildContextStateBuilt))
	c.logger.Describe(ColorGreen, "built successfully in %s", c.recept.Duration)

	return nil
}

func (c *buildContext) loadRecept() (*common.BuildReceipt, error) {
	recept, err := c.buildDir.ReadReceipt()
	if err != nil {
		return nil, err
	}

	if len(recept) == 0 {
		return nil, io.EOF
	}

	var ret common.BuildReceipt
	if err := json.Unmarshal(recept, &ret); err != nil {
		return nil, err
	}

	return &ret, nil
}

// Precondition: The context has an exclusive lock on the definition.
func (c *buildContext) ensureUpToDate() error {
	var err error

	if c.def == nil {
		return fmt.Errorf("definition is nil")
	}

	if c.hash == "" {
		// compute the definition hash
		c.hash, err = c.builder.defDb.HashDefinition(c.def)
		if err != nil {
			return fmt.Errorf("failed to hash definition: %w", err)
		}
	}

	if c.buildDir == nil {
		c.buildDir, err = c.builder.buildDir.GetBuildDirectory(c.hash)
		if errors.Is(err, fs.ErrNotExist) {
			c.buildDir, err = c.builder.buildDir.CreateBuildDirectory(c.hash)
			if err != nil {
				return fmt.Errorf("failed to create build directory: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to get build directory: %w", err)
		}
	}

	writable, isWritable := c.buildDir.(common.WritableBuildCacheDirectory)

	if c.options.AlwaysRebuild {
		if !isWritable {
			c.buildDir, err = c.builder.buildDir.CreateBuildDirectory(c.hash)
			if err != nil {
				return fmt.Errorf("failed to create build directory: %w", err)
			}

			writable = c.buildDir.(common.WritableBuildCacheDirectory)
		}

		// force a rebuild
		defer c.token.Lock("forced rebuild").Close()

		return c.build(writable)
	}

	if c.recept == nil {
		// load the recept
		c.recept, err = c.loadRecept()
		if errors.Is(err, fs.ErrNotExist) || err == io.EOF {
			if !isWritable {
				c.buildDir, err = c.builder.buildDir.CreateBuildDirectory(c.hash)
				if err != nil {
					return fmt.Errorf("failed to create build directory: %w", err)
				}

				writable = c.buildDir.(common.WritableBuildCacheDirectory)
			}

			defer c.token.Lock("fresh build").Close()

			return c.build(writable)
		} else if err != nil {
			return fmt.Errorf("failed to load recept: %w", err)
		}
	}

	if isWritable {
		needsBuild, err := c.def.NeedsBuild(c)
		if err != nil {
			return fmt.Errorf("failed to check if build is needed: %w", err)
		}
		if needsBuild {
			c.logger.Describe(ColorYellow, "rebuilding due to user NeedsBuild")

			defer c.token.Lock("user needs build").Close()

			return c.build(writable)
		}

		// Lock a token since we might be triggering rebuilds of children so we need to ensure we have a token.
		defer c.token.Lock("child rebuild").Close()

		// Check all requirements in parallel.
		for _, req := range c.recept.Requirements {
			child, err := c.builder.contextForHash(c, req, common.BuildOptions{})
			if errors.Is(err, fs.ErrNotExist) {
				c.logger.Describe(ColorYellow, "rebuilding due to missing requirement %s", req.String())
				return c.build(writable)
			} else if err != nil {
				return fmt.Errorf("failed to load requirement: %w", err)
			}

			// Ensure the child has a token.
			if state := atomic.LoadUint32((*uint32)(&child.state)); state == uint32(buildContextStateNew) {
				child.token.Donate()
			}

			art, err := child.getArtifact()
			if err != nil {
				return fmt.Errorf("failed to get requirement artifact: %w", err)
			}

			if art.freshlyBuilt() {
				c.logger.Describe(ColorYellow, "rebuilding due to requirement %s", child.def.String())
				return c.build(writable)
			}
		}
	}

	atomic.StoreUint32((*uint32)(&c.state), uint32(buildContextStateUsedCache))

	return nil
}

func (c *buildContext) getArtifact() (*buildArtifact, error) {
	// Wait for the build to complete.
	c.wg.Wait()

	// Check for errors.
	if c.err != nil {
		return nil, c.err
	}

	// Check the state.
	return &buildArtifact{c}, nil
}

func (c *buildContext) freshlyBuilt() bool {
	return atomic.LoadUint32((*uint32)(&c.state)) == uint32(buildContextStateBuilt)
}

func (c *buildContext) addDependency(def common.BuildDefinition) {
	// blindly add the dependency to trigger the build or load.
	// it will only turn into a requirement if it's used as a child.
	c.builder.contextForDefinition(c, def, common.BuildOptions{})
}

func (c *buildContext) addRequirement(hash hash.Hash) {
	if _, ok := c.requirements[hash]; ok {
		return
	}
	c.requirements[hash] = struct{}{}
	c.recept.Requirements = append(c.recept.Requirements, hash)
}

func (c *buildContext) BuildChild(def common.BuildDefinition) (common.BuildArtifact, error) {
	child := c.builder.contextForDefinition(c, def, common.BuildOptions{})

	if state := atomic.LoadUint32((*uint32)(&child.state)); state == uint32(buildContextStateNew) {
		child.token.Donate()
	}

	artifact, err := child.getArtifact()
	if err != nil {
		return nil, err
	}

	c.addRequirement(child.hash)

	return artifact, nil
}

func (c *buildContext) Describe(format string, args ...interface{}) {
	c.logger.Describe(ColorDefault, format, args...)
}

func (c *buildContext) Logf(format string, args ...interface{}) {
	c.logger.Logf(format, args...)
}

func (c *buildContext) LastBuild() time.Time {
	if c.recept == nil {
		return time.Time{}
	}

	return c.recept.StartTime
}

var validFilename = regexp.MustCompile(`^[a-zA-Z0-9._]+$`)

func (c *buildContext) CreateFile(name string) (io.WriteCloser, error) {
	if !validFilename.MatchString(name) {
		return nil, fmt.Errorf("invalid filename: %s", name)
	}

	writable, ok := c.buildDir.(common.WritableBuildCacheDirectory)
	if !ok {
		return nil, fmt.Errorf("build directory is not writable")
	}

	// Check if the file already exists.
	if _, ok := c.files[name]; ok {
		return nil, fmt.Errorf("file %s already exists", name)
	}

	// Create the file.
	mutHandle, err := writable.CreateOutputFile(name)
	if err != nil {
		return nil, err
	}

	// Automatically hash the file as it's written.
	hash := sha256.New()

	multi := io.MultiWriter(mutHandle, hash)

	c.files[name] = &contextFile{
		writer: mutHandle,
		multi:  multi,
		hash:   hash,
	}

	return c.files[name], nil
}

// Hash implements BuildContext.
func (a *buildContext) DefinitionHash() hash.Hash {
	return a.hash
}

// WriteDefault implements common.BuildContext.
func (c *buildContext) WriteDefault(result common.BuildResult) error {
	out, err := c.CreateFile(defaultSuffix)
	if err != nil {
		return err
	}
	defer out.Close()

	if err := result.WriteResult(out); err != nil {
		return err
	}

	return nil
}

func (c *buildContext) Freeze()               {}
func (c *buildContext) Hash() (uint32, error) { return 0, fmt.Errorf("not hashable") }
func (c *buildContext) String() string        { return c.hash.String() }
func (c *buildContext) Truth() starlark.Bool  { return starlark.False }
func (c *buildContext) Type() string          { return "buildContext" }

func (c *buildContext) Attr(name string) (starlark.Value, error) {
	return star.BuildContextAttr(c, name)
}

func (c *buildContext) AttrNames() []string {
	return star.BuildContextAttrNames()
}

var (
	_ common.BuildContext = &buildContext{}
	_ starlark.HasAttrs   = &buildContext{}
)

type builder struct {
	currentlyBuilding      atomic.Bool
	database               common.PackageDatabase
	buildDir               common.BuildCacheFilesystem
	defDb                  *hash.DefinitionDatabase
	contextCache           sync.Map
	logger                 Logger
	tokenLocker            *tokenLocker
	rebuildUserDefinitions bool
}

// Filesystem implements common.Builder.
func (b *builder) Filesystem() common.BuildCacheFilesystem {
	return b.buildDir
}

// FileFromReference implements common.Builder.
func (b *builder) FileFromReference(ref config.DatabaseReference) (filesystem.File, error) {
	f, err := b.buildDir.FileFromReference(ref)
	if err != nil {
		return nil, fmt.Errorf("failed to get file from reference: %w", err)
	}

	return filesystem.NewSourceWrapper(f, nil), nil
}

// ImportAndValidate implements common.Builder.
func (b *builder) ImportAndValidate(def []byte) (common.BuildDefinition, error) {
	unmarshaled, err := b.defDb.UnmarshalDefinition(bytes.NewReader(def))
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal definition: %w", err)
	}

	buildDef, ok := unmarshaled.(common.BuildDefinition)
	if !ok {
		return nil, fmt.Errorf("definition %T is not a BuildDefinition", unmarshaled)
	}

	defHash, err := b.defDb.HashDefinition(buildDef)
	if err != nil {
		return nil, fmt.Errorf("failed to hash definition: %w", err)
	}

	defDir, err := b.buildDir.CreateBuildDirectory(defHash)
	if err != nil {
		return nil, err
	}

	writable, ok := defDir.(common.WritableBuildCacheDirectory)
	if !ok {
		return nil, fmt.Errorf("build directory is not writable")
	}

	if err := writable.WriteDefinition(def); err != nil {
		return nil, err
	}

	return buildDef, nil
}

// GetDefinitionByHash implements common.Builder.
func (b *builder) GetDefinitionByHash(hash hash.Hash) (common.BuildDefinition, error) {
	def, err := b.defDb.GetDefinitionByHash(hash)
	if err != nil {
		return nil, err
	}

	buildDef, ok := def.(common.BuildDefinition)
	if !ok {
		return nil, fmt.Errorf("definition %T is not a BuildDefinition", def)
	}

	return buildDef, nil
}

// MinimalContext implements common.Builder.
func (b *builder) MinimalContext() common.MinimalBuildContext {
	return &buildContext{
		builder:      b,
		recept:       &common.BuildReceipt{},
		logger:       b.logger.Child("minimal"),
		requirements: make(map[hash.Hash]struct{}),
	}
}

// SetRebuildUserDefinitions implements common.Builder.
func (b *builder) SetRebuildUserDefinitions(rebuild bool) {
	b.rebuildUserDefinitions = rebuild
}

func (b *builder) contextForDefinition(parent *buildContext, def common.BuildDefinition, opts common.BuildOptions) *buildContext {
	maybeCtx := &buildContext{
		parent:       parent,
		builder:      b,
		def:          def,
		requirements: make(map[hash.Hash]struct{}),
		options:      opts,
		token:        b.tokenLocker.New(),
		files:        make(map[string]*contextFile),
	}
	maybeCtx.wg.Add(1)

	c, loaded := b.contextCache.LoadOrStore(def, maybeCtx)
	ctx := c.(*buildContext)
	if !loaded {
		if parent != nil {
			ctx.logger = parent.logger.Child(def.String())
		} else {
			ctx.logger = b.logger.Child(def.String())
		}

		go func() {
			defer ctx.wg.Done()
			if err := ctx.ensureUpToDate(); err != nil {
				ctx.err = err
			}
		}()
	}

	return ctx
}

func (b *builder) cacheDefinitionHash(def common.BuildDefinition, stack []common.BuildDefinition) error {
	// Check for cycles.
	for _, d := range stack[:len(stack)-1] {
		if d == def {
			return fmt.Errorf("cycle detected: %s %+v", def.String(), stack)
		}
	}

	deps, err := def.Dependencies()
	if err != nil {
		return err
	}

	for _, dep := range deps {
		if err := b.cacheDefinitionHash(dep, append(stack, dep)); err != nil {
			return err
		}
	}

	if _, err := b.defDb.HashDefinition(def); err != nil {
		return err
	}

	return nil
}

// Build implements Builder.
func (b *builder) Build(def common.BuildDefinition, opts common.BuildOptions) (common.BuildArtifact, error) {
	if b.currentlyBuilding.CompareAndSwap(false, true) {
		if def == nil {
			return nil, fmt.Errorf("definition is nil")
		}

		if err := b.cacheDefinitionHash(def, []common.BuildDefinition{def}); err != nil {
			return nil, err
		}

		ctx := b.contextForDefinition(nil, def, opts)

		art, err := ctx.getArtifact()
		if err != nil {
			return nil, err
		}

		b.currentlyBuilding.Store(false)

		return art, nil
	} else {
		return nil, fmt.Errorf("a build is already in progress")
	}
}

func (b *builder) loadDefinition(hash hash.Hash) (io.ReadCloser, error) {
	dir, err := b.buildDir.GetBuildDirectory(hash)
	if err != nil {
		return nil, err
	}

	def, err := dir.ReadDefinition()
	if err != nil {
		return nil, err
	}

	return io.NopCloser(bytes.NewReader(def)), nil
}

func (b *builder) contextForHash(parent *buildContext, hash hash.Hash, opts common.BuildOptions) (*buildContext, error) {
	def, err := b.defDb.GetDefinitionByHash(hash)
	if err != nil {
		return nil, err
	}

	buildDef, ok := def.(common.BuildDefinition)
	if !ok {
		return nil, fmt.Errorf("definition %T is not a BuildDefinition", def)
	}

	return b.contextForDefinition(parent, buildDef, opts), nil
}

func (b *builder) receiptFromHash(hash hash.Hash) (*common.BuildReceipt, error) {
	dir, err := b.buildDir.GetBuildDirectory(hash)
	if err != nil {
		return nil, err
	}

	fakeCtx := &buildContext{buildDir: dir}

	return fakeCtx.loadRecept()
}

type garbageCollectorState struct {
	receipt    common.BuildReceipt
	references int
}

// GarbageCollect implements Builder.
func (b *builder) GarbageCollect(olderThan time.Time) ([]hash.Hash, error) {
	receipts := make(map[hash.Hash]*garbageCollectorState)

	hashes, err := b.buildDir.GetAllHashes()
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	// Populate the receipts map.
	for _, hash := range hashes {
		receipt, err := b.receiptFromHash(hash)
		if err != nil {
			log.Warn("failed to load receipt", "err", err)
			continue
		}

		receipts[hash] = &garbageCollectorState{
			receipt:    *receipt,
			references: 0,
		}
	}

	// Count the references.
	for _, state := range receipts {
		for _, hash := range state.receipt.Requirements {
			if _, ok := receipts[hash]; ok {
				receipts[hash].references++
			}
		}
	}

	// Look for any receipts with no references.
	var checkNext []hash.Hash
	for hash, state := range receipts {
		// Don't delete receipts that have references.
		if state.references > 0 {
			continue
		}

		// Don't delete receipts that were built after the cutoff time.
		if state.receipt.StartTime.After(olderThan) {
			continue
		}

		checkNext = append(checkNext, hash)
	}

	deleted := make(map[hash.Hash]bool)
	for len(checkNext) > 0 {
		// Pop the first element.
		hash := checkNext[0]
		checkNext = checkNext[1:]

		if receipts[hash].references > 0 {
			continue
		}

		deleted[hash] = true

		// reduce the reference count of each dependency.
		for _, depHash := range receipts[hash].receipt.Requirements {
			receipts[depHash].references--

			if receipts[depHash].receipt.StartTime.After(olderThan) {
				continue
			}

			checkNext = append(checkNext, depHash)
		}
	}

	var ret []hash.Hash
	for hash := range deleted {
		ret = append(ret, hash)
	}
	return ret, nil
}

var (
	_ common.Builder = &builder{}
)

func New(
	cache common.BuildCacheFilesystem,
	db common.PackageDatabase,
	maxJobs int,
	logger Logger,
) common.Builder {
	b := &builder{
		buildDir:    cache,
		database:    db,
		logger:      logger,
		tokenLocker: newTokenLocker(maxJobs),
	}

	b.defDb = hash.NewDefinitionDatabase(b.loadDefinition)

	return b
}
