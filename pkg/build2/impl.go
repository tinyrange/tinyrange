package build2

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	cryptoHash "hash"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"github.com/tinyrange/tinyrange/pkg/path"
	"github.com/tinyrange/tinyrange/pkg/star"
	"go.starlark.net/starlark"
)

const (
	definitionFileName = "definition.json"
	receiptFileName    = "receipt.json"
	outputPrefix       = "output."
	defaultSuffix      = "default"
)

type buildArtifact struct {
	*buildContext
}

// OpenFile implements BuildArtifact.
func (a *buildArtifact) OpenFile(name string) (filesystem.FileHandle, error) {
	if _, ok := a.recept.Files[name]; !ok {
		return nil, fmt.Errorf("file %s not found", name)
	}

	f, err := a.buildDir.GetChild(outputPrefix + name)
	if err != nil {
		return nil, err
	}

	return f.File.Open()
}

// Default implements BuildArtifact.
func (a *buildArtifact) Default() (filesystem.File, error) {
	if _, ok := a.recept.Files[defaultSuffix]; !ok {
		return nil, fmt.Errorf("file default not found")
	}

	f, err := a.buildDir.GetChild(outputPrefix + defaultSuffix)
	if err != nil {
		return nil, err
	}

	return filesystem.NewSourceWrapper(f.File, a.def), nil
}

// Receipt implements BuildArtifact.
func (a *buildArtifact) Receipt() common.BuildReceipt {
	return *a.recept
}

var (
	_ common.BuildArtifact = &buildArtifact{}
)

type contextFile struct {
	writer io.WriteCloser
	multi  io.Writer
	hash   cryptoHash.Hash
	file   filesystem.MutableFile
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

	slog.Debug("executing VMM", "args", cmd.Args)

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
	buildDir     filesystem.MutableDirectory
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

// PrenotifyChildren implements common.BuildContext.
func (c *buildContext) PrenotifyChildren(children []common.BuildDefinition) error {
	for _, child := range children {
		c.builder.contextForDefinition(c, child, common.BuildOptions{})
	}

	return nil
}

// DigestFromFile implements common.BuildContext.
func (c *buildContext) DigestFromFile(file filesystem.File) (*filesystem.FileDigest, error) {
	filename, err := filesystem.GetHostFilename(file)
	if err != nil {
		return nil, err
	}

	return &filesystem.FileDigest{Hash: filename}, nil
}

// FileFromDigest implements common.BuildContext.
func (c *buildContext) FileFromDigest(digest *filesystem.FileDigest) (filesystem.File, error) {
	if digest.Hash != "" {
		return filesystem.NewLocalFile(digest.Hash, nil), nil
	}

	return nil, fmt.Errorf("could not convert digest to hash")
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

	configFilename, err := filesystem.GetHostFilename(out.(*contextFile).file)
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

	buildDirPath, err := filesystem.GetHostFilename(c.builder.buildDir)
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
func (c *buildContext) build() error {
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
	memFile := filesystem.NewMemoryFile(filesystem.TypeRegular)
	if err := memFile.Overwrite(def); err != nil {
		return err
	}
	if _, err := c.buildDir.Create(definitionFileName, memFile); err != nil {
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
		return err
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
	memFile = filesystem.NewMemoryFile(filesystem.TypeRegular)
	if err := memFile.Overwrite(recept); err != nil {
		return err
	}
	if _, err := c.buildDir.Create(receiptFileName, memFile); err != nil {
		return err
	}

	// Update the state.
	atomic.StoreUint32((*uint32)(&c.state), uint32(buildContextStateBuilt))
	c.logger.Describe(ColorGreen, "built successfully in %s", c.recept.Duration)

	return nil
}

func (c *buildContext) loadRecept() (*common.BuildReceipt, error) {
	dh, err := c.buildDir.GetChild(receiptFileName)
	if err != nil {
		// This error could not not-exist so we propagate it.
		return nil, err
	}

	f, err := dh.Open()
	if err != nil {
		return nil, err
	}

	var recept common.BuildReceipt
	if err := json.NewDecoder(f).Decode(&recept); err != nil {
		return nil, err
	}

	return &recept, nil
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
		// take the first byte of the hash as the directory name
		buildDirTop, err := c.builder.buildDir.Mkdir(c.hash.String()[:2])
		if err != nil {
			return fmt.Errorf("failed to create top build directory: %w", err)
		}

		// create a new build directory
		// If it already exists, it will be reused.
		c.buildDir, err = buildDirTop.Mkdir(c.hash.String()[2:])
		if err != nil {
			return fmt.Errorf("failed to create build directory: %w", err)
		}
	}

	if c.options.AlwaysRebuild {
		// force a rebuild
		defer c.token.Lock().Close()

		return c.build()
	}

	if c.recept == nil {
		// load the recept
		c.recept, err = c.loadRecept()
		if errors.Is(err, fs.ErrNotExist) || err == io.EOF {
			defer c.token.Lock().Close()

			return c.build()
		} else if err != nil {
			return fmt.Errorf("failed to load recept: %w", err)
		}
	}

	needsBuild, err := c.def.NeedsBuild(c)
	if err != nil {
		return fmt.Errorf("failed to check if build is needed: %w", err)
	}
	if needsBuild {
		c.logger.Describe(ColorYellow, "rebuilding due to user NeedsBuild")

		defer c.token.Lock().Close()

		return c.build()
	}

	// Lock a token since we might be triggering rebuilds of children so we need to ensure we have a token.
	defer c.token.Lock().Close()

	// Check all requirements in parallel.
	for _, req := range c.recept.Requirements {
		child, err := c.builder.contextForHash(c, req, common.BuildOptions{})
		if err != nil {
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
			return c.build()
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

	// Check if the file already exists.
	if _, ok := c.files[name]; ok {
		return nil, fmt.Errorf("file %s already exists", name)
	}

	// Create the file.
	f, err := c.buildDir.Create(outputPrefix+name, nil)
	if err != nil {
		return nil, err
	}

	mut, ok := f.(filesystem.MutableFile)
	if !ok {
		return nil, fmt.Errorf("file %T is not mutable", f)
	}

	mutHandle, err := mut.OpenMut()
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
		file:   mut,
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
	buildDir               filesystem.MutableDirectory
	defDb                  *hash.DefinitionDatabase
	contextCache           sync.Map
	logger                 Logger
	tokenLocker            *tokenLocker
	rebuildUserDefinitions bool
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
	dirFileTop, err := b.buildDir.GetChild(hash.String()[:2])
	if err != nil {
		return nil, err
	}

	dir, ok := dirFileTop.File.(filesystem.MutableDirectory)
	if !ok {
		return nil, fmt.Errorf("file is not a directory: %T", dirFileTop)
	}

	dirFile, err := dir.GetChild(hash.String()[2:])
	if err != nil {
		return nil, err
	}

	dir, ok = dirFile.File.(filesystem.MutableDirectory)
	if !ok {
		return nil, fmt.Errorf("file is not a directory: %T", dirFile)
	}

	f, err := dir.GetChild(definitionFileName)
	if err != nil {
		return nil, err
	}

	return f.File.Open()
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
	dirFileTop, err := b.buildDir.GetChild(hash.String()[:2])
	if err != nil {
		return nil, err
	}

	dir, ok := dirFileTop.File.(filesystem.MutableDirectory)
	if !ok {
		return nil, fmt.Errorf("file is not a directory: %T", dirFileTop)
	}

	dirFile, err := dir.GetChild(hash.String()[2:])
	if err != nil {
		return nil, err
	}

	dir, ok = dirFile.File.(filesystem.MutableDirectory)
	if !ok {
		return nil, fmt.Errorf("file is not a directory: %T", dirFile)
	}

	fakeCtx := &buildContext{buildDir: dir}

	return fakeCtx.loadRecept()
}

var (
	validByte   = regexp.MustCompile(`^[0-9a-f]{2}$`)
	validSha256 = regexp.MustCompile(`^[0-9a-f]{62}$`)
)

type garbageCollectorState struct {
	receipt    common.BuildReceipt
	references int
}

// GarbageCollect implements Builder.
func (b *builder) GarbageCollect(olderThan time.Time) ([]hash.Hash, error) {
	ents, err := b.buildDir.Readdir()
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	receipts := make(map[hash.Hash]*garbageCollectorState)

	// Populate the receipts map.
	for _, ent := range ents {
		if !validByte.MatchString(ent.Name) {
			continue
		}

		childDir, ok := ent.File.(filesystem.Directory)
		if !ok {
			continue
		}

		childEnts, err := childDir.Readdir()
		if err != nil {
			slog.Warn("failed to read directory", "err", err)
			continue
		}

		for _, childEnt := range childEnts {
			receipt, err := b.receiptFromHash(hash.Hash(ent.Name + childEnt.Name))
			if err != nil {
				slog.Warn("failed to load receipt", "err", err)
				continue
			}

			receipts[hash.Hash(ent.Name)] = &garbageCollectorState{
				receipt:    *receipt,
				references: 0,
			}
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

func New(buildDir filesystem.MutableDirectory, db common.PackageDatabase, maxJobs int, logger Logger) common.Builder {
	b := &builder{
		buildDir:    buildDir,
		database:    db,
		logger:      logger,
		tokenLocker: newTokenLocker(maxJobs),
	}

	b.defDb = hash.NewDefinitionDatabase(b.loadDefinition)

	return b
}
