package common

import (
	"io"
	"net/http"
	"os/exec"
	"time"

	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

// BuildResult is implemented by definitions and called with a writer.
type BuildResult interface {
	// WriteResult writes the result of the build to the given writer.
	WriteResult(out io.Writer) error
}

// DependencyNode is a single dependency in the build graph.
type DependencyNode interface {
	hash.SerializableValue

	// Dependencies returns the dependencies of the node.
	// This doesn't have to return all dependencies just those that can be staticky determined.
	Dependencies(ctx BuildContext) ([]DependencyNode, error)
}

// BuildDefinition is a definition that can be built and cached.
type BuildDefinition interface {
	hash.Definition
	DependencyNode
	MacroResult
	// Tag returns a human readable name for the definition.
	Tag() string
	// NeedsBuild returns whether the definition needs to be rebuilt.
	NeedsBuild(ctx BuildContext, cacheTime time.Time) (bool, error)
	// Build builds the definition and returns the result.
	Build(ctx BuildContext) (BuildResult, error)
	// ToStarlark converts the definition to a starlark value.
	ToStarlark(ctx BuildContext, result filesystem.File) (starlark.Value, error)
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

type BuildContext interface {
	starlark.Value

	// CreateOutput creates the main output file early.
	CreateOutput() (io.WriteCloser, error)
	// CreateFile creates a new file in the build directory.
	CreateFile(name string) (string, io.WriteCloser, error)
	// HasCached returns whether the build context has a cached file for this build already.
	// This can be used to skip building if the file is already cached.
	HasCached() bool
	// Database returns the package database.
	Database() PackageDatabase
	// BuildChild builds a given child definition.
	BuildChild(def BuildDefinition) (filesystem.File, error)
	// NeedsBuild returns whether the given definition needs to be rebuilt.
	NeedsBuild(def BuildDefinition) (bool, error)
	// Call calls a starlark function declared in a file.
	Call(filename string, builder string, args ...starlark.Value) (starlark.Value, error)
	// FileFromDigest returns a file from a file digest.
	FileFromDigest(digest *filesystem.FileDigest) (filesystem.File, error)
	// FilenameFromDigest returns a filename from a file digest.
	FilenameFromDigest(digest *filesystem.FileDigest) (string, error)
	// RunVMM runs a VMM with the given configuration.
	RunVMM(vmm string, config config.TinyRangeConfig) (*exec.Cmd, error)
	// ShouldRebuildUserDefinitions returns whether user definitions should be rebuilt.
	ShouldRebuildUserDefinitions() bool
}

// InstallationPlan represents a plan for installing packages.
// A complete installation plan has a list of directives that are executed in order.
type InstallationPlan interface {
	starlark.Value

	// Add a new package to the plan.
	Add(ctx BuildContext, builder ContainerBuilder, query PackageQuery, isDefault bool) error
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
	InstallerFor(ctx BuildContext, pkg *Package, tags TagList) (*Installer, error)
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

	// Plan creates an installation plan from a list of queries.
	Plan(ctx BuildContext, packages []PackageQuery, tags TagList, opts PlanOptions) (InstallationPlan, error)
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

// Distribution Servers are a remote build directory used to distribute build artifacts.
// The manager is responsible for setting the distribution server and running the server.
type DistributionServerManager interface {
	// SetDistributionServer sets the distribution server.
	SetDistributionServer(server string) error
	// RunDistributionServer runs the distribution server on the given address.
	RunDistributionServer(addr string) error
}

// RequestManager is responsible for creating http clients.
type RequestManager interface {
	// GetHttpClient returns an http client.
	HttpClient() (*http.Client, error)
}

type Builder interface {
	// Build a definition from a build context.
	Build(def BuildDefinition, opts BuildOptions) (filesystem.File, error)
	// SetRebuildUserDefinitions sets whether user definitions should be rebuilt.
	SetRebuildUserDefinitions(rebuild bool)
}

// PackageDatabase is the core interface.
type PackageDatabase interface {
	MirrorManager
	ContainerBuilderManager
	MacroManager
	DistributionServerManager
	RequestManager
	Builder

	// Run a top-level script.
	RunScript(
		filename string,
		files map[string]filesystem.File,
		additionalArgs []string,
		outputFilename string,
	) error
}
