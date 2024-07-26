package common

import (
	"net/http"

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

	Directives() []Directive
	SetDirectives(directives []Directive)
	WriteTree() error
}

type ContainerBuilder interface {
	starlark.Value

	Plan(db PackageDatabase, packages []PackageQuery, tags TagList, opts PlanOptions) (InstallationPlan, error)
	Search(pkg PackageQuery) ([]*Package, error)
}

type PackageDatabase interface {
	starlark.Value

	GetBuildDir() string
	Build(ctx BuildContext, def BuildDefinition, opts BuildOptions) (filesystem.File, error)
	UrlsFor(url string) ([]string, error)
	HttpClient() (*http.Client, error)
	ShouldRebuildUserDefinitions() bool
	GetContainerBuilder(name string) (ContainerBuilder, error)
	GetBuilder(filename string, builder string) (starlark.Callable, error)
	NewThread(filename string) *starlark.Thread
}
