// This is not a full implemention of Pubgrub yet. This is a simple solver that uses a depth first search to find a solution.
// The solver is not complete and does not handle all cases. It is a starting point for a full implementation.

package pubgrub

import (
	"fmt"
	"slices"
	"strings"
)

type ErrNoSolutionFound struct {
	Term Term
}

func (e ErrNoSolutionFound) Error() string {
	return fmt.Sprintf("no solution found for %s", e.Term)
}

var (
	_ error = ErrNoSolutionFound{}
)

type Name string

type Version interface {
	String() string
	Sort(other Version) int
}

type Condition interface {
	String() string
	Satisfies(ver Version) bool
}

type EqualsCondition struct {
	Version Version
}

// String satisfies Condition.
func (c EqualsCondition) String() string {
	return fmt.Sprintf("== %s", c.Version)
}

// Satisfies satisfies Condition.
func (c EqualsCondition) Satisfies(ver Version) bool {
	return c.Version.String() == ver.String()
}

var (
	_ Condition = EqualsCondition{}
)

type Term struct {
	Name      Name
	Condition Condition
}

func (t Term) String() string {
	return fmt.Sprintf("%s %s", t.Name, t.Condition)
}

func NewTerm(name Name, condition Condition) Term {
	return Term{Name: name, Condition: condition}
}

type NameVersion struct {
	Name    Name
	Version Version
}

func (n NameVersion) String() string {
	return fmt.Sprintf("%s %s", n.Name, n.Version)
}

type Source interface {
	// GetVersions returns all versions of a package in a sorted order.
	GetVersions(name Name) ([]Version, error)
	GetDependencies(name Name, version Version) ([]Term, error)
}

type SimpleVersion string

// Sort implements Version.
func (v SimpleVersion) Sort(other Version) int {
	return strings.Compare(string(v), other.String())
}

// String satisfies Version.
func (v SimpleVersion) String() string {
	return string(v)
}

var (
	_ Version = SimpleVersion("")
)

type InMemorySource struct {
	Packages map[Name]map[Version][]Term
}

// GetVersions satisfies Source.
func (s *InMemorySource) GetVersions(name Name) ([]Version, error) {
	versions, ok := s.Packages[name]
	if !ok {
		return nil, fmt.Errorf("package %s not found", name)
	}

	var result []Version
	for v := range versions {
		result = append(result, v)
	}

	// sort the versions
	slices.SortFunc(result, func(a Version, b Version) int {
		return a.Sort(b)
	})

	return result, nil
}

// GetDependencies satisfies Source.
func (s *InMemorySource) GetDependencies(name Name, version Version) ([]Term, error) {
	versions, ok := s.Packages[name]
	if !ok {
		return nil, fmt.Errorf("package %s not found", name)
	}

	if _, ok := versions[version]; !ok {
		return nil, fmt.Errorf("package %s version %s not found", name, version)
	}

	return s.Packages[name][version], nil
}

// AddPackage adds a package to the source with a list of dependencies.
func (s *InMemorySource) AddPackage(name Name, version Version, deps []Term) {
	if s.Packages == nil {
		s.Packages = make(map[Name]map[Version][]Term)
	}

	if _, ok := s.Packages[name]; !ok {
		s.Packages[name] = make(map[Version][]Term)
	}

	s.Packages[name][version] = deps
}

type CombinedSource []Source

// GetVersions satisfies Source.
func (s CombinedSource) GetVersions(name Name) ([]Version, error) {
	var ret []Version
	for _, source := range s {
		versions, err := source.GetVersions(name)
		if err == nil {
			ret = append(ret, versions...)
		}
	}

	if len(ret) == 0 {
		return nil, fmt.Errorf("package %s not found", name)
	}

	// sort the versions
	slices.SortFunc(ret, func(a Version, b Version) int {
		return a.Sort(b)
	})

	// Reverse so the newest version is first.
	slices.Reverse(ret)

	return ret, nil
}

// GetDependencies satisfies Source.
func (s CombinedSource) GetDependencies(name Name, version Version) ([]Term, error) {
	for _, source := range s {
		deps, err := source.GetDependencies(name, version)
		if err == nil {
			return deps, nil
		}
	}

	return nil, fmt.Errorf("package %s version %s not found", name, version)
}

type RootSource []Term

// GetVersions satisfies Source.
func (s RootSource) GetVersions(name Name) ([]Version, error) {
	if name != "$$root" {
		return nil, fmt.Errorf("package %s not found", name)
	}

	return []Version{SimpleVersion("1")}, nil
}

// GetDependencies satisfies Source.
func (s RootSource) GetDependencies(name Name, version Version) ([]Term, error) {
	if name != "$$root" {
		return nil, fmt.Errorf("package %s not found", name)
	}

	if version != SimpleVersion("1") {
		return nil, fmt.Errorf("package %s version %s not found", name, version)
	}

	return s, nil
}

// AddPackage adds a single term to the source.
func (s *RootSource) AddPackage(name Name, condition Condition) {
	*s = append(*s, NewTerm(name, condition))
}

func (s *RootSource) Term() Term {
	return NewTerm("$$root", EqualsCondition{SimpleVersion("1")})
}

func NewRootSource() *RootSource {
	return &RootSource{}
}

var (
	_ Source = &InMemorySource{}
	_ Source = CombinedSource{}
)

type Solution []NameVersion

func (s Solution) GetVersion(name Name) (Version, bool) {
	for _, nv := range s {
		if nv.Name == name {
			return nv.Version, true
		}
	}

	return nil, false
}

// The inital version of the solver is very simple and implements a depth first search for a solution.
type Solver struct {
	Source Source
}

func (s *Solver) getVersions(t Term) ([]Version, error) {
	versions, err := s.Source.GetVersions(t.Name)
	if err != nil {
		return nil, err
	}

	var ret []Version

	for _, v := range versions {
		if t.Condition == nil || t.Condition.Satisfies(v) {
			ret = append(ret, v)
		}
	}

	return ret, nil
}

func (s *Solver) solve(next Term, partial Solution) (Solution, error) {
	// Check if the term is already in the partial solution.
	if _, ok := partial.GetVersion(next.Name); ok {
		return partial, nil
	}

	// Get a list of versions for the next term.
	versions, err := s.getVersions(next)
	if err != nil {
		return nil, err
	}

outer:
	for _, v := range versions {
		partial := append(partial, NameVersion{Name: next.Name, Version: v})

		// Get the dependencies for the version.
		deps, err := s.Source.GetDependencies(next.Name, v)
		if err != nil {
			return nil, err
		}

		for _, dep := range deps {
			if ver, ok := partial.GetVersion(dep.Name); ok {
				if !dep.Condition.Satisfies(ver) {
					// conflict found

					// slog.Info("conflict", "dep", dep, "ver", ver)

					continue outer
				}
			}

			newPartial, err := s.solve(dep, partial)
			if _, ok := err.(ErrNoSolutionFound); ok {
				continue outer
			} else if err != nil {
				return nil, err
			}

			if newPartial != nil {
				partial = newPartial
			}
		}

		return partial, nil
	}

	// slog.Info("no solution found", "next", next)

	return nil, ErrNoSolutionFound{Term: next}
}

func (s *Solver) Solve(root Term) (Solution, error) {
	return s.solve(root, Solution{})
}

func NewSolver(sources ...Source) *Solver {
	return &Solver{Source: CombinedSource(sources)}
}
