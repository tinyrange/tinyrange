package pubgrub

import "testing"

func TestSolveConflict_IconsDropdown(t *testing.T) {
    prov := demoUniverse()
    cmp := semverCmp{}
    prior := FewestVersionsPrioritizer{Prov: prov, Cmp: cmp}
    chooser := NewestFirstChooser{Prov: prov, Cmp: cmp}
    s := NewSolver(prov, cmp, prior, chooser)

    // dropdown >=2.0.0 and icons <2.0.0 conflict via dropdown 2.0.0 -> icons >=2.0.0
    roots := []Constraint{
        {Pkg: "dropdown", Allow: Interval(semver{2,0,0,""}, true, nil, false)},
        {Pkg: "icons", Allow: Interval(nil, false, semver{2,0,0,""}, false)},
    }
    _, err := s.Solve(roots)
    if err == nil {
        t.Fatalf("expected conflict error, got success")
    }
}

func TestSolveSuccess_IconsDropdown(t *testing.T) {
    prov := demoUniverse()
    cmp := semverCmp{}
    prior := FewestVersionsPrioritizer{Prov: prov, Cmp: cmp}
    chooser := NewestFirstChooser{Prov: prov, Cmp: cmp}
    s := NewSolver(prov, cmp, prior, chooser)

    // dropdown >=2.0.0 and icons >=2.0.0 is consistent
    roots := []Constraint{
        {Pkg: "dropdown", Allow: Interval(semver{2,0,0,""}, true, nil, false)},
        {Pkg: "icons", Allow: Interval(semver{2,0,0,""}, true, nil, false)},
    }
    sol, err := s.Solve(roots)
    if err != nil {
        t.Fatalf("expected success, got error: %v", err)
    }
    if got := sol["dropdown"].String(); got != "2.0.0" {
        t.Fatalf("expected dropdown 2.0.0, got %s", got)
    }
    if got := sol["icons"].String(); got != "2.0.0" {
        t.Fatalf("expected icons 2.0.0, got %s", got)
    }
}

func TestSolveAddsDependencies(t *testing.T) {
    prov := demoUniverse()
    cmp := semverCmp{}
    prior := FewestVersionsPrioritizer{Prov: prov, Cmp: cmp}
    chooser := NewestFirstChooser{Prov: prov, Cmp: cmp}
    s := NewSolver(prov, cmp, prior, chooser)

    // Only constrain dropdown >=2.0.0. Correct solvers include icons 2.0.0 via dependency.
    roots := []Constraint{
        {Pkg: "dropdown", Allow: Interval(semver{2,0,0,""}, true, nil, false)},
    }
    sol, err := s.Solve(roots)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if _, ok := sol["icons"]; !ok {
        t.Fatalf("expected icons to be part of solution via dependency")
    }
    if got := sol["icons"].String(); got != "2.0.0" {
        t.Fatalf("expected icons 2.0.0, got %s", got)
    }
}

// Backtracking scenario: Newest-first picks X 2.0.0 (needs A >=2), but Y needs A <2.
// The solver should backtrack and pick X 1.0.0 (needs A <2), succeeding overall.
func TestBacktrackToAlternateVersion_Succeeds(t *testing.T) {
    // no skip; exercise backtracking with proper resolution
    // Build custom universe
    cmp := semverCmp{}
    m := &memProvider{
        versions: map[PackageID][]Version{},
        deps:     map[PackageID]map[string][]Constraint{},
        yanked:   map[PackageID]map[string]bool{},
        cmp:      cmp,
    }
    // A: 1.0.0, 2.0.0
    m.versions["A"] = []Version{semver{1,0,0,""}, semver{2,0,0,""}}
    // X: 1.0.0 -> A <2 ; 2.0.0 -> A >=2
    m.versions["X"] = []Version{semver{1,0,0,""}, semver{2,0,0,""}}
    m.deps["X"] = map[string][]Constraint{
        "1.0.0": {{Pkg: "A", Allow: Interval(nil, false, semver{2,0,0,""}, false)}},
        "2.0.0": {{Pkg: "A", Allow: Interval(semver{2,0,0,""}, true, nil, false)}},
    }
    // Y: 1.0.0 -> A <2
    m.versions["Y"] = []Version{semver{1,0,0,""}}
    m.deps["Y"] = map[string][]Constraint{
        "1.0.0": {{Pkg: "A", Allow: Interval(nil, false, semver{2,0,0,""}, false)}},
    }

    prior := FewestVersionsPrioritizer{Prov: m, Cmp: cmp}
    chooser := NewestFirstChooser{Prov: m, Cmp: cmp}
    s := NewSolver(m, cmp, prior, chooser)

    roots := []Constraint{
        {Pkg: "X", Allow: Any()},
        {Pkg: "Y", Allow: Any()},
    }
    sol, err := s.Solve(roots)
    if err != nil {
        t.Fatalf("expected success after backtracking, got error: %v", err)
    }
    if sol["X"].String() != "1.0.0" || sol["A"].String() != "1.0.0" {
        t.Fatalf("expected X=1.0.0 and A=1.0.0, got X=%s A=%s", sol["X"].String(), sol["A"].String())
    }
}
