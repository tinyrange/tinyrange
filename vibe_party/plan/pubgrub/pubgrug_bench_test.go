package pubgrub

import (
	"fmt"
	"testing"
)

// buildLinearUniverse builds N packages P0..P{n-1} each with two versions, where
// Pi 2.0.0 depends on P{i+1} >=2 (except last), and 1.0.0 depends on P{i+1} <2.
// Root constrains P0 Any. This forces dependency propagation through the chain.
func buildLinearUniverse(n int) (*memProvider, []Constraint) {
	cmp := semverCmp{}
	m := &memProvider{versions: map[PackageID][]Version{}, deps: map[PackageID]map[string][]Constraint{}, yanked: map[PackageID]map[string]bool{}, cmp: cmp}
	for i := 0; i < n; i++ {
		p := PackageID(fmt.Sprintf("P%d", i))
		m.versions[p] = []Version{semver{1, 0, 0, ""}, semver{2, 0, 0, ""}}
	}
	for i := 0; i < n-1; i++ {
		p := PackageID(fmt.Sprintf("P%d", i))
		next := PackageID(fmt.Sprintf("P%d", i+1))
		m.deps[p] = map[string][]Constraint{
			"1.0.0": {{Pkg: next, Allow: Interval(nil, false, semver{2, 0, 0, ""}, false)}},
			"2.0.0": {{Pkg: next, Allow: Interval(semver{2, 0, 0, ""}, true, nil, false)}},
		}
	}
	roots := []Constraint{{Pkg: "P0", Allow: Any()}}
	return m, roots
}

func BenchmarkSolveLinearChain_100(b *testing.B) {
	prov, roots := buildLinearUniverse(100)
	cmp := semverCmp{}
	prior := FewestVersionsPrioritizer{Prov: prov, Cmp: cmp}
	chooser := NewestFirstChooser{Prov: prov, Cmp: cmp}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := NewSolver(prov, cmp, prior, chooser)
		if _, err := s.Solve(roots); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// Branching universe: a binary tree of packages depth d; each node has 2 deps with
// opposite constraints to force propagation breadth.
func buildBranchingUniverse(depth int) (*memProvider, []Constraint) {
	cmp := semverCmp{}
	m := &memProvider{versions: map[PackageID][]Version{}, deps: map[PackageID]map[string][]Constraint{}, yanked: map[PackageID]map[string]bool{}, cmp: cmp}
	var id func(int, int) PackageID
	id = func(level, idx int) PackageID { return PackageID(fmt.Sprintf("N_%d_%d", level, idx)) }
	for lvl := 0; lvl <= depth; lvl++ {
		nodes := 1 << lvl
		for i := 0; i < nodes; i++ {
			m.versions[id(lvl, i)] = []Version{semver{1, 0, 0, ""}, semver{2, 0, 0, ""}}
		}
	}
	for lvl := 0; lvl < depth; lvl++ {
		nodes := 1 << lvl
		for i := 0; i < nodes; i++ {
			p := id(lvl, i)
			l := id(lvl+1, 2*i)
			r := id(lvl+1, 2*i+1)
			m.deps[p] = map[string][]Constraint{
				"1.0.0": {{Pkg: l, Allow: Interval(nil, false, semver{2, 0, 0, ""}, false)}, {Pkg: r, Allow: Interval(nil, false, semver{2, 0, 0, ""}, false)}},
				"2.0.0": {{Pkg: l, Allow: Interval(semver{2, 0, 0, ""}, true, nil, false)}, {Pkg: r, Allow: Interval(semver{2, 0, 0, ""}, true, nil, false)}},
			}
		}
	}
	roots := []Constraint{{Pkg: id(0, 0), Allow: Any()}}
	return m, roots
}

func BenchmarkSolveBranchingDepth_7(b *testing.B) {
	prov, roots := buildBranchingUniverse(7) // 255 nodes
	cmp := semverCmp{}
	prior := FewestVersionsPrioritizer{Prov: prov, Cmp: cmp}
	chooser := NewestFirstChooser{Prov: prov, Cmp: cmp}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := NewSolver(prov, cmp, prior, chooser)
		if _, err := s.Solve(roots); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}
