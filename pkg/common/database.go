package common

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"

	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

type OutputFileHandle interface {
	filesystem.WritableFileHandle

	GetHostFilename() (string, error)
}

type OutputFileProvider interface {
	// File opens a output file in the build directory.
	File(name string) (filesystem.File, error)
}

type BuildCacheDirectory interface {
	OutputFileProvider

	// ReadDefinition opens the definition file in the build directory and reads it.
	ReadDefinition() ([]byte, error)

	// ReadReceipt opens the receipt file in the build directory and reads it.
	ReadReceipt() ([]byte, error)
}

type WritableBuildCacheDirectory interface {
	BuildCacheDirectory

	// WriteDefinition writes the build definition.
	WriteDefinition(def []byte) error

	// WriteReceipt writes the build receipt.
	WriteReceipt(recept []byte) error

	// CreateOutputFile creates a new output file in the build directory.
	CreateOutputFile(name string) (OutputFileHandle, error)
}

type SimpleCache interface {
	GetOrSet(hash string, setter func(w io.Writer) error) (io.ReaderAt, error)
}

type BuildCacheFilesystem interface {
	config.BuildCacheFilesystem

	// SimpleCache returns a basic get/set cache suitable for small files.
	// Can return nil if not supported.
	SimpleCache() SimpleCache

	// GetHostFilename returns the host filename of the build cache directory.
	GetHostFilename() (string, error)

	// CreateBuildDirectory creates a new build directory.
	CreateBuildDirectory(hash hash.Hash) (BuildCacheDirectory, error)

	// GetBuildDirectory returns the build directory for the given hash.
	GetBuildDirectory(hash hash.Hash) (BuildCacheDirectory, error)

	// GetAllHashes returns all the hashes in the build cache.
	GetAllHashes() ([]hash.Hash, error)

	// DatabaseConfig returns the configuration for the build database.
	DatabaseConfig() ([]filesystem.BuildDatabaseConfig, error)
}

type ErrNonFatal struct {
	Err error
}

func (e ErrNonFatal) Error() string {
	return "non-fatal: " + e.Err.Error()
}

func (e ErrNonFatal) Unwrap() error {
	return e.Err
}

var (
	_ error = ErrNonFatal{}
)

func MakeNonFatal(err error) error {
	return ErrNonFatal{Err: err}
}

type ErrTemplateBuilt string

// Error implements error.
func (e ErrTemplateBuilt) Error() string { return "template built" }

var (
	_ error = ErrTemplateBuilt("")
)

var (
	ErrUseExistingBuild = errors.New("use existing build")
)

type BuilderFactor func(PackageDatabase) (Builder, error)

// BuildResult is implemented by definitions and called with a writer.
type BuildResult interface {
	// WriteResult writes the result of the build to the given writer.
	WriteResult(out io.Writer) error
}

type StarBuildDefinition interface {
	starlark.Value
	BuildDefinition
}

// BuildDefinition is a definition that can be built and cached.
type BuildDefinition interface {
	hash.Definition
	MacroResult
	fmt.Stringer

	// NeedsBuild returns whether the definition needs to be rebuilt.
	NeedsBuild(ctx BuildContext) (bool, error)
	// Dependencies returns the dependencies of the definition.
	Dependencies() ([]BuildDefinition, error)
	// Build builds the definition and returns the result.
	Build(ctx BuildContext) error
	// ToStarlark converts the definition to a starlark value.
	ToStarlark(artifact BuildArtifact) (starlark.Value, error)
}

// RedistributableDefinition is a extension of BuildDefinition that can be redistributed.
// Redistributable definitions can be downloaded from public servers.
type RedistributableDefinition interface {
	BuildDefinition
	Redistributable() bool
}

type MacroResult interface {
}

type BuildOptions struct {
	AlwaysRebuild bool
}

type PlanOptions struct {
	Debug bool
}

type BuildReceipt struct {
	Requirements []hash.Hash       `json:"requirements"`
	StartTime    time.Time         `json:"start_time"`
	Duration     time.Duration     `json:"duration"`
	Files        map[string]string `json:"files"` // map of filename to sha256 hash
}

// BuildArtifact is the result of a build.
type BuildArtifact interface {
	OutputFileProvider

	// Database returns the package database.
	Database() PackageDatabase

	// Hash returns the hash of the definition.
	DefinitionHash() hash.Hash
	// Receipt returns the receipt of the build.
	Receipt() BuildReceipt
	// Default returns the default file written with WriteDefault.
	Default() (filesystem.File, error)
	// OpenFile opens a file in the artifact.
	OpenFile(name string) (filesystem.FileHandle, error)

	// ReferenceForFile returns a database reference for the given file.
	ReferenceForFile(name string) (config.DatabaseReference, error)
	// ReferenceForDefault returns a database reference for the default file.
	ReferenceForDefault() (config.DatabaseReference, error)
}

type MinimalBuildContext interface {
	// BuildChild builds a child definition.
	BuildChild(def BuildDefinition) (BuildArtifact, error)
	// Database returns the package database.
	Database() PackageDatabase
	// Return a factory to make definitions.
	Factory() DefinitionFactory
}

// BuildContext is the context of a build.
type BuildContext interface {
	starlark.Value
	MinimalBuildContext

	// DatabaseConfig returns the configuration for the build database.
	DatabaseConfig() ([]filesystem.BuildDatabaseConfig, error)

	// PrenotifyChildren starts builds in the background for a list of children.
	PrenotifyChildren(children []BuildDefinition) error

	// ShouldRebuildUserDefinitions returns whether user definitions should be rebuilt.
	ShouldRebuildUserDefinitions() bool
	// LastBuild returns the time of the last build.
	LastBuild() time.Time
	// RunVMM runs a VMM with the given configuration.
	RunVMM(vmm string, config config.TinyRangeConfig) (*exec.Cmd, error)
	// Hash returns the hash of the build context.
	DefinitionHash() hash.Hash
	// CreateDefault creates the default file for the build.
	CreateDefault() (io.WriteCloser, error)
	// WriteDefault writes the default file for the build.
	WriteDefault(result BuildResult) error

	// CreateFile creates a file in the build context.
	CreateFile(name string) (io.WriteCloser, error)
	// Describe logs a message about the build.
	Describe(format string, args ...interface{})
	// Logf logs a message about the build.
	Logf(format string, args ...interface{})
}

// InstallationPlan represents a plan for installing packages.
// A complete installation plan has a list of directives that are executed in order.
type InstallationPlan interface {
	starlark.Value

	// Add a new package to the plan.
	Add(ctx MinimalBuildContext, builder ContainerBuilder, query PackageQuery, isDefault bool) error
	// Get the list of directives in the plan.
	Directives() []Directive
	// Set the list of directives in the plan.
	SetDirectives(directives []Directive)
	// Dump a formatted version of the plan to a writer.
	WriteTree(output io.Writer) error
}

// PackageCollection represents a collection of packages from a single source.
type PackageCollection interface {
	starlark.Value

	// Search for packages that match the given query.
	Query(query PackageQuery) ([]*Package, error)
	// Get the package with the given name.
	InstallerFor(ctx MinimalBuildContext, pkg *Package, tags TagList) (*Installer, error)
}

// ContainerBuilder takes a package collection and creates an installation plan from a list of queries.
type ContainerBuilder interface {
	starlark.Value

	// Key returns the key of the container builder used to access it in PackageDatabase.
	Key() string
	// DisplayName returns the display name of the container builder.
	DisplayName() string
	// Packages returns the package collection of the container builder.
	Packages() PackageCollection
	// EnsureLoaded ensures that the container builder is loaded.
	EnsureLoaded(ctx MinimalBuildContext) error

	// Plan creates an installation plan from a list of queries.
	Plan(ctx MinimalBuildContext, packages []PackageQuery, tags TagList, opts PlanOptions) (InstallationPlan, error)
	// Search for packages that match the given query.
	Search(pkg PackageQuery) ([]*Package, error)
}

type InstallationPlanBuilder interface {
	starlark.Value
}

// MacroContext represents the context in which a macro is executed.
type MacroContext interface {
	// Thread returns the Starlark thread in which the macro is executed.
	Thread() *starlark.Thread

	Builder(name string) (InstallationPlanBuilder, error)
	AddBuilder(name string, builder InstallationPlanBuilder)

	// Variable returns the value of a variable with the given name.
	Variable(name string) string
	// AddVariable adds a new variable with the given name and value.
	AddVariable(name string, value string)
}

type Macro interface {
	Call(ctx MacroContext) (MacroResult, error)
}

// Mirrors are a configurable shorthand for a list of URLs.
// Mirrors use the url scheme mirror://<name>/<path> to refer to a URL.
type MirrorManager interface {
	// UrlsFor returns the list of URLs for the given URL.
	UrlsFor(url string) ([]string, error)
	// AddMirror adds a new mirror with the given name and URLs.
	// If the mirror already exists, the URLs are overwritten.
	AddMirror(name string, options []string) error
}

// ContainerBuilders take a package collection and create an installation plan from a list of queries.
// The manager is responsible for loading and managing container builders.
type ContainerBuilderManager interface {
	GetContainerBuilder(name string, arch config.CPUArchitecture) (ContainerBuilder, error)
	GetContainerBuilders() map[string]ContainerBuilder
}

// Macros are functions that can be called to return definitions or directives.
// The manager is responsible for creating a macro context and getting macros.
type MacroManager interface {
	// NewMacroContext creates a new macro context.
	// MacroContexts store builders and variables that can be used by macros.
	NewMacroContext() MacroContext
	// GetMacroByShorthand returns a macro with the given shorthand.
	GetMacroByShorthand(ctx MacroContext, shorthand string, allowLocal bool) (Macro, error)
}

// RequestManager is responsible for creating http clients.
type RequestManager interface {
	// HttpClient returns an http client.
	HttpClient() (*http.Client, error)
}

// Builder is the root object used to build definitions.
type Builder interface {
	// Filesystem returns the filesystem used to store build artifacts.
	Filesystem() BuildCacheFilesystem
	// Build builds a definition.
	Build(def BuildDefinition, opts BuildOptions) (BuildArtifact, error)
	// SetRebuildUserDefinitions sets whether user definitions should be rebuilt.
	SetRebuildUserDefinitions(rebuild bool)
	// GarbageCollect removes old build artifacts.
	GarbageCollect(olderThan time.Time) ([]hash.Hash, error)
	// MinimalContext returns a minimal build context that can only build children.
	MinimalContext() MinimalBuildContext
	// GetDefinitionByHash returns a definition by hash.
	GetDefinitionByHash(hash hash.Hash) (BuildDefinition, error)
	// FileFromReference returns a file from a database reference.
	FileFromReference(ref config.DatabaseReference) (filesystem.File, error)
	// ImportAndValidate imports and validates a definition adding it to the build cache in an unbuilt state.
	ImportAndValidate(def []byte) (BuildDefinition, error)
}

// PackageDatabase is the core interface.
type PackageDatabase interface {
	MirrorManager
	ContainerBuilderManager
	MacroManager
	RequestManager
	Builder() Builder

	// Get a set of extended file methods to return a HTTPClient and handle simple caching.
	FileMethods() filesystem.ExtendedFileMethods

	// Call calls a starlark function declared in a file.
	Call(filename string, builder string, args ...starlark.Value) (starlark.Value, error)
	// Run a top-level script.
	RunScript(
		filename string,
		files map[string]filesystem.File,
		additionalArgs []string,
		outputFilename string,
	) error
}
