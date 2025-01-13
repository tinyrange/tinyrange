package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	cryptoHash "hash"
	"io"
	"io/fs"
	"log/slog"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
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

// Receipt implements BuildArtifact.
func (a *buildArtifact) Receipt() BuildReceipt {
	return *a.recept
}

var (
	_ BuildArtifact = &buildArtifact{}
)

type contextFile struct {
	writer io.WriteCloser
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

type buildContext struct {
	builder      *builder
	parent       *buildContext
	hash         hash.Hash
	def          BuildDefinition
	buildDir     filesystem.MutableDirectory
	options      BuildOptions
	recept       *BuildReceipt
	requirements map[hash.Hash]struct{}
	err          error
	state        buildContextState
	wg           sync.WaitGroup
	logger       Logger
	token        *token
	files        map[string]*contextFile
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

	c.recept = &BuildReceipt{
		StartTime: time.Now(),
		Files:     make(map[string]string),
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
	if err := c.def.Build(c); err != nil {
		return err
	}

	c.recept.Duration = time.Since(c.recept.StartTime)

	// Set all the hashes in the recept.
	for name, file := range c.files {
		c.recept.Files[name] = fmt.Sprintf("%x", file.hash.Sum(nil))
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

func (c *buildContext) loadRecept() (*BuildReceipt, error) {
	dh, err := c.buildDir.GetChild(receiptFileName)
	if err != nil {
		// This error could not not-exist so we propagate it.
		return nil, err
	}

	f, err := dh.Open()
	if err != nil {
		return nil, err
	}

	var recept BuildReceipt
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
		// create a new build directory
		// If it already exists, it will be reused.
		c.buildDir, err = c.builder.buildDir.Mkdir(c.hash.String())
		if err != nil {
			return fmt.Errorf("failed to create build directory: %w", err)
		}
	}

	if c.options.ForceRebuild {
		// force a rebuild
		defer c.token.Lock().Close()

		return c.build()
	}

	if c.recept == nil {
		// load the recept
		c.recept, err = c.loadRecept()
		if errors.Is(err, fs.ErrNotExist) {
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
		child, err := c.builder.contextForHash(c, req, BuildOptions{})
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

func (c *buildContext) addDependency(def BuildDefinition) {
	// blindly add the dependency to trigger the build or load.
	// it will only turn into a requirement if it's used as a child.
	c.builder.contextForDefinition(c, def, BuildOptions{})
}

func (c *buildContext) addRequirement(hash hash.Hash) {
	if _, ok := c.requirements[hash]; ok {
		return
	}
	c.requirements[hash] = struct{}{}
	c.recept.Requirements = append(c.recept.Requirements, hash)
}

func (c *buildContext) BuildChild(def BuildDefinition) (BuildArtifact, error) {
	child := c.builder.contextForDefinition(c, def, BuildOptions{})

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
	}

	return c.files[name], nil
}

// Hash implements BuildContext.
func (a *buildContext) Hash() hash.Hash {
	return a.hash
}

var (
	_ BuildContext = &buildContext{}
)

type builder struct {
	buildDir     filesystem.MutableDirectory
	defDb        *hash.DefinitionDatabase
	contextCache sync.Map
	logger       Logger
	tokenLocker  *tokenLocker
}

func (b *builder) contextForDefinition(parent *buildContext, def BuildDefinition, opts BuildOptions) *buildContext {
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

func (b *builder) cacheDefinitionHash(def BuildDefinition) error {
	deps, err := def.Dependencies()
	if err != nil {
		return err
	}

	for _, dep := range deps {
		if err := b.cacheDefinitionHash(dep); err != nil {
			return err
		}
	}

	if _, err := b.defDb.HashDefinition(def); err != nil {
		return err
	}

	return nil
}

// Build implements Builder.
func (b *builder) Build(def BuildDefinition, opts BuildOptions) (BuildArtifact, error) {
	if def == nil {
		return nil, fmt.Errorf("definition is nil")
	}

	if err := b.cacheDefinitionHash(def); err != nil {
		return nil, err
	}

	ctx := b.contextForDefinition(nil, def, opts)

	return ctx.getArtifact()
}

func (b *builder) loadDefinition(hash hash.Hash) (io.ReadCloser, error) {
	return nil, fmt.Errorf("loadDefinition not implemented")
}

func (b *builder) contextForHash(parent *buildContext, hash hash.Hash, opts BuildOptions) (*buildContext, error) {
	def, err := b.defDb.GetDefinitionByHash(hash)
	if err != nil {
		return nil, err
	}

	buildDef, ok := def.(BuildDefinition)
	if !ok {
		return nil, fmt.Errorf("definition %T is not a BuildDefinition", def)
	}

	return b.contextForDefinition(parent, buildDef, opts), nil
}

func (b *builder) receiptFromHash(hash hash.Hash) (*BuildReceipt, error) {
	hashDir, err := b.buildDir.GetChild(hash.String())
	if err != nil {
		return nil, err
	}

	mutDir, ok := hashDir.File.(filesystem.MutableDirectory)
	if !ok {
		return nil, fmt.Errorf("directory is not mutable: %T", hashDir)
	}

	fakeCtx := &buildContext{buildDir: mutDir}

	return fakeCtx.loadRecept()
}

var validSha256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

type garbageCollectorState struct {
	receipt    BuildReceipt
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
		if !validSha256.MatchString(ent.Name) {
			continue
		}

		receipt, err := b.receiptFromHash(hash.Hash(ent.Name))
		if err != nil {
			slog.Warn("failed to load receipt", "err", err)
			continue
		}

		receipts[hash.Hash(ent.Name)] = &garbageCollectorState{
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
	_ Builder = &builder{}
)

func New(buildDir filesystem.MutableDirectory, maxJobs int, logger Logger) Builder {
	b := &builder{
		buildDir:    buildDir,
		logger:      logger,
		tokenLocker: newTokenLocker(maxJobs),
	}

	b.defDb = hash.NewDefinitionDatabase(b.loadDefinition)

	return b
}
