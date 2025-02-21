package main

import "errors"

var ErrPackageNotFound = errors.New("package not found")

type PackageID string

type Condition interface {
	SatisfiedByVersion(planner Planner, v string) bool
}

type PackageQuery struct {
	Name      string
	Condition Condition
}

type Directive interface {
	Execute() error
}

type PackageSource interface {
	// Find packages that match the query
	FindPackages(planner Planner, query PackageQuery) ([]PackageID, error)
	// Get the dependencies of a package
	GetPackageDependencies(id PackageID) ([]PackageQuery, error)
	// Get the directives that should be executed to install the package
	GetPackageDirectives(id PackageID) ([]Directive, error)
}

type Planner interface {
	// Compare two versions of a package. Returns -1 if a < b, 0 if a == b, 1 if a > b
	CompareVersions(a, b string) int
	// Add a source of packages to the planner
	AddSource(source PackageSource) error
	// Add a set of queries to the planner. Returns an error if the query cannot be satisfied
	AddPackages(packages []PackageQuery) error
	// Return a list of directives that should be executed to install the packages
	Plan() ([]Directive, error)
}
