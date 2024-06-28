package common

import (
	"net/http"

	"github.com/tinyrange/pkg2/v2/filesystem"
)

type BuildOptions struct {
	AlwaysRebuild bool
}

type PackageDatabase interface {
	Build(ctx BuildContext, def BuildDefinition, opts BuildOptions) (filesystem.File, error)
	UrlsFor(url string) ([]string, error)
	HttpClient() (*http.Client, error)
	ShouldRebuildUserDefinitions() bool
}
