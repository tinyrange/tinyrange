package planner2

import (
	"testing"
)

type testPackage struct {
	name         PackageName
	aliases      []PackageName
	installers   []testPackage
	tags         TagList
	directives   []Directive
	conflicts    []PackageQuery
	dependencies []PackageOptions
}

// implements Installer.
func (t testPackage) Name() PackageName                       { return t.name }
func (t testPackage) Aliases() []PackageName                  { return t.aliases }
func (t testPackage) Tags() TagList                           { return t.tags }
func (t testPackage) Conflicts() ([]PackageQuery, error)      { return t.conflicts, nil }
func (t testPackage) Dependencies() ([]PackageOptions, error) { return t.dependencies, nil }
func (t testPackage) Directives() ([]Directive, error)        { return t.directives, nil }

// Installers implements Package.
func (t testPackage) Installers() ([]Installer, error) {
	var ret []Installer

	for _, installer := range t.installers {
		installer.name = t.name
		installer.aliases = t.aliases

		ret = append(ret, installer)
	}

	return ret, nil
}

var (
	_ Package   = testPackage{}
	_ Installer = testPackage{}
)

type testSource []testPackage

// Find implements PackageSource.
func (t testSource) Find(q PackageQuery) ([]Package, error) {
	var ret []Package

	for _, pkg := range t {
		match, err := pkg.Name().Matches(q)
		if err != nil {
			return nil, err
		}

		if match == MatchResultMatched {
			ret = append(ret, pkg)
		}
	}

	return ret, nil
}

var (
	_ PackageSource = testSource{}
)

var testPkgSource = testSource{
	testPackage{
		name: NewName("hello", "world"),
		installers: []testPackage{
			{tags: TagList{"hello"}},
		},
	},

	testPackage{
		name: NewName("hello2", "world"),
		installers: []testPackage{
			{
				tags: TagList{"hello"},
				dependencies: []PackageOptions{
					NewPackageOptions(NewPackageQuery("hello")),
				},
			},
		},
	},
}

func Test(t *testing.T) {
	plan := NewPlan([]PackageSource{testPkgSource}, TagList{"hello"})

	if err := plan.Add(NewPackageOptions(NewPackageQuery("hello2"))); err != nil {
		t.Fatal(err)
	}
}
