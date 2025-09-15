// pubgrub.go
// A minimal, deterministic, single-file Go implementation of PubGrub,
// with pluggable version semantics and an embedded CLI demo.
// No filesystem or network I/O; all inputs are provided via interfaces.
//
// References consulted while implementing the algorithm and interfaces:
// - Dart Pub solver doc (CDCL, incompatibilities, explanations)
// - pubgrub-rs docs and guide (DependencyProvider, VersionSet split)
// - Original PubGrub article (intuition + error reporting style)
//
// See: https://github.com/dart-lang/pub/blob/master/doc/solver.md
//      https://pubgrub-rs-guide.pages.dev/internals/intro
//      https://docs.rs/pubgrub/latest/pubgrub/solver/trait.DependencyProvider.html
//      https://docs.rs/pubgrub
//
// CLI demo:
//   go run pubgrub.go resolve root@">=1.0 <3.0"

package pubgrub

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

////////////////////////////////////////////////////////////////////////////////
// Core abstractions (pluggable for Conda)
//
// You can swap these with Conda semantics (e.g., PEP 440-like comparison,
// conda-spec ranges, markers) by implementing Comparator and Range, and by
// providing a Provider that yields package versions and their constraints.
////////////////////////////////////////////////////////////////////////////////

// PackageID uniquely identifies a package in the universe. Leave source
// attribution to higher layers; this core only uses the opaque ID.
type PackageID string

// Version is an opaque value; the solver uses Comparator/Range to reason about it.
type Version interface {
	String() string
}

// Comparator compares two Version values (a, b).
// Return -1 if a<b, 0 if equal, +1 if a>b (like strings.Compare).
type Comparator interface {
	Compare(a, b Version) int
}

// Range represents a set of versions (e.g., >=1.2,<2.0).
// Intersect returns the logical intersection; IsEmpty detects contradictions.
type Range interface {
	Contains(v Version, cmp Comparator) bool
	Intersect(other Range, cmp Comparator) Range
	IsEmpty() bool
	String() string
}

// Constraint ties a dependency to a Range.
type Constraint struct {
	Pkg   PackageID
	Allow Range
	Note  string // optional: for better explanations
}

// Provider exposes the dependency universe. It must be deterministic.
type Provider interface {
	// ListVersions returns ALL versions known for pkg, sorted descending by preferred order
	// for selection (the solver will still enforce determinism). Sorting direction can be
	// chosen by you; solver uses Comparator for actual comparisons/filters.
	ListVersions(pkg PackageID) ([]Version, error)

	// Dependencies returns the constraints introduced by selecting (pkg@ver).
	Dependencies(pkg PackageID, ver Version) ([]Constraint, error)

	// Optional: tell if a version is “yanked” (still selectable if explicitly allowed).
	// Return false if you don't care.
	IsYanked(pkg PackageID, ver Version) bool
}

////////////////////////////////////////////////////////////////////////////////
// PubGrub data model (terms, incompatibilities, derivations)
////////////////////////////////////////////////////////////////////////////////

// Term represents an assertion about a package’s version domain.
// In PubGrub literature, terms are sign-bearing (positive/negative).
type Term struct {
	Pkg PackageID
	// rangeAllowed describes the *allowed* versions for positive terms.
	// A negative term is encoded as "not in range": we represent it via a
	// special flag, keeping the Range as the disallowed set.
	Allowed  Range
	Positive bool
}

func (t Term) String() string {
	if t.Positive {
		return fmt.Sprintf("%s in %s", t.Pkg, t.Allowed)
	}
	return fmt.Sprintf("%s not in %s", t.Pkg, t.Allowed)
}

// Incompatibility is a disjunction of negative statements (a clause):
// "not t1 or not t2 or ..." which reads as "t1 and t2 cannot all be true".
type Incompatibility struct {
	Terms []Term
	Cause Cause
}

func (ic Incompatibility) String() string {
	ts := make([]string, len(ic.Terms))
	for i, t := range ic.Terms {
		if t.Positive {
			// PubGrub usually prints positive terms in requirements; here we
			// phrase as “because X requires Y in R…”
			ts[i] = t.String()
		} else {
			ts[i] = t.String()
		}
	}
	return strings.Join(ts, " or ")
}

// Cause tracks why we learned an incompatibility.
type Cause interface {
	isCause()
	String() string
}

// CauseDependency says “pkg@v implies constraints”, which become terms.
type CauseDependency struct {
	Pkg PackageID
	Ver Version
}

func (CauseDependency) isCause() {}
func (c CauseDependency) String() string {
	return fmt.Sprintf("because %s@%s has those dependencies", c.Pkg, c.Ver.String())
}

// CauseExternal is a root/user constraint.
type CauseExternal struct {
	Reason string
}

func (CauseExternal) isCause() {}
func (c CauseExternal) String() string {
	if c.Reason == "" {
		return "from root constraints"
	}
	return c.Reason
}

// CauseConflict is derived via resolution of conflicting clauses.
type CauseConflict struct {
	Conflict *Incompatibility
	Other    *Incompatibility
}

func (CauseConflict) isCause() {}
func (c CauseConflict) String() string {
	return "due to conflict resolution"
}

////////////////////////////////////////////////////////////////////////////////
// Partial solution (decisions & derived assignments)
////////////////////////////////////////////////////////////////////////////////

type assignmentKind int

const (
	assignDecision assignmentKind = iota + 1
	assignDerivation
)

type assignment struct {
	Pkg       PackageID
	Allowed   Range
	Kind      assignmentKind
	DecisionN int              // decision level index (1..)
	Reason    *Incompatibility // nil for root decisions; non-nil for derivations
}

type partialSolution struct {
	// For each package, the current allowed range.
	allowed map[PackageID]Range
	// ordered trail of assignments; each with a decision level.
	trail []assignment
	// index of current decision level (0 = before any decisions)
	level int
	// satisfiers records which assignment satisfied a given term at the time
	// it became true. Organized per package to keep lookups compact.
	// Key format: pkg:range:polarity where polarity is true for positive.
	satisfiers map[PackageID]map[string]Satisfier
}

func newPartialSolution() *partialSolution {
	return &partialSolution{
		allowed:    map[PackageID]Range{},
		trail:      []assignment{},
		level:      0,
		satisfiers: map[PackageID]map[string]Satisfier{},
	}
}

// Satisfier describes the assignment/incompatibility that made a term true
// during propagation.
type Satisfier struct {
	By         *Incompatibility
	Pivot      PackageID
	TrailIndex int
}

// termKey builds a stable key for satisfier tracking.
func termKey(t Term) string {
	return fmt.Sprintf("%s:%s:%t", t.Pkg, rangeString(t.Allowed), t.Positive)
}

// recordSatisfier registers that term t became satisfied due to the given
// incompatibility and pivot at the provided trail index.
func (ps *partialSolution) recordSatisfier(t Term, trailIndex int, by *Incompatibility, pivot PackageID) {
	if ps.satisfiers == nil {
		ps.satisfiers = map[PackageID]map[string]Satisfier{}
	}
	m := ps.satisfiers[t.Pkg]
	if m == nil {
		m = map[string]Satisfier{}
		ps.satisfiers[t.Pkg] = m
	}
	k := termKey(t)
	// Only record the first satisfier to preserve the earliest cause at this state.
	if _, exists := m[k]; !exists {
		m[k] = Satisfier{By: by, Pivot: pivot, TrailIndex: trailIndex}
	}
}

// satisfierOf returns the recorded satisfier for term t, if any.
func (ps *partialSolution) satisfierOf(t Term) (Satisfier, bool) {
	if ps.satisfiers == nil {
		return Satisfier{}, false
	}
	m := ps.satisfiers[t.Pkg]
	if m == nil {
		return Satisfier{}, false
	}
	s, ok := m[termKey(t)]
	return s, ok
}

func (ps *partialSolution) currentAllowed(pkg PackageID) (Range, bool) {
	r, ok := ps.allowed[pkg]
	return r, ok
}

func (ps *partialSolution) decide(pkg PackageID, r Range) {
	ps.level++
	as := assignment{Pkg: pkg, Allowed: r, Kind: assignDecision, DecisionN: ps.level}
	ps.trail = append(ps.trail, as)
	ps.allowed[pkg] = r
}

func (ps *partialSolution) derive(pkg PackageID, r Range, reason *Incompatibility) {
	as := assignment{Pkg: pkg, Allowed: r, Kind: assignDerivation, DecisionN: ps.level, Reason: reason}
	ps.trail = append(ps.trail, as)
	ps.allowed[pkg] = r
}

func (ps *partialSolution) backtrack(toLevel int) {
	if toLevel < 0 {
		toLevel = 0
	}
	// Remove assignments above toLevel.
	newTrail := ps.trail[:0]
	newAllowed := map[PackageID]Range{}
	for _, as := range ps.trail {
		if as.DecisionN <= toLevel {
			newTrail = append(newTrail, as)
			newAllowed[as.Pkg] = as.Allowed
		}
	}
	ps.trail = newTrail
	ps.allowed = newAllowed
	ps.level = toLevel
}

////////////////////////////////////////////////////////////////////////////////
// Solver policy hooks
////////////////////////////////////////////////////////////////////////////////

type Prioritizer interface {
	// ChooseNextPackage returns an unresolved package to decide on.
	ChooseNextPackage(allowed map[PackageID]Range, decided map[PackageID]bool) (PackageID, bool)
}

// VersionChooser lets you inject custom tie-breakers for candidate versions.
type VersionChooser interface {
	// CandidateVersions filters and orders the candidate versions for pkg,
	// given current Allowed range (if any) and Provider’s full list.
	CandidateVersions(pkg PackageID, allowed Range, all []Version, cmp Comparator) []Version
}

////////////////////////////////////////////////////////////////////////////////
// Solver (PubGrub core)
////////////////////////////////////////////////////////////////////////////////

type Solver struct {
	Prov      Provider
	Cmp       Comparator
	Prior     Prioritizer
	Chooser   VersionChooser
	Explainer *Explainer // pretty reports
	// Incompatibilities learned so far (monotonic growth).
	incs []*Incompatibility
}

func NewSolver(p Provider, cmp Comparator, prior Prioritizer, choose VersionChooser) *Solver {
	return &Solver{
		Prov:      p,
		Cmp:       cmp,
		Prior:     prior,
		Chooser:   choose,
		Explainer: &Explainer{},
		incs:      []*Incompatibility{},
	}
}

func (s *Solver) addIncompatibility(ic *Incompatibility) {
	s.incs = append(s.incs, ic)
}

// Solve attempts to satisfy root constraints.
func (s *Solver) Solve(roots []Constraint) (map[PackageID]Version, error) {
	ps := newPartialSolution()

	// Seed external constraints as "incompatibilities" against the root.
	// For each root constraint "pkg in R", record an incompatibility equivalent
	// to forbidding pkg outside R. We encode as term "pkg not in R" with CauseExternal.
	for _, c := range roots {
		ic := &Incompatibility{
			Terms: []Term{{Pkg: c.Pkg, Allowed: c.Allow, Positive: true}},
			Cause: CauseExternal{Reason: "root constraint"},
		}
		// A positive root "pkg in R" becomes active when resolution intersects.
		// We'll handle it through propagation.
		s.addIncompatibility(ic)
	}

	// Main loop: propagate → decide → repeat.
	decided := map[PackageID]bool{}
	steps := 0
	for {
        steps++
        if steps > 2000 {
            // Loop safeguard: forbid latest decision and continue search
            latestDec := -1
            for i := len(ps.trail) - 1; i >= 0; i-- {
                if ps.trail[i].Kind == assignDecision {
                    latestDec = i
                    break
                }
            }
            if latestDec == -1 {
                return nil, errors.New("resolution aborted: too many steps (possible loop)")
            }
            as := ps.trail[latestDec]
            if sn, ok := as.Allowed.(singletonRange); ok {
                learned := &Incompatibility{Terms: []Term{{Pkg: as.Pkg, Allowed: sn, Positive: false}}, Cause: CauseExternal{Reason: "loop-guard forbid"}}
                s.addIncompatibility(learned)
                ps.backtrack(as.DecisionN - 1)
                for k := range decided { delete(decided, k) }
                for _, d := range ps.trail { if d.Kind == assignDecision { decided[d.Pkg] = true } }
                steps = 0
                continue
            }
            return nil, errors.New("resolution aborted: too many steps (non-singleton decision)")
        }
		conflict := s.unitPropagate(ps)
		if conflict != nil {
			backLevel, learned := s.resolveConflict(ps, conflict)
			// Guard against negative backjump in ambiguous cases: treat as back to 0
			// and continue searching unless truly unsatisfiable by exhaustion.
			if backLevel < 0 {
				backLevel = 0
			}
			s.addIncompatibility(learned)
			ps.backtrack(backLevel)
			// Recompute decided set after backtracking to avoid stale entries.
			for k := range decided {
				delete(decided, k)
			}
			for _, as := range ps.trail {
				if as.Kind == assignDecision {
					decided[as.Pkg] = true
				}
			}
			continue
		}

		// Check if all packages implicated by constraints are decided and pinned.
		if s.isComplete(ps) {
			// Extract concrete versions from allowed ranges by picking unique version.
			solution := map[PackageID]Version{}
			for pkg, r := range ps.allowed {
				vers, _ := s.Prov.ListVersions(pkg)
				cands := filterByRange(vers, r, s.Cmp)
				if len(cands) == 0 {
					// Shouldn’t happen if complete; treat as failure.
					return nil, fmt.Errorf("internal: empty candidate set for %s", pkg)
				}
				// Deterministically pick the *first* candidate from chooser order.
				ordered := s.Chooser.CandidateVersions(pkg, r, cands, s.Cmp)
				if len(ordered) == 0 {
					return nil, fmt.Errorf("internal: chooser removed all candidates for %s", pkg)
				}
				solution[pkg] = ordered[0]
			}
			return solution, nil
		}

		// Decision: prefer undecided roots first, then prioritizer suggestion, then any unresolved.
		var nextPkg PackageID
		picked := false
		// roots preference
		if len(roots) > 0 {
			ids := make([]string, 0, len(roots))
			for _, r := range roots {
				ids = append(ids, string(r.Pkg))
			}
			sort.Strings(ids)
			for _, id := range ids {
				pid := PackageID(id)
				if !decided[pid] {
					nextPkg = pid
					picked = true
					break
				}
			}
		}
		if !picked {
			if id, ok := s.Prior.ChooseNextPackage(ps.allowed, decided); ok {
				nextPkg = id
				picked = true
			}
		}
		if !picked {
			nextPkg = s.pickAnyUnresolved(ps.allowed, decided)
			if nextPkg == "" {
				return nil, errors.New("no packages to decide on")
			}
		}

		// Choose a candidate version set for decision (as a range containing one version).
		vers, err := s.Prov.ListVersions(nextPkg)
		if err != nil {
			return nil, err
		}
		var allowed Range
		if r, ok := ps.currentAllowed(nextPkg); ok {
			allowed = r
		} else {
			// If no prior constraints, allow “any”. Our demo Range implementations will
			// expose a helper Any() via a type assertion; in generic code, a Provider
			// could inject an initial constraint via roots.
			allowed = anyRange{}
		}
		cands := filterByRange(vers, allowed, s.Cmp)
		// Exclude versions forbidden by learned single-literal negative clauses for this package.
		if len(s.incs) > 0 {
			banned := map[string]bool{}
			for _, ic := range s.incs {
				if len(ic.Terms) == 1 {
					t := ic.Terms[0]
					if t.Pkg == nextPkg && !t.Positive {
						if sn, ok := t.Allowed.(singletonRange); ok && sn.V != nil {
							banned[sn.V.String()] = true
						}
					}
				}
			}
			if len(banned) > 0 {
				filtered := make([]Version, 0, len(cands))
				for _, v := range cands {
					if !banned[v.String()] {
						filtered = append(filtered, v)
					}
				}
				cands = filtered
			}
		}
		cands = s.Chooser.CandidateVersions(nextPkg, allowed, cands, s.Cmp)
		if len(cands) == 0 {
			// No possible version for nextPkg under current decisions.
			// Learn a clause that forbids the latest decision to force backtracking.
			// If no decisions have been made, it's unsatisfiable.
			latestDec := -1
			for i := len(ps.trail) - 1; i >= 0; i-- {
				if ps.trail[i].Kind == assignDecision {
					latestDec = i
					break
				}
			}
			if latestDec == -1 {
				conf := &Incompatibility{Terms: []Term{{Pkg: nextPkg, Allowed: emptyRange{}, Positive: true}}, Cause: CauseExternal{Reason: "no viable version for decision"}}
				return nil, s.Explainer.ErrorFromConflict(conf)
			}
			as := ps.trail[latestDec]
			if sn, ok := as.Allowed.(singletonRange); ok {
				learned := &Incompatibility{Terms: []Term{{Pkg: as.Pkg, Allowed: sn, Positive: false}}, Cause: CauseExternal{Reason: "backtrack from no candidates"}}
				s.addIncompatibility(learned)
				bj := as.DecisionN - 1
				ps.backtrack(bj)
				// Recompute decided after backtrack
				for k := range decided {
					delete(decided, k)
				}
				for _, d := range ps.trail {
					if d.Kind == assignDecision {
						decided[d.Pkg] = true
					}
				}
				continue
			}
			// If decision wasn't singleton (shouldn't happen), fallback to unsat report.
			conf := &Incompatibility{Terms: []Term{{Pkg: nextPkg, Allowed: emptyRange{}, Positive: true}}, Cause: CauseExternal{Reason: "no viable version for decision"}}
			return nil, s.Explainer.ErrorFromConflict(conf)
		}
		// Decide the *first* candidate deterministically by chooser’s order.
		chosen := cands[0]
		// Decision is a singleton range [v].
		ps.decide(nextPkg, singletonRange{V: chosen})
		// Learn dependencies of this concrete version as incompatibilities.
		s.learnDependencies(nextPkg, chosen)
		decided[nextPkg] = true
	}
}

// learnDependencies records, for a selected concrete version pkg@ver, the
// incompatibilities representing its dependencies: (pkg in [ver]) and (dep in R).
func (s *Solver) learnDependencies(pkg PackageID, ver Version) {
	deps, err := s.Prov.Dependencies(pkg, ver)
	if err != nil {
		return
	}
	for _, d := range deps {
		// Encode implication: (pkg@[ver]) -> (dep in R) as (not pkg@[ver]) or (dep in R)
		ic := &Incompatibility{
			Terms: []Term{
				{Pkg: pkg, Allowed: singletonRange{V: ver}, Positive: false},
				{Pkg: d.Pkg, Allowed: d.Allow, Positive: true},
			},
			Cause: CauseDependency{Pkg: pkg, Ver: ver},
		}
		s.addIncompatibility(ic)
	}
}

func (s *Solver) isComplete(ps *partialSolution) bool {
	// Heuristic: if every package in allowed has a singleton range, we can call it complete.
	for _, r := range ps.allowed {
		switch rr := r.(type) {
		case singletonRange:
			_ = rr
		default:
			return false
		}
	}
	return len(ps.allowed) > 0
}

func (s *Solver) pickAnyUnresolved(allowed map[PackageID]Range, decided map[PackageID]bool) PackageID {
	ids := make([]string, 0, len(allowed))
	for id := range allowed {
		if !decided[id] {
			ids = append(ids, string(id))
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return PackageID(ids[0])
}

func (s *Solver) pickRootPackage(roots []Constraint) PackageID {
	if len(roots) == 0 {
		return ""
	}
	ids := make([]string, 0, len(roots))
	for _, r := range roots {
		ids = append(ids, string(r.Pkg))
	}
	sort.Strings(ids)
	return PackageID(ids[0])
}

// unitPropagate applies all known incompatibilities to narrow ranges and
// detect immediate conflicts. Returns a conflicting incompatibility if found.
func (s *Solver) unitPropagate(ps *partialSolution) *Incompatibility {
    // Helpers to evaluate literal states
    termTrue := func(t Term, have Range) bool { return have != nil && termSatisfied(t, have, s.Cmp) }
    termFalse := func(t Term, have Range) bool {
        if have == nil {
            return false
        }
        inter := have.Intersect(t.Allowed, s.Cmp)
        if t.Positive {
            return inter.IsEmpty()
        }
        return !inter.IsEmpty()
    }
    latestIdx := func(pkg PackageID) int {
        for i := len(ps.trail) - 1; i >= 0; i-- {
            if ps.trail[i].Pkg == pkg {
                return i
            }
        }
        return -1
    }

    changed := true
    for changed {
        changed = false
        for _, ic := range s.incs {
            falseCount := 0
            unknownIdx := -1
            type fals struct{ term Term; li int }
            falses := make([]fals, 0, len(ic.Terms))
            satisfiedClause := false
            for i, t := range ic.Terms {
                have, has := ps.currentAllowed(t.Pkg)
                if !has {
                    if unknownIdx == -1 {
                        unknownIdx = i
                    } else {
                        unknownIdx = -2
                        break
                    }
                    continue
                }
                if termTrue(t, have) {
                    satisfiedClause = true
                    break
                }
                if termFalse(t, have) {
                    falseCount++
                    falses = append(falses, fals{term: t, li: latestIdx(t.Pkg)})
                } else {
                    if unknownIdx == -1 {
                        unknownIdx = i
                    } else {
                        unknownIdx = -2
                        break
                    }
                }
            }
            if satisfiedClause {
                continue
            }
            if unknownIdx == -1 && falseCount == len(ic.Terms) {
                return ic
            }
            if unknownIdx >= 0 && falseCount == len(ic.Terms)-1 {
                // Unit: force unknown literal to be true
                t := ic.Terms[unknownIdx]
                for _, f := range falses {
                    if f.li >= 0 {
                        ps.recordSatisfier(f.term, f.li, ic, t.Pkg)
                    }
                }
                prev, _ := ps.currentAllowed(t.Pkg)
                newR := deriveRangeForTerm(t, prev, s.Cmp)
                if newR.IsEmpty() {
                    return ic
                }
                if rangeString(prev) != rangeString(newR) {
                    ps.derive(t.Pkg, newR, ic)
                    changed = true
                }
            }
        }
    }
    return nil
}

func termSatisfied(t Term, have Range, cmp Comparator) bool {
	if t.Positive {
		// satisfied if have ⊆ t.Allowed (i.e., we are within the allowed range)
		inter := have.Intersect(t.Allowed, cmp)
		return !inter.IsEmpty() && rangeString(have) == rangeString(inter)
	}
	// Negative term: satisfied if have ∩ Allowed == ∅ (we exclude that range)
	inter := have.Intersect(t.Allowed, cmp)
	return inter.IsEmpty()
}

func deriveRangeForTerm(t Term, prev Range, cmp Comparator) Range {
    // Make the literal t true given previous range prev
    if t.Positive {
        if prev == nil {
            return t.Allowed
        }
        return prev.Intersect(t.Allowed, cmp)
    }
    // Negative literal: ensure exclusion of t.Allowed from prev
    if prev == nil {
        return negatedRange{Disallow: t.Allowed}
    }
    switch p := prev.(type) {
    case singletonRange:
        if t.Allowed.Contains(p.V, cmp) {
            return emptyRange{}
        }
        return prev
    case anyRange:
        return negatedRange{Disallow: t.Allowed}
    case negatedRange:
        if rangeContainsRange(p.Disallow, t.Allowed) {
            return prev
        }
        return negatedRange{Disallow: unionRange{A: p.Disallow, B: t.Allowed}}
    case intervalRange:
        if sv, ok := t.Allowed.(singletonRange); ok {
            if p.Lower != nil && cmp.Compare(sv.V, p.Lower) == 0 && p.IncLower {
                return intervalRange{Lower: p.Lower, Upper: p.Upper, IncLower: false, IncUpper: p.IncUpper}
            }
            if p.Upper != nil && cmp.Compare(sv.V, p.Upper) == 0 && p.IncUpper {
                return intervalRange{Lower: p.Lower, Upper: p.Upper, IncLower: p.IncLower, IncUpper: false}
            }
        }
        return prev
    default:
        return prev
    }
}

// rangeContainsRange reports whether container already includes target when the
// container may be a nested unionRange. This uses structural/string equality of
// leaf ranges to avoid infinite growth during repeated propagation.
func rangeContainsRange(container Range, target Range) bool {
	if rangeString(container) == rangeString(target) {
		return true
	}
	if u, ok := container.(unionRange); ok {
		return rangeContainsRange(u.A, target) || rangeContainsRange(u.B, target)
	}
	return false
}

// resolveConflict performs conflict-driven learning and backjump level selection.
// This is a compact single-file rendition capturing the spirit of PubGrub:
// we synthesize a learned clause from the conflict and the current trail.
func (s *Solver) resolveConflict(ps *partialSolution, conflict *Incompatibility) (int, *Incompatibility) {
    learned := conflict

    latestIdx := func(pkg PackageID) int {
        for i := len(ps.trail) - 1; i >= 0; i-- {
            if ps.trail[i].Pkg == pkg {
                return i
            }
        }
        return -1
    }

    // Heuristic: prefer to block the most recent depender version involved in derivations.
    for i := len(ps.trail) - 1; i >= 0; i-- {
        as := ps.trail[i]
        if as.Reason != nil {
            if cd, ok := as.Reason.Cause.(CauseDependency); ok {
                learned = &Incompatibility{Terms: []Term{{Pkg: cd.Pkg, Allowed: singletonRange{V: cd.Ver}, Positive: false}}, Cause: CauseConflict{Conflict: conflict, Other: as.Reason}}
                // backjump just before that depender decision
                li := latestIdx(cd.Pkg)
                bj := 0
                if li >= 0 { bj = ps.trail[li].DecisionN - 1 }
                return bj, learned
            }
        }
    }

    // Fast-path removed: rely on UIP resolution to ensure good backjumps.

    // (UIP) Conflict analysis proceeds via resolution; no eager depender blocking here.

	for {
		// Count current-level literals and select most recent as pivot
		countCurr := 0
		var pivotPkg PackageID
		pivotPos := -1
		pivotIdx := -1
		for i, t := range learned.Terms {
			li := latestIdx(t.Pkg)
			if li < 0 {
				continue
			}
			if ps.trail[li].DecisionN == ps.level {
				countCurr++
				if li > pivotIdx {
					pivotIdx = li
					pivotPkg = t.Pkg
					pivotPos = i
				}
			}
		}
        if countCurr <= 1 {
            // Prefer blocking an earlier depender that forced a non-current term.
            for _, t := range learned.Terms {
                // skip pivot package if identified
                if pivotPos >= 0 && t.Pkg == learned.Terms[pivotPos].Pkg {
                    continue
                }
                li := latestIdx(t.Pkg)
                if li < 0 {
                    continue
                }
                // walk back to find an assignment with a reason
                var withReason *assignment
                for j := li; j >= 0; j-- {
                    if ps.trail[j].Pkg != t.Pkg {
                        continue
                    }
                    if ps.trail[j].Reason != nil {
                        withReason = &ps.trail[j]
                        break
                    }
                }
                if withReason != nil {
                    if cd, ok := withReason.Reason.Cause.(CauseDependency); ok {
                        // Learn unit forbidding that depender version
                        learned = &Incompatibility{Terms: []Term{{Pkg: cd.Pkg, Allowed: singletonRange{V: cd.Ver}, Positive: false}}, Cause: CauseConflict{Conflict: learned, Other: withReason.Reason}}
                        // backjump to just before depender's decision
                        lli := latestIdx(cd.Pkg)
                        bj := 0
                        if lli >= 0 {
                            bj = ps.trail[lli].DecisionN - 1
                        }
                        return bj, learned
                    }
                }
            }
            // Compute backjump level as the highest level among non-current literals
            backLevel := 0
			for j, t := range learned.Terms {
				if pivotPos >= 0 && j == pivotPos {
					continue
				}
				li := latestIdx(t.Pkg)
				if li < 0 {
					continue
				}
				lvl := ps.trail[li].DecisionN
				if lvl < ps.level && lvl > backLevel {
					backLevel = lvl
				}
			}
			// If pivot is a decision on a concrete version, forbid it
			if pivotPos >= 0 && pivotIdx >= 0 {
				as := ps.trail[pivotIdx]
				if as.Kind == assignDecision {
					if sn, ok := as.Allowed.(singletonRange); ok {
						learned = &Incompatibility{Terms: []Term{{Pkg: pivotPkg, Allowed: sn, Positive: false}}, Cause: CauseConflict{Conflict: learned, Other: as.Reason}}
						return as.DecisionN - 1, learned
					}
				}
			}
			return backLevel, learned
		}
		if pivotPos == -1 {
			break
		}
		// Fetch reason for pivot via satisfier if possible
		pivotTerm := learned.Terms[pivotPos]
		var reason *Incompatibility
		if sat, ok := ps.satisfierOf(pivotTerm); ok && sat.By != nil {
			reason = sat.By
		}
		if reason == nil {
			if pivotIdx >= 0 {
				as := ps.trail[pivotIdx]
				reason = as.Reason
			}
		}
		if reason == nil {
			break
		}
		learned = resolveOn(learned, reason, pivotPkg)
	}

	// Fallback: block latest decision at current level
	for i := len(ps.trail) - 1; i >= 0; i-- {
		as := ps.trail[i]
		if as.Kind == assignDecision && as.DecisionN == ps.level {
			if sn, ok := as.Allowed.(singletonRange); ok {
				learned = &Incompatibility{Terms: []Term{{Pkg: as.Pkg, Allowed: sn, Positive: false}}, Cause: CauseConflict{Conflict: conflict, Other: learned}}
				return as.DecisionN - 1, learned
			}
		}
	}
	return 0, learned
}

func termHasPkg(ic *Incompatibility, pkg PackageID) bool {
	for _, t := range ic.Terms {
		if t.Pkg == pkg {
			return true
		}
	}
	return false
}

// resolveOn merges two incompatibilities eliminating references to pivotPkg.
// This is a simplified resolution: union of terms excluding pivot, with de-dupe.
func resolveOn(a, b *Incompatibility, pivotPkg PackageID) *Incompatibility {
	out := &Incompatibility{Terms: []Term{}, Cause: CauseConflict{Conflict: a, Other: b}}
	add := func(t Term) {
		for _, et := range out.Terms {
			if et.Pkg == t.Pkg && rangeString(et.Allowed) == rangeString(t.Allowed) && et.Positive == t.Positive {
				return
			}
		}
		out.Terms = append(out.Terms, t)
	}
	for _, t := range a.Terms {
		if t.Pkg == pivotPkg {
			continue
		}
		add(t)
	}
	for _, t := range b.Terms {
		if t.Pkg == pivotPkg {
			continue
		}
		add(t)
	}
	return out
}

/*
////////////////////////////////////////////////////////////////////////////////
// Minimal Range algebra (SemVer-friendly, but pluggable)
//
// To keep this single-file, we ship a tiny range system:
//
//   - anyRange:     all versions allowed
//   - emptyRange:   no versions allowed
//   - singletonRange{v}
//   - intervalRange{lower/upper inclusive flags}
//
// This is sufficient for the demo and for wiring your own Conda-style Range later.
////////////////////////////////////////////////////////////////////////////////

type anyRange struct{}

func (a anyRange) Contains(v Version, _ Comparator) bool     { return true }
func (a anyRange) Intersect(other Range, _ Comparator) Range { return other }
func (a anyRange) IsEmpty() bool                             { return false }
func (a anyRange) String() string                            { return "*" }

type emptyRange struct{}

func (e emptyRange) Contains(v Version, _ Comparator) bool     { return false }
func (e emptyRange) Intersect(other Range, _ Comparator) Range { return e }
func (e emptyRange) IsEmpty() bool                             { return true }
func (e emptyRange) String() string                            { return "∅" }

type singletonRange struct{ V Version }

func (s singletonRange) Contains(v Version, cmp Comparator) bool { return cmp.Compare(v, s.V) == 0 }
func (s singletonRange) Intersect(other Range, cmp Comparator) Range {
	if other.Contains(s.V, cmp) {
		return s
	}
	return emptyRange{}
}
func (s singletonRange) IsEmpty() bool  { return false }
func (s singletonRange) String() string { return fmt.Sprintf("[%s]", s.V.String()) }

type intervalRange struct {
	Lower    Version // may be nil meaning unbounded
	Upper    Version // may be nil meaning unbounded
	IncLower bool
	IncUpper bool
}

// Exported helpers for constructing ranges from other packages (and tests).
func Any() Range                                         { return anyRange{} }
func Empty() Range                                       { return emptyRange{} }
func Singleton(v Version) Range                          { return singletonRange{V: v} }
func Interval(lower Version, incLower bool, upper Version, incUpper bool) Range {
    return intervalRange{Lower: lower, Upper: upper, IncLower: incLower, IncUpper: incUpper}
}

func (r intervalRange) Contains(v Version, cmp Comparator) bool {
	if r.Lower != nil {
		c := cmp.Compare(v, r.Lower)
		if c < 0 || (c == 0 && !r.IncLower) {
			return false
		}
	}
	if r.Upper != nil {
		c := cmp.Compare(v, r.Upper)
		if c > 0 || (c == 0 && !r.IncUpper) {
			return false
		}
	}
	return true
}
func (r intervalRange) Intersect(o Range, cmp Comparator) Range {
	switch oo := o.(type) {
	case emptyRange:
		return emptyRange{}
	case anyRange:
		return r
	case singletonRange:
		if r.Contains(oo.V, cmp) {
			return oo
		}
		return emptyRange{}
	case intervalRange:
		var lo Version = r.Lower
		var hi Version = r.Upper
		incLo := r.IncLower
		incHi := r.IncUpper
    // max lower
    if oo.Lower != nil {
        if lo == nil || cmp.Compare(oo.Lower, lo) > 0 {
            lo = oo.Lower
            incLo = oo.IncLower
        } else if cmp.Compare(oo.Lower, lo) == 0 {
            incLo = incLo && oo.IncLower
        }
    }
    // min upper
    if oo.Upper != nil {
        if hi == nil || cmp.Compare(oo.Upper, hi) < 0 {
            hi = oo.Upper
            incHi = oo.IncUpper
        } else if cmp.Compare(oo.Upper, hi) == 0 {
            incHi = incHi && oo.IncUpper
        }
    }
		if lo != nil && hi != nil {
			c := cmp.Compare(lo, hi)
			if c > 0 || (c == 0 && (!incLo || !incHi)) {
				return emptyRange{}
			}
		}
		return intervalRange{Lower: lo, Upper: hi, IncLower: incLo, IncUpper: incHi}
	default:
		// unknown Range type: best-effort check by sampling is not allowed; be conservative.
		return o.Intersect(r, cmp)
	}
}
func (r intervalRange) IsEmpty() bool { return false }
func (r intervalRange) String() string {
	var lo, hi string
	if r.Lower != nil {
		if r.IncLower {
			lo = "[" + r.Lower.String()
		} else {
			lo = "(" + r.Lower.String()
		}
	} else {
		lo = "(-∞"
	}
	if r.Upper != nil {
		if r.IncUpper {
			hi = r.Upper.String() + "]"
		} else {
			hi = r.Upper.String() + ")"
		}
	} else {
		hi = "+∞)"
	}
	return lo + "," + hi
}

// negatedRange disallows a subrange from "any"; compact helper for derived negatives.
type negatedRange struct {
	Disallow Range
}

func (n negatedRange) Contains(v Version, cmp Comparator) bool { return !n.Disallow.Contains(v, cmp) }
func (n negatedRange) Intersect(o Range, cmp Comparator) Range {
	// Intersect by filtering o to exclude Disallow if o is finite/simple.
	switch oo := o.(type) {
	case emptyRange:
		return emptyRange{}
	case singletonRange:
		if n.Disallow.Contains(oo.V, cmp) {
			return emptyRange{}
		}
		return oo
	case anyRange:
		return n
	default:
		// For simplicity: if intersection with disallow is total, become empty; else return o.
		inter := oo.Intersect(n.Disallow, cmp)
		if !inter.IsEmpty() && rangeString(inter) == rangeString(oo) {
			return emptyRange{}
		}
		return o
	}
}
func (n negatedRange) IsEmpty() bool  { return false }
func (n negatedRange) String() string { return fmt.Sprintf("!*%s", n.Disallow.String()) }

// unionRange models a union of two ranges. It is only used as a helper
// inside negatedRange to accumulate disallowed subsets.
type unionRange struct{ A, B Range }

func (u unionRange) Contains(v Version, cmp Comparator) bool {
    return u.A.Contains(v, cmp) || u.B.Contains(v, cmp)
}
func (u unionRange) Intersect(o Range, cmp Comparator) Range {
    switch oo := o.(type) {
    case emptyRange:
        return emptyRange{}
    case singletonRange:
        if u.Contains(oo.V, cmp) {
            return emptyRange{}
        }
        return oo
    case anyRange:
        return u
    default:
        ia := u.A.Intersect(oo, cmp)
        ib := u.B.Intersect(oo, cmp)
        if (!ia.IsEmpty() && rangeString(ia) == rangeString(oo)) || (!ib.IsEmpty() && rangeString(ib) == rangeString(oo)) {
            return emptyRange{}
        }
        return o
    }
}
func (u unionRange) IsEmpty() bool  { return false }
func (u unionRange) String() string { return fmt.Sprintf("(%s ∪ %s)", u.A.String(), u.B.String()) }

func rangeString(r Range) string {
	if r == nil {
		return "<nil>"
	}
	return r.String()
}

func filterByRange(vers []Version, r Range, cmp Comparator) []Version {
    out := make([]Version, 0, len(vers))
    for _, v := range vers {
        if r.Contains(v, cmp) {
            out = append(out, v)
        }
    }
    return out
}
*/
// Explainer moved to explainer.go

////////////////////////////////////////////////////////////////////////////////
// Deterministic default policies
////////////////////////////////////////////////////////////////////////////////

// Prioritizer and chooser moved to policy.go

////////////////////////////////////////////////////////////////////////////////
// Minimal SemVer for the demo (opaque Version + Comparator)
//
// This is intentionally tiny; swap with a Conda comparator/range later.
////////////////////////////////////////////////////////////////////////////////

type semver struct {
	Mj, Mi, P int
	Pre       string
}

func (s semver) String() string {
	if s.Pre != "" {
		return fmt.Sprintf("%d.%d.%d-%s", s.Mj, s.Mi, s.P, s.Pre)
	}
	return fmt.Sprintf("%d.%d.%d", s.Mj, s.Mi, s.P)
}

type semverCmp struct{}

func (semverCmp) Compare(a, b Version) int {
	aa := a.(semver)
	bb := b.(semver)
	if aa.Mj != bb.Mj {
		if aa.Mj < bb.Mj {
			return -1
		}
		return 1
	}
	if aa.Mi != bb.Mi {
		if aa.Mi < bb.Mi {
			return -1
		}
		return 1
	}
	if aa.P != bb.P {
		if aa.P < bb.P {
			return -1
		}
		return 1
	}
	// pre-release lower than release
	if aa.Pre == "" && bb.Pre != "" {
		return 1
	}
	if aa.Pre != "" && bb.Pre == "" {
		return -1
	}
	return strings.Compare(aa.Pre, bb.Pre)
}

func semverInterval(lo *semver, loInc bool, hi *semver, hiInc bool) intervalRange {
	var l Version
	var u Version
	if lo != nil {
		l = *lo
	}
	if hi != nil {
		u = *hi
	}
	return intervalRange{Lower: l, Upper: u, IncLower: loInc, IncUpper: hiInc}
}

////////////////////////////////////////////////////////////////////////////////
// Demo universe (in-memory Provider)
//
// This mirrors the classic PubGrub “dropdown/icons” example to exercise
// conflicts and explanations. You can replace this with a Conda provider.
////////////////////////////////////////////////////////////////////////////////

type memProvider struct {
	versions map[PackageID][]Version               // pkg -> all versions (unsorted ok)
	deps     map[PackageID]map[string][]Constraint // pkg -> ver.String() -> constraints
	yanked   map[PackageID]map[string]bool
	cmp      Comparator
}

func (m *memProvider) ListVersions(pkg PackageID) ([]Version, error) {
	vs := append([]Version(nil), m.versions[pkg]...)
	// deterministic sorting (newest first to align with chooser expectation)
	sort.Slice(vs, func(i, j int) bool {
		c := m.cmp.Compare(vs[i], vs[j])
		if c != 0 {
			return c > 0
		}
		return vs[i].String() > vs[j].String()
	})
	return vs, nil
}
func (m *memProvider) Dependencies(pkg PackageID, ver Version) ([]Constraint, error) {
	if m.deps[pkg] == nil {
		return nil, nil
	}
	cs := m.deps[pkg][ver.String()]
	out := append([]Constraint(nil), cs...)
	return out, nil
}
func (m *memProvider) IsYanked(pkg PackageID, ver Version) bool {
	return m.yanked[pkg] != nil && m.yanked[pkg][ver.String()]
}

func demoUniverse() *memProvider {
	cmp := semverCmp{}
	m := &memProvider{
		versions: map[PackageID][]Version{},
		deps:     map[PackageID]map[string][]Constraint{},
		yanked:   map[PackageID]map[string]bool{},
		cmp:      cmp,
	}
	// icons: 1.0.0, 2.0.0
	m.versions["icons"] = []Version{semver{1, 0, 0, ""}, semver{2, 0, 0, ""}}
	// dropdown: 1.0.0 depends icons <2.0.0 ; 2.0.0 depends icons >=2.0.0
	m.versions["dropdown"] = []Version{semver{1, 0, 0, ""}, semver{2, 0, 0, ""}}
	m.deps["dropdown"] = map[string][]Constraint{
		"1.0.0": {
			{Pkg: "icons", Allow: semverInterval(nil, false, &semver{2, 0, 0, ""}, false), Note: "dropdown 1.x needs icons <2.0.0"},
		},
		"2.0.0": {
			{Pkg: "icons", Allow: semverInterval(&semver{2, 0, 0, ""}, true, nil, false), Note: "dropdown 2.x needs icons >=2.0.0"},
		},
	}
	// root: depends dropdown >=2.0.0 and icons <2.0.0 to create a conflict
	m.versions["root"] = []Version{semver{1, 0, 0, ""}}
	return m
}

////////////////////////////////////////////////////////////////////////////////
// CLI (embedded demo)
////////////////////////////////////////////////////////////////////////////////

func parseRootConstraint(spec string) (PackageID, Range, error) {
	// Format: name@"ranges", where ranges like ">=1.0 <3.0" or "*" or "=1.2.3"
	parts := strings.SplitN(spec, "@", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("root must look like name@\"range\" (quotes optional)")
	}
	pkg := PackageID(strings.TrimSpace(parts[0]))
	rs := strings.Trim(parts[1], "\"")
	if rs == "*" || rs == "" {
		return pkg, anyRange{}, nil
	}
	// super small parser supporting "=X.Y.Z", ">=A <=B" etc., space-separated
	toks := strings.Fields(rs)
	if len(toks) == 1 && strings.HasPrefix(toks[0], "=") {
		v := parseSemver(strings.TrimPrefix(toks[0], "="))
		return pkg, singletonRange{V: v}, nil
	}
	var lo *semver
	var hi *semver
	loInc := false
	hiInc := false
	for _, t := range toks {
		switch {
		case strings.HasPrefix(t, ">="):
			v := parseSemver(strings.TrimPrefix(t, ">="))
			lo = &v
			loInc = true
		case strings.HasPrefix(t, ">"):
			v := parseSemver(strings.TrimPrefix(t, ">"))
			lo = &v
			loInc = false
		case strings.HasPrefix(t, "<="):
			v := parseSemver(strings.TrimPrefix(t, "<="))
			hi = &v
			hiInc = true
		case strings.HasPrefix(t, "<"):
			v := parseSemver(strings.TrimPrefix(t, "<"))
			hi = &v
			hiInc = false
		default:
			return "", nil, fmt.Errorf("unsupported token in range: %q", t)
		}
	}
	return pkg, semverInterval(lo, loInc, hi, hiInc), nil
}

func parseSemver(s string) semver {
	// VERY tiny parser: major.minor.patch[-pre]
	core, pre, _ := strings.Cut(s, "-")
	ps := strings.Split(core, ".")
	asInt := func(x string) int {
		n := 0
		for i := 0; i < len(x); i++ {
			if x[i] < '0' || x[i] > '9' {
				break
			}
			n = n*10 + int(x[i]-'0')
		}
		return n
	}
	var mj, mi, p int
	if len(ps) > 0 {
		mj = asInt(ps[0])
	}
	if len(ps) > 1 {
		mi = asInt(ps[1])
	}
	if len(ps) > 2 {
		p = asInt(ps[2])
	}
	return semver{mj, mi, p, pre}
}

// Note: CLI demo moved to cmd/pubgrub for cleanliness.
