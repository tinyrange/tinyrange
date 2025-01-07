package build2

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
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/tinyrange/tinyrange/pkg/buildinfo"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
)

// A valid output name is alphanumeric with underscores.
var validOutputName = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

const (
	defFilename     = "definition.json"
	receiptFilename = "receipt.json"
	lockFilename    = "lock.pid"
	outputPrefix    = "output."
)

func formatHash(hash hash.Hash) string {
	if len(hash) < 8 {
		return string(hash)
	}
	return string(hash)[:8]
}

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

type buildInfoState int

const (
	// buildInfoStateWaiting is the initial state of a buildInfo.
	// The buildInfo is waiting for a token to be acquired.
	buildInfoStateWaiting buildInfoState = iota
	// buildInfoStateBuilding is the state of a buildInfo when it is building.
	buildInfoStateBuilding
	// buildInfoStateBuilt is the state of a buildInfo when it has been built.
	buildInfoStateBuilt
)

type buildInfo struct {
	state atomic.Value
	tk    *token
	ctx   *buildContext
	wg    sync.WaitGroup
}

type builder struct {
	mtx            sync.Mutex
	buildDirectory filesystem.MutableDirectory
	hashDb         *hash.DefinitionDatabase
	buildCache     map[hash.Hash]*buildInfo
	tl             *tokenLocker
	logger         BuildLogger
}

// needsRebuild checks if a BuildDefinition needs to be rebuilt.
// returns the BuildArtifact if it does not need to be rebuilt.
func (b *builder) needsRebuild(h hash.Hash, options BuildOptions) (BuildArtifact, error) {
	if options.ForceRebuild {
		return nil, nil
	}

	// returns true if the hash needs to be rebuilt.
	var checkHash func(hash hash.Hash) (bool, error)

	checkHash = func(hash hash.Hash) (bool, error) {
		art, err := b.ArtifactFromHash(hash)
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		} else if err != nil {
			return true, fmt.Errorf("failed to get artifact from hash: %w", err)
		}

		// Check if the receipt has expired.
		expireTime := art.Receipt().ExpireTime
		if !expireTime.IsZero() && expireTime.Before(time.Now()) {
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

	needsRebuild, err := checkHash(h)
	if err != nil {
		return nil, fmt.Errorf("failed to check hash: %w", err)
	} else if needsRebuild {
		return nil, nil
	}

	art, err := b.ArtifactFromHash(h)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get artifact from hash: %w", err)
	}

	return art, nil
}

// Build implements Builder.
func (b *builder) Build(def BuildDefinition, options BuildOptions) (BuildArtifact, error) {
	go func() {
		if err := b.logger.Run(os.Stdout); err != nil {
			slog.Warn("failed to run logger", "error", err)
		}
	}()
	defer b.logger.Close()

	return b.BuildChild(nil, def, options)
}

func (b *builder) getOrCreateBuild(hash hash.Hash) (*buildInfo, bool, error) {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	if info, ok := b.buildCache[hash]; ok {
		return info, false, nil
	}

	info := &buildInfo{
		ctx: nil,
	}
	info.state.Store(buildInfoStateWaiting)
	info.wg.Add(1)
	info.tk = b.tl.New()

	b.buildCache[hash] = info

	return info, true, nil
}

// BuildChild implements Builder.
func (b *builder) BuildChild(parent BuildContext, def BuildDefinition, options BuildOptions) (BuildArtifact, error) {
	parentHash := hash.Hash("")
	if parent != nil {
		parentHash = parent.Hash()
	}

	// Hash the definition.
	hash, err := b.HashDefinition(def)
	if err != nil {
		return nil, fmt.Errorf("failed to hash definition: %w", err)
	}

	var logGroup Group
	if parent != nil {
		logGroup = parent.LogGroup().Subgroup(uuid.NewString())
	} else {
		logGroup = b.logger.Group(uuid.NewString())
	}
	defer logGroup.Close()

	buildInfo, locked, err := b.getOrCreateBuild(hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get or wait for build: %w", err)
	}
	if !locked {
		// If we are a blocker for another build and the current build is still waiting to start then preempt that build.
		if options.BlockerFor != nil && buildInfo.state.Load() == buildInfoStateWaiting {
			logGroup.Description("[%s %s] donating token to preempt build", formatHash(hash), def)
			buildInfo.tk.Donate()
		}

		// Wait for the build to complete.
		logGroup.Description("[%s %s] waiting for build to complete", formatHash(hash), def)
		buildInfo.wg.Wait()

		if buildInfo.ctx != nil {
			if options.DependencyInfo != nil {
				options.DependencyInfo.UsedCache = true
			}

			return buildInfo.ctx.artifact, nil
		} else {
			logGroup.Description("[%s %s] getting existing artifact", formatHash(hash), def)
			return b.ArtifactFromHash(hash)
		}
	}
	defer buildInfo.wg.Done()

	logGroup.Description("[%s %s] checking if a rebuild is needed", formatHash(hash), def)

	// Check if the definition needs to be rebuilt.
	art, err := b.needsRebuild(hash, options)
	if err != nil {
		return nil, err
	} else if art != nil {
		logGroup.Description("[%s %s] skipping build", formatHash(hash), def)

		if options.DependencyInfo != nil {
			options.DependencyInfo.UsedCache = true
		}

		return art, nil
	}

	logGroup.Description("[%s %s] waiting for token", formatHash(hash), def)

	// Lock the token locker.
	defer buildInfo.tk.Lock().Close()

	logGroup.Description("[%s %s] building", formatHash(hash), def)

	buildInfo.state.Store(buildInfoStateBuilding)

	// Create a child directory for the build.
	childDir, err := b.buildDirectory.Mkdir(string(hash))
	if err != nil && !errors.Is(err, fs.ErrExist) {
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
		group:   logGroup,
	}

	// Build the definition.
	art, err = buildInfo.ctx.build()
	if err != nil {
		return nil, fmt.Errorf("failed to build: %w", err)
	}

	buildInfo.state.Store(buildInfoStateBuilt)

	logGroup.Description("[%s %s] build finished", formatHash(hash), def)

	if art == nil {
		logGroup.Description("[%s %s] returning existing artifact", formatHash(hash), def)

		if options.DependencyInfo != nil {
			options.DependencyInfo.UsedCache = true
		}

		return b.ArtifactFromHash(hash)
	}
	return art, nil
}

// HashDefinition implements Builder.
func (b *builder) HashDefinition(def BuildDefinition) (hash.Hash, error) {
	return b.hashDb.HashDefinition(def)
}

// SerializeDefinition implements Builder.
func (b *builder) MarshalDefinition(def BuildDefinition) ([]byte, error) {
	return b.hashDb.MarshalDefinition(def)
}

var validSha256 = regexp.MustCompile(`^[a-f0-9]{64}$`)

// ArtifactFromHash implements Builder.
func (b *builder) ArtifactFromHash(hash hash.Hash) (BuildArtifact, error) {
	// validate the hash
	if !validSha256.MatchString(string(hash)) {
		return nil, fmt.Errorf("invalid hash: %s", hash)
	}

	child, err := b.buildDirectory.GetChild(string(hash))
	if err != nil {
		return nil, err
	}

	childDir, ok := child.File.(filesystem.Directory)
	if !ok {
		return nil, fmt.Errorf("child is not a directory: %T", child)
	}

	receiptFile, err := childDir.GetChild(receiptFilename)
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
func (b *builder) GarbageCollect(olderThan time.Time) ([]hash.Hash, error) {
	ents, err := b.buildDirectory.Readdir()
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	receipts := make(map[hash.Hash]*garbageCollectorState)

	// Populate the receipts map.
	for _, ent := range ents {
		if !validSha256.MatchString(ent.Name) {
			continue
		}

		art, err := b.ArtifactFromHash(hash.Hash(ent.Name))
		if err != nil {
			return nil, fmt.Errorf("failed to get artifact from hash: %w", err)
		}

		receipts[hash.Hash(ent.Name)] = &garbageCollectorState{
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
	var checkNext []hash.Hash
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
		for _, dep := range receipts[hash].receipt.Dependencies {
			depHash := dep.Hash

			receipts[depHash].references--

			if receipts[depHash].receipt.BuildTime.After(olderThan) {
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

func (b *builder) hashCacheMiss(hash hash.Hash) (io.ReadCloser, error) {
	art, err := b.ArtifactFromHash(hash)
	if err != nil {
		return nil, err
	}

	return art.RawDefinition()
}

var (
	_ Builder = &builder{}
)

type buildArtifact struct {
	hash     hash.Hash
	receipt  BuildReceipt
	buildDir filesystem.Directory
}

// Hash implements BuildArtifact.
func (b *buildArtifact) Hash() hash.Hash {
	return b.hash
}

// Receipt implements BuildArtifact.
func (b *buildArtifact) Receipt() BuildReceipt {
	return b.receipt
}

// RawDefinition implements BuildArtifact.
func (b *buildArtifact) RawDefinition() (io.ReadCloser, error) {
	defFile, err := b.buildDir.GetChild(defFilename)
	if err != nil {
		return nil, err
	}

	return defFile.File.Open()
}

// GetOutput implements BuildArtifact.
func (b *buildArtifact) GetOutput(name string) (filesystem.File, error) {
	child, err := b.buildDir.GetChild(outputPrefix + name)
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
		if strings.HasPrefix(ent.Name, outputPrefix) {
			outputs = append(outputs, ent.Name[len(outputPrefix):])
		}
	}
	return outputs, nil
}

var (
	_ BuildArtifact = &buildArtifact{}
)

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

type buildContext struct {
	mtx            sync.Mutex
	builder        Builder
	parent         BuildContext
	hash           hash.Hash
	buildDirectory filesystem.MutableDirectory
	def            BuildDefinition
	receipt        BuildReceipt
	outputs        map[string]*buildOutputWriter
	artifact       BuildArtifact
	group          Group
}

// Logf implements BuildContext.
func (b *buildContext) Logf(format string, args ...any) {
	b.group.Logf(format, args...)
}

// LogGroup implements BuildContext.
func (b *buildContext) LogGroup() Group {
	return b.group
}

func (b *buildContext) updateStatus(format string, args ...any) {
	b.group.Description(fmt.Sprintf("[%s %s] %s", formatHash(b.hash), b.def, fmt.Sprintf(format, args...)))
}

// Hash implements BuildContext.
func (b *buildContext) Hash() hash.Hash { return b.hash }

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
	opts.BlockerFor = b

	return b.builder.BuildChild(b, def, opts)
}

// CreateOutput implements BuildContext.
func (b *buildContext) CreateOutput(name string) (BuildOutputWriter, error) {
	// Validate the output name.
	if !validOutputName.MatchString(name) {
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

func (b *buildContext) writeLockFile(hash hash.Hash, buildDir filesystem.MutableDirectory) (bool, error) {
	// write the lock file.
	pid := os.Getpid()
	pidStr := strconv.Itoa(pid)

	if err := writeFile(buildDir, lockFilename, []byte(pidStr)); err != nil {
		return false, fmt.Errorf("failed to write lock file: %w", err)
	}

	// double check that we own the lock file that was written.
	lockFile, err := buildDir.GetChild(lockFilename)
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

		return b.waitForLockFile(hash, buildDir, pid)
	}

	return true, nil
}

func (b *buildContext) waitForLockFile(hash hash.Hash, buildDir filesystem.MutableDirectory, pid int) (bool, error) {
	// wait for the lock to be released.
	b.updateStatus("waiting for lock release hash=%s", formatHash(hash))

	for {
		time.Sleep(100 * time.Millisecond)
		// slog.Info("wait")

		// check to see if the lock file still exists.
		_, err := buildDir.GetChild(lockFilename)
		if err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}

		// if the process crashed, we need to rebuild.
		if running, _ := processRunning(pid); !running {
			b.updateStatus("process crashed pid=%d", pid)

			return b.writeLockFile(hash, buildDir)
		} else {
			// the lock file was deleted and the process is still
			// running so we assume it's built successfully.
			return false, nil
		}
	}
}

// returns true if we own the lock file and need to build.
func (b *buildContext) exclusiveLockForBuild(hash hash.Hash, buildDir filesystem.MutableDirectory) (bool, error) {
	// check if the lock file exists.
	lockFile, err := buildDir.GetChild(lockFilename)
	if errors.Is(err, fs.ErrNotExist) {
		return b.writeLockFile(hash, buildDir)
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
	if running, _ := processRunning(pid); !running {
		b.updateStatus("process crashed pid=%d", pid)

		return b.writeLockFile(hash, buildDir)
	} else {
		return b.waitForLockFile(hash, buildDir, pid)
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
	ok, err := b.exclusiveLockForBuild(b.hash, b.buildDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for lock release: %w", err)
	}
	if !ok {
		// the build is already in progress.
		// slog.Info("skipping build due to lock", "hash", b.hash[:8])
		return nil, nil
	}
	defer b.buildDirectory.Unlink(lockFilename)

	b.updateStatus("building")

	// serialize the definition to a file.
	contents, err := b.builder.MarshalDefinition(b.def)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize definition: %w", err)
	}

	if err := writeFile(b.buildDirectory, defFilename, contents); err != nil {
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
			errChan <- fmt.Errorf("failed to build definition %s: %w", formatHash(b.hash), err)
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

	if err := writeFile(b.buildDirectory, receiptFilename, contents); err != nil {
		return nil, fmt.Errorf("failed to write receipt: %w", err)
	}

	b.updateStatus("built duration=%s", b.receipt.BuildDuration)

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
