package common

import (
	"net/http"

	"github.com/tinyrange/pkg2/v2/filesystem"
)

type PackageDatabase interface {
	Build(ctx BuildContext, def BuildDefinition) (filesystem.File, error)
	UrlsFor(url string) ([]string, error)
	HttpClient() (*http.Client, error)
	ShouldRebuildUserDefinitions() bool
}
