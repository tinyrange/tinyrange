package common

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
)

type PackageQuery struct {
	Name    string
	Version string
}

func ParsePackageQuery(s string) (PackageQuery, error) {
	name, version, _ := strings.Cut(s, ":")

	return PackageQuery{Name: name, Version: version}, nil
}

type PackageName struct {
	Name    string
	Version string
	Tags    []string
}

func (name PackageName) Matches(query PackageQuery) bool {
	if query.Name != "" {
		if name.Name != query.Name {
			return false
		}
	}

	if query.Version != "" {
		if name.Version != query.Version {
			return false
		}
	}

	return true
}

func (name PackageName) String() string   { return fmt.Sprintf("%s:%s", name.Name, name.Version) }
func (PackageName) Type() string          { return "PackageName" }
func (PackageName) Hash() (uint32, error) { return 0, fmt.Errorf("PackageName is not hashable") }
func (PackageName) Truth() starlark.Bool  { return starlark.True }
func (PackageName) Freeze()               {}

var (
	_ starlark.Value = PackageName{}
)

type Package struct {
	Name       PackageName
	Directives []Directive
}

func (pkg *Package) Matches(query PackageQuery) bool {
	return pkg.Name.Matches(query)
}

func (pkg *Package) String() string    { return pkg.Name.String() }
func (*Package) Type() string          { return "Package" }
func (*Package) Hash() (uint32, error) { return 0, fmt.Errorf("Package is not hashable") }
func (*Package) Truth() starlark.Bool  { return starlark.True }
func (*Package) Freeze()               {}

var (
	_ starlark.Value = &Package{}
)

func NewPackage(name PackageName, directives []Directive) *Package {
	return &Package{Name: name, Directives: directives}
}
