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
	"strings"
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

type BuildReceipt struct {
	// Version is the version of TinyRange that built the artifact.
	Version string `json:"version"`
	// Redistributable is true if the artifact can be redistributed.
	Redistributable bool `json:"redistributable"`
	// Non-zero if the build should unconditionally be rebuilt after this time.
	ExpireTime time.Time `json:"expire_time"`
	// Dependencies is a list of dependencies that were used to build the artifact.
	Dependencies []string `json:"dependencies"`
	// BuiltFor is the parent definition that the artifact was built for.
	BuiltFor string `json:"built_for"`
	// OutputHashes is a map of output names to their SHA-256 hashes.
	OutputHashes map[string]string `json:"output_hashes"`
}

type BuildOptions struct {
	ForceRebuild bool
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
}

type builder struct {
	buildDirectory filesystem.MutableDirectory
	hashDb         *hash.DefinitionDatabase
}

// needsRebuild checks if a BuildDefinition needs to be rebuilt.
// returns the BuildArtifact if it does not need to be rebuilt.
func (b *builder) needsRebuild(def BuildDefinition, options BuildOptions) (BuildArtifact, error) {
	if options.ForceRebuild {
		return nil, nil
	}

	hash, err := b.HashDefinition(def)
	if err != nil {
		return nil, fmt.Errorf("failed to hash definition: %w", err)
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
			ok, err := checkHash(dep)
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

// BuildChild implements Builder.
func (b *builder) BuildChild(parent BuildContext, def BuildDefinition, options BuildOptions) (BuildArtifact, error) {
	parentHash := ""
	if parent != nil {
		parentHash = parent.Hash()
	}

	// Check if the definition needs to be rebuilt.
	art, err := b.needsRebuild(def, options)
	if err != nil {
		return nil, err
	} else if art != nil {
		// The artifact does not need to be rebuilt.
		return art, nil
	}

	// Hash the definition.
	hash, err := b.HashDefinition(def)
	if err != nil {
		return nil, fmt.Errorf("failed to hash definition: %w", err)
	}

	// Create a child directory for the build.
	childDir, err := b.buildDirectory.Mkdir(hash)
	if err != nil {
		return nil, fmt.Errorf("failed to create build directory: %w", err)
	}

	// Create a new build context.
	ctx := &buildContext{
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
	return ctx.build()
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
		receipt:  receipt,
		buildDir: childDir,
	}, nil
}

// DefinitionFromArtifact implements Builder.
func (b *builder) DefinitionFromArtifact(artifact BuildArtifact) (BuildDefinition, error) {
	return nil, fmt.Errorf("Builder.DefinitionFromArtifact not implemented")
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
	}
	b.hashDb = hash.NewDefinitionDatabase(b.hashCacheMiss)
	return b
}

type BuildArtifact interface {
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
	receipt  BuildReceipt
	buildDir filesystem.Directory
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
	builder        Builder
	parent         BuildContext
	hash           string
	buildDirectory filesystem.MutableDirectory
	def            BuildDefinition
	receipt        BuildReceipt
	outputs        map[string]*buildOutputWriter
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

// BuildChild implements BuildContext.
func (b *buildContext) BuildChild(def BuildDefinition, opts BuildOptions) (BuildArtifact, error) {
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

func writeFile(dir filesystem.MutableDirectory, name string, contents []byte) error {
	file, err := dir.Create(name, nil)
	if err != nil {
		return err
	}

	mutFile, ok := file.(filesystem.MutableFile)
	if !ok {
		return fmt.Errorf("file is not mutable: %T", file)
	}

	return mutFile.Overwrite(contents)
}

func (b *buildContext) build() (BuildArtifact, error) {
	// serialize the definition to a file.
	contents, err := b.builder.MarshalDefinition(b.def)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize definition: %w", err)
	}

	if err := writeFile(b.buildDirectory, DEFINITION_FILENAME, contents); err != nil {
		return nil, fmt.Errorf("failed to write definition: %w", err)
	}

	// build the definition.
	if err := b.def.Build(b); err != nil {
		return nil, fmt.Errorf("failed to build definition %s: %w", b.hash[:8], err)
	}

	// set the written files in the receipt.
	for name, writer := range b.outputs {
		hash := hex.EncodeToString(writer.hasher.Sum(nil))
		b.receipt.OutputHashes[name] = hash
	}

	// serialize the receipt to a file.
	contents, err = json.Marshal(b.receipt)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize receipt: %w", err)
	}

	if err := writeFile(b.buildDirectory, RECEIPT_FILENAME, contents); err != nil {
		return nil, fmt.Errorf("failed to write receipt: %w", err)
	}

	// create the artifact.
	return &buildArtifact{
		receipt:  b.receipt,
		buildDir: b.buildDirectory,
	}, nil
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

// Create implements BuildDefinition.
func (b *basicBuildDefinition) Create(params hash.SerializableValue) hash.Definition {
	return &basicBuildDefinition{params: *params.(*basicBuildDefinitionParams)}
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
	for _, child := range b.params.Children {
		if _, err := ctx.BuildChild(child, BuildOptions{}); err != nil {
			return err
		}
	}

	time.Sleep(time.Duration(b.params.SleepTime) * time.Millisecond)

	out, err := ctx.CreateOutput("output")
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := out.Write([]byte("hello world\n")); err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	return nil
}

// Dependencies implements BuildDefinition.
func (b *basicBuildDefinition) Dependencies() ([]BuildDefinition, error) {
	return b.params.Children, nil
}

var (
	_ BuildDefinition = &basicBuildDefinition{}
)

func newBasicBuildDefinition(sleepTime time.Duration, children ...BuildDefinition) *basicBuildDefinition {
	return &basicBuildDefinition{
		params: basicBuildDefinitionParams{
			SleepTime: int(sleepTime.Milliseconds()),
			Children:  children,
		},
	}
}

func appMain() error {
	hash.RegisterType(&basicBuildDefinition{})

	builder := NewBuilder(filesystem.NewMemoryDirectory())

	defTree := newBasicBuildDefinition(100*time.Millisecond,
		newBasicBuildDefinition(200*time.Millisecond),
	)

	res, err := builder.Build(defTree, BuildOptions{})
	if err != nil {
		return err
	}

	_ = res

	return nil
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
