package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	cryptoHash "hash"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tinyrange/tinyrange/pkg/buildinfo"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
)

// Glossary:
// - BuildDefinition: A definition of a build that can be executed by a Builder.
// - BuildReceipt: A receipt that describes the build process that was used to create a BuildArtifact.
// - BuildArtifact: An artifact that was created by a Builder.
// - Builder: An interface that can build a BuildDefinition.
// - BuildOutputWriter: An interface that can write the output of a build.
// - BuildContext: A context in which a build can be executed.

// A valid output name is alphanumeric with underscores.
var VALID_OUTPUT_NAME = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

const (
	DEFINITION_FILENAME = "definition.json"
	RECEIPT_FILENAME    = "receipt.json"
	LOCK_FILENAME       = "lock.pid"
	OUTPUT_PREFIX       = "output."
)

func readJSONFromFile(file filesystem.File, v any) error {
	r, err := file.Open()
	if err != nil {
		return err
	}
	defer r.Close()

	return json.NewDecoder(r).Decode(v)
}

func readFile(file filesystem.File) ([]byte, error) {
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()

	return io.ReadAll(r)
}

func writeFile(dir filesystem.MutableDirectory, name string, contents []byte) error {
	file, err := dir.Create(name, nil)
	if err != nil {
		return err
	}
	if file == nil {
		return fs.ErrExist
	}

	mutFile, ok := file.(filesystem.MutableFile)
	if !ok {
		return fmt.Errorf("file is not mutable: %T dir=%T", file, dir)
	}

	return mutFile.Overwrite(contents)
}

type DependencyInfo struct {
	// Hash is the SHA-256 hash of the dependency.
	Hash string `json:"hash"`
	// UsedCache is true if the dependency was loaded from the cache.
	UsedCache bool `json:"used_cache"`
	// Explicit is true if the dependency was explicitly added.
	Explicit bool `json:"explicit"`
}

type BuildReceipt struct {
	// Version is the version of TinyRange that built the artifact.
	Version string `json:"version"`
	// Redistributable is true if the artifact can be redistributed.
	Redistributable bool `json:"redistributable"`
	// Non-zero if the build should unconditionally be rebuilt after this time.
	ExpireTime time.Time `json:"expire_time"`
	// Dependencies is a list of dependencies that were used to build the artifact.
	Dependencies []*DependencyInfo `json:"dependencies"`
	// BuiltFor is the parent definition that the artifact was built for.
	BuiltFor string `json:"built_for"`
	// OutputHashes is a map of output names to their SHA-256 hashes.
	OutputHashes map[string]string `json:"output_hashes"`
	// BuildTime is the time the artifact was built.
	BuildTime time.Time `json:"build_time"`
	// BuildDuration is the duration of the build.
	BuildDuration time.Duration `json:"build_duration"`
}

type BuildOptions struct {
	ForceRebuild   bool
	DependencyInfo *DependencyInfo
}

type Builder interface {
	// Build builds a BuildDefinition returning a build artifact.
	Build(def BuildDefinition, options BuildOptions) (BuildArtifact, error)

	// BuildChild builds a BuildDefinition as a child of a parent BuildContext.
	BuildChild(parent BuildContext, def BuildDefinition, options BuildOptions) (BuildArtifact, error)

	// HashDefinition hashes a BuildDefinition.
	HashDefinition(def BuildDefinition) (string, error)

	// SerializeDefinition serializes a BuildDefinition.
	MarshalDefinition(def BuildDefinition) ([]byte, error)

	// ArtifactFromHash gets a BuildArtifact from a hash.
	ArtifactFromHash(hash string) (BuildArtifact, error)

	// DefinitionFromArtifact gets a BuildDefinition from a BuildArtifact.
	DefinitionFromArtifact(artifact BuildArtifact) (BuildDefinition, error)

	// GarbageCollect returns a list of garbage collectable hashes.
	GarbageCollect(olderThan time.Time) ([]string, error)
}

type buildInfo struct {
	mtx sync.Mutex
	ctx *buildContext
	wg  sync.WaitGroup
}

type builder struct {
	mtx            sync.Mutex
	buildDirectory filesystem.MutableDirectory
	hashDb         *hash.DefinitionDatabase
	buildCache     map[string]*buildInfo
}

// needsRebuild checks if a BuildDefinition needs to be rebuilt.
// returns the BuildArtifact if it does not need to be rebuilt.
func (b *builder) needsRebuild(hash string, def BuildDefinition, options BuildOptions) (BuildArtifact, error) {
	if options.ForceRebuild {
		return nil, nil
	}

	// returns true if the hash needs to be rebuilt.
	var checkHash func(string) (bool, error)

	checkHash = func(hash string) (bool, error) {
		art, err := b.ArtifactFromHash(hash)
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		} else if err != nil {
			return true, fmt.Errorf("failed to get artifact from hash: %w", err)
		}

		// Check if the receipt has expired.
		if art.Receipt().ExpireTime.Before(time.Now()) {
			return true, nil
		}

		// Check if any dependencies need to be rebuilt.
		for _, dep := range art.Receipt().Dependencies {
			ok, err := checkHash(dep.Hash)
			if err != nil {
				return true, fmt.Errorf("failed to check dependency hash: %w", err)
			}
			if ok {
				return true, nil
			}
		}

		return false, nil
	}

	needsRebuild, err := checkHash(hash)
	if err != nil {
		return nil, fmt.Errorf("failed to check hash: %w", err)
	} else if !needsRebuild {
		return nil, nil
	}

	art, err := b.ArtifactFromHash(hash)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get artifact from hash: %w", err)
	}

	return art, nil
}

// Build implements Builder.
func (b *builder) Build(def BuildDefinition, options BuildOptions) (BuildArtifact, error) {
	return b.BuildChild(nil, def, options)
}

func (b *builder) getOrCreateBuild(hash string) (*buildInfo, bool, error) {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	if info, ok := b.buildCache[hash]; ok {
		return info, false, nil
	}

	info := &buildInfo{
		ctx: nil,
	}
	info.wg.Add(1)

	b.buildCache[hash] = info

	return info, true, nil
}

// BuildChild implements Builder.
func (b *builder) BuildChild(parent BuildContext, def BuildDefinition, options BuildOptions) (BuildArtifact, error) {
	parentHash := ""
	if parent != nil {
		parentHash = parent.Hash()
	}

	// Hash the definition.
	hash, err := b.HashDefinition(def)
	if err != nil {
		return nil, fmt.Errorf("failed to hash definition: %w", err)
	}

	buildInfo, locked, err := b.getOrCreateBuild(hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get or wait for build: %w", err)
	}
	if !locked {
		// Wait for the build to complete.
		buildInfo.wg.Wait()

		if buildInfo.ctx != nil {
			if options.DependencyInfo != nil {
				options.DependencyInfo.UsedCache = true
			}

			return buildInfo.ctx.artifact, nil
		} else {
			return nil, fmt.Errorf("failed to get build: %s", hash)
		}
	}
	defer buildInfo.wg.Done()

	// Check if the definition needs to be rebuilt.
	art, err := b.needsRebuild(hash, def, options)
	if err != nil {
		return nil, err
	} else if art != nil {
		// The artifact does not need to be rebuilt.
		hash, err := b.HashDefinition(def)
		if err != nil {
			return nil, fmt.Errorf("failed to hash definition: %w", err)
		}

		slog.Debug("skipping build", "hash", hash[:8], "parent", parentHash[:8])

		if options.DependencyInfo != nil {
			options.DependencyInfo.UsedCache = true
		}

		return art, nil
	}

	// Create a child directory for the build.
	childDir, err := b.buildDirectory.Mkdir(hash)
	if err != nil {
		return nil, fmt.Errorf("failed to create build directory: %w", err)
	}

	// Create a new build context.
	buildInfo.ctx = &buildContext{
		builder:        b,
		parent:         parent,
		hash:           hash,
		buildDirectory: childDir,
		def:            def,
		receipt: BuildReceipt{
			Version:      buildinfo.VERSION,
			BuiltFor:     parentHash,
			OutputHashes: make(map[string]string),
		},
		outputs: make(map[string]*buildOutputWriter),
	}

	// Build the definition.
	art, err = buildInfo.ctx.build()
	if err != nil {
		return nil, fmt.Errorf("failed to build: %w", err)
	}
	if art == nil {
		slog.Debug("returning existing build", "hash", hash[:8])

		if options.DependencyInfo != nil {
			options.DependencyInfo.UsedCache = true
		}

		return b.ArtifactFromHash(hash)
	}
	return art, nil
}

// HashDefinition implements Builder.
func (b *builder) HashDefinition(def BuildDefinition) (string, error) {
	return b.hashDb.HashDefinition(def)
}

// SerializeDefinition implements Builder.
func (b *builder) MarshalDefinition(def BuildDefinition) ([]byte, error) {
	return b.hashDb.MarshalDefinition(def)
}

var VALID_SHA256 = regexp.MustCompile(`^[a-f0-9]{64}$`)

// ArtifactFromHash implements Builder.
func (b *builder) ArtifactFromHash(hash string) (BuildArtifact, error) {
	// validate the hash
	if !VALID_SHA256.MatchString(hash) {
		return nil, fmt.Errorf("invalid hash: %s", hash)
	}

	child, err := b.buildDirectory.GetChild(hash)
	if err != nil {
		return nil, err
	}

	childDir, ok := child.File.(filesystem.Directory)
	if !ok {
		return nil, fmt.Errorf("child is not a directory: %T", child)
	}

	receiptFile, err := childDir.GetChild(RECEIPT_FILENAME)
	if err != nil {
		return nil, err
	}

	var receipt BuildReceipt
	if err := readJSONFromFile(receiptFile.File, &receipt); err != nil {
		return nil, err
	}

	return &buildArtifact{
		hash:     hash,
		receipt:  receipt,
		buildDir: childDir,
	}, nil
}

// DefinitionFromArtifact implements Builder.
func (b *builder) DefinitionFromArtifact(artifact BuildArtifact) (BuildDefinition, error) {
	rawDef, err := artifact.RawDefinition()
	if err != nil {
		return nil, err
	}

	def, err := b.hashDb.UnmarshalDefinition(rawDef)
	if err != nil {
		return nil, err
	}

	return def.(BuildDefinition), nil
}

type garbageCollectorState struct {
	receipt    BuildReceipt
	references int
}

// GarbageCollect implements Builder.
func (b *builder) GarbageCollect(olderThan time.Time) ([]string, error) {
	ents, err := b.buildDirectory.Readdir()
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	receipts := make(map[string]*garbageCollectorState)

	// Populate the receipts map.
	for _, ent := range ents {
		if !VALID_SHA256.MatchString(ent.Name) {
			continue
		}

		art, err := b.ArtifactFromHash(ent.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get artifact from hash: %w", err)
		}

		receipts[ent.Name] = &garbageCollectorState{
			receipt:    art.Receipt(),
			references: 0,
		}
	}

	// Count the references.
	for _, state := range receipts {
		for _, dep := range state.receipt.Dependencies {
			hash := dep.Hash
			if _, ok := receipts[hash]; ok {
				receipts[hash].references++
			}
		}
	}

	// Look for any receipts with no references.
	var checkNext []string
	for hash, state := range receipts {
		// Don't delete receipts that have references.
		if state.references > 0 {
			continue
		}

		// Don't delete receipts that were built after the cutoff time.
		if state.receipt.BuildTime.After(olderThan) {
			continue
		}

		checkNext = append(checkNext, hash)
	}

	deleted := make(map[string]bool)
	for len(checkNext) > 0 {
		// Pop the first element.
		hash := checkNext[0]
		checkNext = checkNext[1:]

		if receipts[hash].references > 0 {
			continue
		}

		deleted[hash] = true

		// reduce the reference count of each dependency.
		for _, dep := range receipts[hash].receipt.Dependencies {
			depHash := dep.Hash

			receipts[depHash].references--

			if receipts[depHash].receipt.BuildTime.After(olderThan) {
				continue
			}

			checkNext = append(checkNext, depHash)
		}
	}

	var ret []string
	for hash := range deleted {
		ret = append(ret, hash)
	}
	return ret, nil
}

func (b *builder) hashCacheMiss(hash string) (io.ReadCloser, error) {
	art, err := b.ArtifactFromHash(hash)
	if err != nil {
		return nil, err
	}

	return art.RawDefinition()
}

var (
	_ Builder = &builder{}
)

func NewBuilder(buildDirectory filesystem.MutableDirectory) Builder {
	b := &builder{
		buildDirectory: buildDirectory,
		buildCache:     make(map[string]*buildInfo),
	}
	b.hashDb = hash.NewDefinitionDatabase(b.hashCacheMiss)
	return b
}

type BuildArtifact interface {
	// Hash returns the SHA-256 hash of the artifact.
	Hash() string

	// Receipt returns the BuildReceipt for the artifact.
	Receipt() BuildReceipt

	// RawDefinition returns the raw definition for the artifact.
	RawDefinition() (io.ReadCloser, error)

	// GetOutput returns an output file by name.
	GetOutput(name string) (filesystem.File, error)

	// ListOutputs returns a list of output names.
	ListOutputs() ([]string, error)
}

type buildArtifact struct {
	hash     string
	receipt  BuildReceipt
	buildDir filesystem.Directory
}

// Hash implements BuildArtifact.
func (b *buildArtifact) Hash() string {
	return b.hash
}

// Receipt implements BuildArtifact.
func (b *buildArtifact) Receipt() BuildReceipt {
	return b.receipt
}

// RawDefinition implements BuildArtifact.
func (b *buildArtifact) RawDefinition() (io.ReadCloser, error) {
	defFile, err := b.buildDir.GetChild(DEFINITION_FILENAME)
	if err != nil {
		return nil, err
	}

	return defFile.File.Open()
}

// GetOutput implements BuildArtifact.
func (b *buildArtifact) GetOutput(name string) (filesystem.File, error) {
	child, err := b.buildDir.GetChild(OUTPUT_PREFIX + name)
	if err != nil {
		return nil, err
	}

	return child.File, nil
}

// ListOutputs implements BuildArtifact.
func (b *buildArtifact) ListOutputs() ([]string, error) {
	ents, err := b.buildDir.Readdir()
	if err != nil {
		return nil, err
	}

	var outputs []string
	for _, ent := range ents {
		if strings.HasPrefix(ent.Name, OUTPUT_PREFIX) {
			outputs = append(outputs, ent.Name[len(OUTPUT_PREFIX):])
		}
	}
	return outputs, nil
}

var (
	_ BuildArtifact = &buildArtifact{}
)

type BuildOutputWriter interface {
	io.WriteCloser
}

type buildOutputWriter struct {
	hasher     cryptoHash.Hash
	fileHandle filesystem.WritableFileHandle
}

// Close implements BuildOutputWriter.
func (b *buildOutputWriter) Close() error {
	return b.fileHandle.Close()
}

// Write implements BuildOutputWriter.
func (b *buildOutputWriter) Write(p []byte) (n int, err error) {
	return io.MultiWriter(b.hasher, b.fileHandle).Write(p)
}

var (
	_ BuildOutputWriter = &buildOutputWriter{}
)

type BuildContext interface {
	Hash() string
	BuildChild(def BuildDefinition, opts BuildOptions) (BuildArtifact, error)
	CreateOutput(name string) (BuildOutputWriter, error)
	SetRedistributable(redistributable bool)
	SetExpireTime(expireTime time.Time)
}

type buildContext struct {
	mtx            sync.Mutex
	builder        Builder
	parent         BuildContext
	hash           string
	buildDirectory filesystem.MutableDirectory
	def            BuildDefinition
	receipt        BuildReceipt
	outputs        map[string]*buildOutputWriter
	artifact       BuildArtifact
}

// Hash implements BuildContext.
func (b *buildContext) Hash() string { return b.hash }

// SetRedistributable implements BuildContext.
func (b *buildContext) SetRedistributable(redistributable bool) {
	b.receipt.Redistributable = redistributable
}

// SetExpireTime implements BuildContext.
func (b *buildContext) SetExpireTime(expireTime time.Time) {
	b.receipt.ExpireTime = expireTime
}

func (b *buildContext) addDependency(def BuildDefinition, explicit bool) (*DependencyInfo, error) {
	hash, err := b.builder.HashDefinition(def)
	if err != nil {
		return nil, fmt.Errorf("failed to hash definition: %w", err)
	}

	if ok := slices.ContainsFunc(b.receipt.Dependencies, func(dep *DependencyInfo) bool {
		return dep.Hash == hash
	}); ok {
		return nil, nil
	}

	depInfo := &DependencyInfo{
		Hash:     hash,
		Explicit: explicit,
	}

	b.receipt.Dependencies = append(b.receipt.Dependencies, depInfo)

	return depInfo, nil
}

// BuildChild implements BuildContext.
func (b *buildContext) BuildChild(def BuildDefinition, opts BuildOptions) (BuildArtifact, error) {
	depInfo, err := b.addDependency(def, false)
	if err != nil {
		return nil, err
	}

	opts.DependencyInfo = depInfo

	return b.builder.BuildChild(b, def, opts)
}

// CreateOutput implements BuildContext.
func (b *buildContext) CreateOutput(name string) (BuildOutputWriter, error) {
	// Validate the output name.
	if !VALID_OUTPUT_NAME.MatchString(name) {
		return nil, fmt.Errorf("invalid output name: %s", name)
	}

	// Check if the output already exists.
	if _, ok := b.outputs[name]; ok {
		return nil, fmt.Errorf("output already exists: %s", name)
	}

	// Create the output file.
	file, err := b.buildDirectory.Create("output."+name, nil)
	if err != nil {
		return nil, err
	}

	// Open the file for writing.
	fh, err := file.Open()
	if err != nil {
		return nil, err
	}

	// Ensure the file handle is writable.
	fhMut, ok := fh.(filesystem.WritableFileHandle)
	if !ok {
		return nil, fmt.Errorf("file handle is not writable: %T", fh)
	}

	hasher := sha256.New()

	writer := &buildOutputWriter{
		hasher:     hasher,
		fileHandle: fhMut,
	}

	b.outputs[name] = writer

	return writer, nil
}

func writeLockFile(hash string, buildDir filesystem.MutableDirectory) (bool, error) {
	// write the lock file.
	pid := os.Getpid()
	pidStr := strconv.Itoa(pid)

	if err := writeFile(buildDir, LOCK_FILENAME, []byte(pidStr)); err != nil {
		return false, fmt.Errorf("failed to write lock file: %w", err)
	}

	// double check that we own the lock file that was written.
	lockFile, err := buildDir.GetChild(LOCK_FILENAME)
	if err != nil {
		return false, err
	}

	pidBytes, err := readFile(lockFile.File)
	if err != nil {
		return false, err
	}

	if string(pidBytes) != pidStr {
		pid, err := strconv.Atoi(string(pidBytes))
		if err != nil {
			return false, err
		}

		return waitForLockFile(hash, buildDir, pid)
	}

	return true, nil
}

func waitForLockFile(hash string, buildDir filesystem.MutableDirectory, pid int) (bool, error) {
	// wait for the lock to be released.
	slog.Info("waiting for lock release", "hash", hash[:8])

	for {
		time.Sleep(100 * time.Millisecond)
		// slog.Info("wait")

		// check to see if the lock file still exists.
		_, err := buildDir.GetChild(LOCK_FILENAME)
		if err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}

		// if the process crashed, we need to rebuild.
		if running, _ := ProcessRunning(pid); !running {
			slog.Warn("process crashed", "pid", pid)

			return writeLockFile(hash, buildDir)
		} else {
			// the lock file was deleted and the process is still
			// running so we assume it's built successfully.
			return false, nil
		}
	}
}

// returns true if we own the lock file and need to build.
func exclusiveLockForBuild(hash string, buildDir filesystem.MutableDirectory) (bool, error) {
	// check if the lock file exists.
	lockFile, err := buildDir.GetChild(LOCK_FILENAME)
	if errors.Is(err, fs.ErrNotExist) {
		return writeLockFile(hash, buildDir)
	} else if err != nil {
		return false, err
	}

	// read the lock file.
	pidBytes, err := readFile(lockFile.File)
	if err != nil {
		return false, err
	}

	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		return false, err
	}

	// check if the process is still running.
	if running, _ := ProcessRunning(pid); !running {
		slog.Warn("process crashed", "pid", pid)

		return writeLockFile(hash, buildDir)
	} else {
		return waitForLockFile(hash, buildDir, pid)
	}
}

func (b *buildContext) build() (BuildArtifact, error) {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	if b.artifact != nil {
		return b.artifact, nil
	}

	start := time.Now()

	// check if the lock file exists.
	ok, err := exclusiveLockForBuild(b.hash, b.buildDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for lock release: %w", err)
	}
	if !ok {
		// the build is already in progress.
		// slog.Info("skipping build due to lock", "hash", b.hash[:8])
		return nil, nil
	}
	defer b.buildDirectory.Unlink(LOCK_FILENAME)

	slog.Info("building", "hash", b.hash[:8])

	// serialize the definition to a file.
	contents, err := b.builder.MarshalDefinition(b.def)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize definition: %w", err)
	}

	if err := writeFile(b.buildDirectory, DEFINITION_FILENAME, contents); err != nil {
		return nil, fmt.Errorf("failed to write definition: %w", err)
	}

	// start builds for any dependencies.
	deps, err := b.def.Dependencies()
	if err != nil {
		return nil, fmt.Errorf("failed to get dependencies: %w", err)
	}

	errChan := make(chan error)
	doneChan := make(chan struct{})

	wg := sync.WaitGroup{}

	for _, dep := range deps {
		wg.Add(1)

		depInfo, err := b.addDependency(dep, true)
		if err != nil {
			return nil, err
		}

		go func(dep BuildDefinition, depInfo *DependencyInfo) {
			defer wg.Done()

			if _, err := b.builder.BuildChild(b, dep, BuildOptions{
				DependencyInfo: depInfo,
			}); err != nil {
				errChan <- fmt.Errorf("failed to build dependency %s: %w", dep.Params().SerializableType(), err)
			}
		}(dep, depInfo)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		// build the definition.
		if err := b.def.Build(b); err != nil {
			errChan <- fmt.Errorf("failed to build definition %s: %w", b.hash[:8], err)
		}
	}()

	go func() {
		wg.Wait()
		close(doneChan)
	}()

	select {
	case err := <-errChan:
		return nil, err
	case <-doneChan:
	}

	// set the written files in the receipt.
	for name, writer := range b.outputs {
		hash := hex.EncodeToString(writer.hasher.Sum(nil))
		b.receipt.OutputHashes[name] = hash
	}

	// set the build time and duration.
	b.receipt.BuildTime = time.Now()
	b.receipt.BuildDuration = time.Since(start)

	// serialize the receipt to a file.
	contents, err = json.Marshal(b.receipt)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize receipt: %w", err)
	}

	if err := writeFile(b.buildDirectory, RECEIPT_FILENAME, contents); err != nil {
		return nil, fmt.Errorf("failed to write receipt: %w", err)
	}

	slog.Info("built", "hash", b.hash[:8], "duration", b.receipt.BuildDuration)

	// create the artifact.
	b.artifact = &buildArtifact{
		hash:     b.hash,
		receipt:  b.receipt,
		buildDir: b.buildDirectory,
	}

	return b.artifact, nil
}

var (
	_ BuildContext = &buildContext{}
)

type BuildDefinition interface {
	hash.Definition

	Build(ctx BuildContext) error
	Dependencies() ([]BuildDefinition, error)
}

type basicBuildDefinitionParams struct {
	Name      string
	SleepTime int
	Children  []BuildDefinition
}

// SerializableType implements hash.SerializableValue.
func (b basicBuildDefinitionParams) SerializableType() string { return "basicBuildDefinitionParams" }

var (
	_ hash.SerializableValue = basicBuildDefinitionParams{}
)

type basicBuildDefinition struct {
	params basicBuildDefinitionParams
}

func (b *basicBuildDefinition) String() string {
	return b.params.Name
}

// Create implements BuildDefinition.
func (b *basicBuildDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &basicBuildDefinition{params: params.(basicBuildDefinitionParams)}
}

// Params implements BuildDefinition.
func (b *basicBuildDefinition) Params() hash.SerializableValue {
	return b.params
}

// SerializableType implements BuildDefinition.
func (b *basicBuildDefinition) SerializableType() string {
	return "basicBuildDefinition"
}

// Build implements BuildDefinition.
func (b *basicBuildDefinition) Build(ctx BuildContext) error {
	slog.Info("[#] starting", "name", b.params.Name)

	for _, child := range b.params.Children {
		if _, err := ctx.BuildChild(child, BuildOptions{}); err != nil {
			return err
		}
	}

	slog.Info("[#] building", "name", b.params.Name)

	time.Sleep(time.Duration(b.params.SleepTime) * time.Millisecond)

	out, err := ctx.CreateOutput("output")
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := out.Write([]byte("hello world\n")); err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	slog.Info("[#] finished", "name", b.params.Name)

	return nil
}

// Dependencies implements BuildDefinition.
func (b *basicBuildDefinition) Dependencies() ([]BuildDefinition, error) {
	return b.params.Children, nil
}

var (
	_ BuildDefinition = &basicBuildDefinition{}
)

func newBasicBuildDefinition(name string, sleepTime time.Duration, children ...BuildDefinition) *basicBuildDefinition {
	return &basicBuildDefinition{
		params: basicBuildDefinitionParams{
			Name:      name,
			SleepTime: int(sleepTime.Milliseconds()),
			Children:  children,
		},
	}
}

func dumpTree(b Builder, art BuildArtifact, info *DependencyInfo, prefix string) {
	def, err := b.DefinitionFromArtifact(art)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sfailed to get definition: %v\n", prefix, err)
		return
	}

	usedCache := "fresh"
	if info != nil && info.UsedCache {
		usedCache = "cache"
	}

	fmt.Fprintf(os.Stderr, "[%s] %s%s [%s, %s]\n", art.Hash()[:8], prefix, def, art.Receipt().BuildDuration, usedCache)
	for _, dep := range art.Receipt().Dependencies {
		child, err := b.ArtifactFromHash(dep.Hash)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sfailed to get child: %v\n", prefix, err)
			continue
		}

		dumpTree(b, child, dep, prefix+"  ")
	}
}

func appMain() error {
	hash.RegisterType(&basicBuildDefinition{})

	builder := NewBuilder(filesystem.NewMemoryDirectory())

	item2 := newBasicBuildDefinition("item2", 100*time.Millisecond)

	defTree := newBasicBuildDefinition("top", 200*time.Millisecond,
		newBasicBuildDefinition("topIt2", 400*time.Millisecond, item2),
		newBasicBuildDefinition("long", 800*time.Millisecond,
			newBasicBuildDefinition("longChild1", 200*time.Millisecond),
			newBasicBuildDefinition("longChild2", 200*time.Millisecond, item2),
			newBasicBuildDefinition("longChild3", 200*time.Millisecond),
			newBasicBuildDefinition("longChild4", 200*time.Millisecond),
			newBasicBuildDefinition("longChild5", 200*time.Millisecond),
		),
		item2,
	)

	res, err := builder.Build(defTree, BuildOptions{})
	if err != nil {
		return err
	}

	dumpTree(builder, res, nil, "")

	deleted, err := builder.GarbageCollect(time.Now().Add(time.Hour))
	if err != nil {
		return err
	}

	slog.Info("garbage collected", "deleted", deleted)

	return nil
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
