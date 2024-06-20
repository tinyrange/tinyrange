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

func (q PackageQuery) String() string      { return fmt.Sprintf("%s:%s", q.Name, q.Version) }
func (PackageQuery) Type() string          { return "PackageQuery" }
func (PackageQuery) Hash() (uint32, error) { return 0, fmt.Errorf("PackageQuery is not hashable") }
func (PackageQuery) Truth() starlark.Bool  { return starlark.True }
func (PackageQuery) Freeze()               {}

var (
	_ starlark.Value = PackageQuery{}
)

func ParsePackageQuery(s string) (PackageQuery, error) {
	name, version, _ := strings.Cut(s, ":")

	return PackageQuery{Name: name, Version: version}, nil
}

type PackageName struct {
	Name    string
	Version string
	Tags    []string
}

func (name PackageName) Query() PackageQuery {
	return PackageQuery{
		Name:    name.Name,
		Version: name.Version,
	}
}

func (name PackageName) Matches(query PackageQuery) bool {
	if query.Name != "" {
		if name.Name != query.Name {
			return false
		}
	}

	// if query.Version != "" {
	// 	if name.Version != query.Version {
	// 		return false
	// 	}
	// }

	return true
}

func (name PackageName) Key() string {
	return name.String()
}

func (name PackageName) String() string   { return fmt.Sprintf("%s:%s", name.Name, name.Version) }
func (PackageName) Type() string          { return "PackageName" }
func (PackageName) Hash() (uint32, error) { return 0, fmt.Errorf("PackageName is not hashable") }
func (PackageName) Truth() starlark.Bool  { return starlark.True }
func (PackageName) Freeze()               {}

var (
	_ starlark.Value = PackageName{}
)

type Installer struct {
	Tags         TagList
	Directives   []Directive
	Dependencies []PackageQuery
}

func (*Installer) String() string        { return "Installer" }
func (*Installer) Type() string          { return "Installer" }
func (*Installer) Hash() (uint32, error) { return 0, fmt.Errorf("Installer is not hashable") }
func (*Installer) Truth() starlark.Bool  { return starlark.True }
func (*Installer) Freeze()               {}

var (
	_ starlark.Value = &Package{}
)

func NewInstaller(tagList TagList, directives []Directive, dependencies []PackageQuery) *Installer {
	return &Installer{Tags: tagList, Directives: directives, Dependencies: dependencies}
}

type Package struct {
	Name       PackageName
	Installers []*Installer
	Aliases    []PackageName
	Raw        string
}

func (pkg *Package) Matches(query PackageQuery) bool {
	if pkg.Name.Matches(query) {
		return true
	}

	for _, alias := range pkg.Aliases {
		if alias.Matches(query) {
			return true
		}
	}

	return false
}

func (pkg *Package) String() string    { return pkg.Name.String() }
func (*Package) Type() string          { return "Package" }
func (*Package) Hash() (uint32, error) { return 0, fmt.Errorf("Package is not hashable") }
func (*Package) Truth() starlark.Bool  { return starlark.True }
func (*Package) Freeze()               {}

var (
	_ starlark.Value = &Package{}
)

func NewPackage(name PackageName, installers []*Installer, aliases []PackageName, raw string) *Package {
	return &Package{Name: name, Installers: installers, Aliases: aliases, Raw: raw}
}
