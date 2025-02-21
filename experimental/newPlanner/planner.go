package main

import (
	"fmt"

	"github.com/google/uuid"
)

type virtualPackage struct {
	name PackageID
	deps []PackageQuery
}

func (p *virtualPackage) FindPackages(planner Planner, query PackageQuery) ([]PackageID, error) {
	if query.Name == string(p.name) {
		return []PackageID{p.name}, nil
	}
	return nil, nil
}

func (p *virtualPackage) GetPackageDependencies(id PackageID) ([]PackageQuery, error) {
	if id == p.name {
		return p.deps, nil
	}
	return nil, ErrPackageNotFound
}

func (p *virtualPackage) GetPackageDirectives(id PackageID) ([]Directive, error) {
	return nil, nil
}

var (
	_ PackageSource = &virtualPackage{}
)

type planner struct {
	sources         []PackageSource
	compareVersions func(a, b string) int

	currentPackages map[PackageID]PackageSource
	directives      []Directive
}

func (p *planner) addPackage(source PackageSource, id PackageID) error {
	return fmt.Errorf("not implemented")
}

// AddPackages implements Planner.
func (p *planner) AddPackages(packages []PackageQuery) error {
	id := PackageID(uuid.NewString())

	return p.addPackage(&virtualPackage{
		name: id,
		deps: packages,
	}, id)
}

// AddSource implements Planner.
func (p *planner) AddSource(source PackageSource) error {
	p.sources = append(p.sources, source)
	return nil
}

// CompareVersions implements Planner.
func (p *planner) CompareVersions(a string, b string) int {
	return p.compareVersions(a, b)
}

// Plan implements Planner.
func (p *planner) Plan() ([]Directive, error) {
	return p.directives, nil
}

var (
	_ Planner = &planner{}
)

func NewPlanner(compare func(a, b string) int) Planner {
	return &planner{
		compareVersions: compare,
	}
}
