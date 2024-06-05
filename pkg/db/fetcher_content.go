package db

import (
	"fmt"

	"go.starlark.net/starlark"
)

type ContentFetcher struct {
	Func *starlark.Function
	Args starlark.Tuple
}

// Attr implements starlark.HasAttrs.
func (fetch *ContentFetcher) Attr(name string) (starlark.Value, error) {
	return nil, nil
}

// AttrNames implements starlark.HasAttrs.
func (fetch *ContentFetcher) AttrNames() []string {
	return []string{}
}

func (fetch *ContentFetcher) GetDefinition(db *PackageDatabase, pkg *Package, url string) *BuildDef {
	return &BuildDef{
		builder:    fetch.Func,
		args:       append(starlark.Tuple{pkg, starlark.String(url)}, fetch.Args...),
		privateTag: "fetchContents_" + url,
		Tag:        "fetch_contents_" + url,
	}
}

func (*ContentFetcher) String() string        { return "ContentFetcher" }
func (*ContentFetcher) Type() string          { return "ContentFetcher" }
func (*ContentFetcher) Hash() (uint32, error) { return 0, fmt.Errorf("ContentFetcher is not hashable") }
func (*ContentFetcher) Truth() starlark.Bool  { return starlark.True }
func (*ContentFetcher) Freeze()               {}

var (
	_ starlark.Value    = &ContentFetcher{}
	_ starlark.HasAttrs = &ContentFetcher{}
)
