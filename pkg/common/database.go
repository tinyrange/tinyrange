package common

import (
	"io"
	"net/http"

	"github.com/tinyrange/tinyrange/pkg/config"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"go.starlark.net/starlark"
)

type BuildOptions struct {
	AlwaysRebuild bool
}

type PlanOptions struct {
	Debug bool
}

type InstallationPlan interface {
	starlark.Value

	Add(ctx BuildContext, builder ContainerBuilder, query PackageQuery, isDefault bool) error
	Directives() []Directive
	SetDirectives(directives []Directive)
	WriteTree() error
}

type PackageCollection interface {
	starlark.Value

	Query(query PackageQuery) ([]*Package, error)
	InstallerFor(ctx BuildContext, pkg *Package, tags TagList) (*Installer, error)
}

type ContainerBuilder interface {
	starlark.Value

	Key() string
	Packages() PackageCollection
	EnsureLoaded(ctx BuildContext) error
	DisplayName() string
	Plan(ctx BuildContext, packages []PackageQuery, tags TagList, opts PlanOptions) (InstallationPlan, error)
	Search(pkg PackageQuery) ([]*Package, error)
}

type MacroContext interface {
	Thread() *starlark.Thread
	Builder(name string) (InstallationPlanBuilder, error)
	AddBuilder(name string, builder InstallationPlanBuilder)
	Variable(name string) string
	AddVariable(name string, value string)
}

type Macro interface {
	Call(ctx MacroContext) (MacroResult, error)
}

type PackageDatabase interface {
	starlark.Value

	BuildDir() string
	FilenameFromHash(hash string, suffix string) (string, error)
	Build(ctx BuildContext, def BuildDefinition, opts BuildOptions) (filesystem.File, error)
	UrlsFor(url string) ([]string, error)
	HttpClient() (*http.Client, error)
	ShouldRebuildUserDefinitions() bool
	SetRebuildUserDefinitions(rebuild bool)
	GetContainerBuilder(ctx BuildContext, name string, arch config.CPUArchitecture) (ContainerBuilder, error)
	GetBuilder(filename string, builder string) (starlark.Callable, error)
	NewThread(filename string) *starlark.Thread
	HashDefinition(def BuildDefinition) (string, error)
	NewBuildContext(source BuildSource) BuildContext
	GetContainerBuilders() map[string]ContainerBuilder
	NewMacroContext() MacroContext
	GetMacroByShorthand(ctx MacroContext, shorthand string, allowLocal bool) (Macro, error)
	RunScript(filename string, files map[string]filesystem.File, additionalArgs []string, outputFilename string) error
	GetDefinitionByHash(hash string) (BuildDefinition, error)
	Inspect(def BuildDefinition, out io.Writer) error
	SetDistributionServer(server string) error
	RunDistributionServer(addr string) error
	LoadBuiltinBuilders() error
	AddMirror(name string, options []string) error
	GetAllHashes() ([]string, error)
}

type InstallationPlanBuilder interface {
	starlark.Value
}
