package common

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
)

type PackageQuery struct {
	MatchDirect      bool
	Name             string
	MatchPartialName bool
	Version          string
}

func (q PackageQuery) Equals(n PackageName) bool {
	return q.Name == n.Name && q.Version == n.Version
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
	if s == "*" {
		return PackageQuery{}, nil
	}

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
	if query.MatchPartialName && query.Name == "" {
		return true
	}

	if query.Name != "" {
		if query.MatchPartialName {
			return strings.Contains(name.Name, query.Name)
		} else {
			if name.Name != query.Name {
				return false
			}
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

// Attr implements starlark.HasAttrs.
func (i *Installer) Attr(name string) (starlark.Value, error) {
	if name == "directives" {
		var ret []starlark.Value

		for _, directive := range i.Directives {
			if val, ok := directive.(starlark.Value); ok {
				ret = append(ret, val)
			} else {
				ret = append(ret, &StarDirective{Directive: directive})
			}
		}

		return starlark.NewList(ret), nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (i *Installer) AttrNames() []string {
	return []string{"directives"}
}

func (*Installer) String() string        { return "Installer" }
func (*Installer) Type() string          { return "Installer" }
func (*Installer) Hash() (uint32, error) { return 0, fmt.Errorf("Installer is not hashable") }
func (*Installer) Truth() starlark.Bool  { return starlark.True }
func (*Installer) Freeze()               {}

var (
	_ starlark.Value    = &Installer{}
	_ starlark.HasAttrs = &Installer{}
)

func NewInstaller(tagList TagList, directives []Directive, dependencies []PackageQuery) *Installer {
	return &Installer{Tags: tagList, Directives: directives, Dependencies: dependencies}
}

type Package struct {
	Name    PackageName
	Aliases []PackageName
	Raw     starlark.Value
}

// Attr implements starlark.HasAttrs.
func (pkg *Package) Attr(name string) (starlark.Value, error) {
	if name == "raw" {
		return pkg.Raw, nil
	} else {
		return nil, nil
	}
}

// AttrNames implements starlark.HasAttrs.
func (pkg *Package) AttrNames() []string {
	return []string{"raw"}
}

func (pkg *Package) Matches(query PackageQuery) bool {
	if pkg.Name.Matches(query) {
		return true
	}

	if query.MatchDirect {
		return false
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
	_ starlark.Value    = &Package{}
	_ starlark.HasAttrs = &Package{}
)

func NewPackage(name PackageName, aliases []PackageName, raw starlark.Value) *Package {
	return &Package{Name: name, Aliases: aliases, Raw: raw}
}
