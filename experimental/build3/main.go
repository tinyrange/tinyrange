package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
)

// Interfaces

const (
	definitionFileName = "definition.json"
	receptFileName     = "recept.json"
)

type Logger interface {
	Logf(format string, args ...interface{})
	Describe(format string, args ...interface{})
	Child(description string) Logger
}

type BuildOptions struct {
}

type BuildRecept struct {
	Requirements []hash.Hash `json:"requirements"`
}

type BuildArtifact interface {
}

type BuildContext interface {
	BuildChild(def BuildDefinition) (BuildArtifact, error)
}

type BuildDefinition interface {
	hash.Definition
	fmt.Stringer

	NeedsBuild(ctx BuildContext) (bool, error)
	Dependencies() ([]BuildDefinition, error)
	Build(ctx BuildContext) error
}

type Builder interface {
	Build(def BuildDefinition, opts BuildOptions) (BuildArtifact, error)
}

// Implementation

type buildArtifact struct {
	*buildContext
}

type buildContextState uint32

const (
	buildContextStateNew buildContextState = iota
	buildContextStateBuilding
	buildContextStateBuilt
)

type buildContext struct {
	builder      *builder
	parent       *buildContext
	hash         hash.Hash
	def          BuildDefinition
	buildDir     filesystem.MutableDirectory
	options      BuildOptions
	recept       *BuildRecept
	requirements map[hash.Hash]struct{}
	err          error
	state        buildContextState
	wg           sync.WaitGroup
	logger       Logger
	token        *token
}

// Precondition: The definition has to be rebuilt.
func (c *buildContext) build() error {
	defer c.token.Lock().Close()

	// Update the state.
	atomic.StoreUint32((*uint32)(&c.state), uint32(buildContextStateBuilding))
	c.logger.Describe("building %s", c.def.String())

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

	c.recept = &BuildRecept{}

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

	// Save the recept to the build directory.
	recept, err := json.Marshal(c.recept)
	if err != nil {
		return err
	}
	memFile = filesystem.NewMemoryFile(filesystem.TypeRegular)
	if err := memFile.Overwrite(recept); err != nil {
		return err
	}
	if _, err := c.buildDir.Create(receptFileName, memFile); err != nil {
		return err
	}

	// Update the state.
	atomic.StoreUint32((*uint32)(&c.state), uint32(buildContextStateBuilt))
	c.logger.Describe("built %s", c.def.String())

	return nil
}

func (c *buildContext) loadRecept() (*BuildRecept, error) {
	dh, err := c.buildDir.GetChild(receptFileName)
	if err != nil {
		// This error could not not-exist so we propagate it.
		return nil, err
	}

	f, err := dh.Open()
	if err != nil {
		return nil, err
	}

	var recept BuildRecept
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
			return err
		}
	}

	if c.buildDir == nil {
		// create a new build directory
		// If it already exists, it will be reused.
		c.buildDir, err = c.builder.buildDir.Mkdir(c.hash.String())
		if err != nil {
			return err
		}
	}

	if c.recept == nil {
		// load the recept
		c.recept, err = c.loadRecept()
		if errors.Is(err, fs.ErrNotExist) {
			return c.build()
		} else if err != nil {
			return err
		}
	}

	needsBuild, err := c.def.NeedsBuild(c)
	if err != nil {
		return err
	}
	if needsBuild {
		return c.build()
	}

	// TODO(joshua): Check the requirements.

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

func (c *buildContext) addDependency(def BuildDefinition) {
	// blindly add the dependency to trigger the build or load.
	// it will only turn into a requirement if it's used as a child.
	c.builder.contextForDefinition(c, def, BuildOptions{})
}

func (c *buildContext) addRequirement(hash hash.Hash) {
	if c.parent != nil {
		c.parent.addRequirement(hash)
	}

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

// Main

type basicBuildDefinitionParams struct {
	Name     string
	Children []BuildDefinition
}

func (p basicBuildDefinitionParams) SerializableType() string { return "basic" }

var (
	_ hash.SerializableValue = &basicBuildDefinitionParams{}
)

type basicBuildDefinition struct {
	params basicBuildDefinitionParams
}

// String implements BuildDefinition.
func (d *basicBuildDefinition) String() string {
	return d.params.Name
}

// Create implements BuildDefinition.
func (d *basicBuildDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &basicBuildDefinition{params: params.(basicBuildDefinitionParams)}
}

// Params implements BuildDefinition.
func (d *basicBuildDefinition) Params() hash.SerializableValue {
	return d.params
}

// SerializableType implements BuildDefinition.
func (d *basicBuildDefinition) SerializableType() string {
	return "basic"
}

// NeedsBuild implements BuildDefinition.
func (d *basicBuildDefinition) NeedsBuild(ctx BuildContext) (bool, error) {
	return false, nil
}

// Dependencies implements BuildDefinition.
func (d *basicBuildDefinition) Dependencies() ([]BuildDefinition, error) {
	return d.params.Children, nil
}

// Build implements BuildDefinition.
func (d *basicBuildDefinition) Build(ctx BuildContext) error {
	for _, child := range d.params.Children {
		if _, err := ctx.BuildChild(child); err != nil {
			return err
		}
	}

	return nil
}

var (
	_ BuildDefinition = &basicBuildDefinition{}
)

func newBasicBuildDefinition(name string, children ...BuildDefinition) *basicBuildDefinition {
	return &basicBuildDefinition{
		params: basicBuildDefinitionParams{
			Name:     name,
			Children: children,
		},
	}
}

type basicLogger struct {
	id string
}

// Logf implements Logger.
func (l *basicLogger) Logf(format string, args ...interface{}) {
	slog.Info("log", "id", l.id, "msg", fmt.Sprintf(format, args...))
}

// Describe implements Logger.
func (l *basicLogger) Describe(format string, args ...interface{}) {
	slog.Info("desc", "id", l.id, "msg", fmt.Sprintf(format, args...))
}

// Child implements Logger.
func (l *basicLogger) Child(description string) Logger {
	child := &basicLogger{id: uuid.NewString()}
	child.Describe(description)
	return child
}

var (
	_ Logger = &basicLogger{}
)

func graphToBuildDefinition(graph []Edge) (map[int]BuildDefinition, error) {
	defs := make(map[int]BuildDefinition)

	for _, edge := range graph {
		if _, ok := defs[edge.From]; !ok {
			defs[edge.From] = newBasicBuildDefinition(fmt.Sprintf("node%d", edge.From))
		}

		if _, ok := defs[edge.To]; !ok {
			defs[edge.To] = newBasicBuildDefinition(fmt.Sprintf("node%d", edge.To))
		}

		parent := defs[edge.From]
		child := defs[edge.To]

		if parent == child {
			continue
		}

		parent.(*basicBuildDefinition).params.Children = append(parent.(*basicBuildDefinition).params.Children, child)
	}

	return defs, nil
}

var (
	jobs = flag.Int("jobs", 1, "number of jobs to run in parallel")
)

func appMain() error {
	flag.Parse()

	hash.RegisterType(&basicBuildDefinition{})

	slog.Info("generating graph")

	graph, root, err := GenerateRandomDAG(25000, 25000)
	if err != nil {
		return err
	}

	slog.Info("converting graph to build definition")

	buildGraph, err := graphToBuildDefinition(graph)
	if err != nil {
		return err
	}

	slog.Info("generated graph")

	rootDef := buildGraph[root]

	buildDir := filesystem.NewMemoryDirectory()

	builder := New(buildDir, *jobs, &basicLogger{})

	if _, err := builder.Build(rootDef, BuildOptions{}); err != nil {
		return err
	}

	return nil
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
