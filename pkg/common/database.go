package common

import (
	"io"
	"net/http"

	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	"go.starlark.net/starlark"
)

type BuildOptions struct {
	AlwaysRebuild bool
}

type PlanOptions struct {
	Debug bool
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

	// EnsureLoaded ensures that the container builder is loaded. This needs to be called before any other methods.
	EnsureLoaded(ctx BuildContext) error

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

type ScriptManager interface {
	GetBuilder(filename string, builder string) (starlark.Callable, error)
	NewThread(filename string) *starlark.Thread
	RunScript(filename string, files map[string]filesystem.File, additionalArgs []string, outputFilename string) error
}

// ContainerBuilders take a package collection and create an installation plan from a list of queries.
// The manager is responsible for loading and managing container builders.
type ContainerBuilderManager interface {
	GetContainerBuilder(ctx BuildContext, name string, arch config.CPUArchitecture) (ContainerBuilder, error)
	GetContainerBuilders() map[string]ContainerBuilder
	LoadBuiltinBuilders() error
}

// Macros are functions that can be called to return definitions or directives.
// The manager is responsible for creating a macro context and getting macros.
type MacroManager interface {
	ScriptManager

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

// PackageDatabase is the core interface.
type PackageDatabase interface {
	starlark.Value

	MirrorManager
	ScriptManager
	ContainerBuilderManager
	MacroManager
	DistributionServerManager
	RequestManager

	// Get the build directory.
	BuildDir() string
	// Get the filename of a given hash.
	FilenameFromHash(hash hash.Hash, suffix string) (string, error)
	// Build a definition from a build context.
	Build(ctx BuildContext, def BuildDefinition, opts BuildOptions) (filesystem.File, error)
	// ShouldRebuildUserDefinitions returns whether user definitions should be rebuilt.
	ShouldRebuildUserDefinitions() bool
	// SetRebuildUserDefinitions sets whether user definitions should be rebuilt.
	SetRebuildUserDefinitions(rebuild bool)
	// HashDefinition creates a hash from a build definition.
	HashDefinition(def BuildDefinition) (hash.Hash, error)
	// NewBuildContext creates a new build context from a build source.
	NewBuildContext(source BuildSource) BuildContext
	// Get a build definition by hash.
	GetDefinitionByHash(hash hash.Hash) (BuildDefinition, error)
	// Pretty print a build definition and write the result to the given writer.
	Inspect(def BuildDefinition, out io.Writer) error
	// Get a list of all hashes in the database.
	GetAllHashes() ([]hash.Hash, error)
}
