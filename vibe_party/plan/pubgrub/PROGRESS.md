# PubGrub Solver – Progress and Plan

This document tracks current status and the plan to complete a proper PubGrub-style solver with full UIP clause learning, better backtracking behavior, and measurable performance.

## Current Status

Implemented:
- Core solver with decisions, partial solution trail, and incompatibility store.
- Dependency propagation: after deciding `pkg@ver`, learn `(pkg in [ver]) ∧ (dep in R)` for every dependency.
- Range algebra fixes (interval boundary inclusivity, negated union).
- Clause learning improvements and a basic resolution loop.
- E2E tests for conflict, success, and dependency inclusion (passing).
- Benchmarks: linear chain (100) and branching (depth 7).

Outstanding:
- Backtracking e2e case still loops in a crafted scenario. Needs true UIP learning with satisfier tracking and polarity-aware resolution.

## Goals

1) Implement full UIP conflict learning (as in PubGrub):
   - Track literal satisfiers precisely.
   - Resolve on the most recent current-level literal using its satisfier until one current-level literal remains.
   - Produce an asserting clause that is unit at the backjump level (guarantees progress).

2) Make TestBacktrackToAlternateVersion_Succeeds pass deterministically.

3) Maintain/measure performance with existing benchmarks; add a conflict-heavy benchmark.

## Plan of Work

1) Data model changes (satisfier tracking)
- Extend the assignment trail to record satisfier information:
  - For each package assignment, keep a pointer to the incompatibility that justified it (already present as `Reason`) and the decision level.
  - Add a lightweight “satisfier map” per package that records, for a term that becomes satisfied, which assignment (decision or derived) satisfied it at the time of propagation.
    - Representation: `map[termKey]satisfier`, where `termKey = pkg + ":" + rangeString(range) + ":" + polarity`.
    - `satisfier` holds: pointer to incompatibility (the one applied), the pivot package used, and the trail index.
- Introduce helpers:
  - `recordSatisfier(term, asgn)` – called when a term becomes satisfied during propagation.
  - `satisfierOf(term)` – returns the assignment/incompatibility that satisfied the term.

2) Unit propagation changes
- When evaluating an incompatibility, if all but one term are satisfied (under current ranges), the last term becomes a unit requirement. Before deriving/tightening the last term:
  - Record satisfiers for the satisfied terms (if not already recorded) using the current trail assignment that made them true.
  - When an empty intersection is detected, construct the initial learned clause from the conflicting incompatibility and use satisfiers to start resolution.

3) Polarity and resolution mechanics
- Normalize term polarity for resolution:
  - Maintain terms as “pkg in R” (positive) and “pkg not in R” (negative). Resolution on pivot removes complementary occurrences (positive vs negative) of the same package/range.
  - Add helper: `resolve(icA, icB, pivotPkg)` that merges clauses and removes opposite pivot literals, deduplicating remaining terms.
- UIP resolution loop:
  - While there are more than one current-level literals in the learned clause:
    - Pick the most recent current-level literal (by trail index) as pivot.
    - Fetch its satisfier incompatibility (the reason that introduced/justified that literal at the time it became true).
    - Resolve the learned clause with the satisfier over the pivot.
  - After the loop, compute backjump level as the highest decision level among remaining literals except the current level; the clause must be unit at that level (asserting).
  - If the learned clause reduces to a single literal, flip to a negative unit that forbids the exact decision.

4) Decision order (no-op if UIP is correct)
- Keep current “roots-first, then prioritizer, then any unresolved” policy. If UIP guarantees progress, extra heuristics shouldn’t be necessary.

5) Tests
- Enable and keep passing existing tests:
  - Range boundary and negation tests.
  - E2E: conflict/success/dependencies.
  - E2E: backtracking to alternate version (must pass).
- Add two more E2E tests:
  - Multi-level backjump: conflict requires jumping back >1 decision.
  - Conflicting siblings: two dependers require disjoint ranges of a shared dep; learned clause should block one earlier decision reliably.

6) Benchmarks
- Keep current benchmarks.
- Add a conflict-heavy benchmark that triggers backjumping repeatedly on a chain (to measure UIP overhead):
  - e.g., alternating constraints that force the solver to switch earlier decisions several times across N nodes.

7) Cleanup
- Remove the step-cap safety once UIP is implemented and verified to converge.
- Keep resolution helpers small and local; avoid overfitting heuristics (UIP should suffice).

## Acceptance Criteria
- All unit and E2E tests pass, including TestBacktrackToAlternateVersion_Succeeds and added multi-level backjump case.
- No infinite loops without the step cap; convergence is guaranteed by asserting clauses.
- Benchmarks remain within reasonable bounds:
  - Linear chain (100): same order-of-magnitude runtime.
  - Branching depth 7: same order-of-magnitude runtime.
  - Conflict-heavy benchmark: completes without pathological explosion.

## Risk/Notes
- Satisfier tracking must be carefully wired during propagation to reflect the assignment that truly satisfied a term (not just the latest assignment for that package).
- Resolution must remove complementary pivot literals only; incorrect polarity handling will produce non-asserting clauses.
- Keep incompatibility/term structures minimal to avoid heavy allocations in hot paths.

## Implementation Steps (incremental commits)
1) Add satisfier scaffolding (types, helpers), compile-only.
2) Wire satisfier recording in unit propagation; retain current resolution path.
3) Replace resolution with UIP loop using satisfiers; remove ad-hoc heuristics.
4) Re-enable backtracking test; iterate until green.
5) Add multi-level backjump test; iterate until green.
6) Add conflict-heavy benchmark; re-run all benchmarks.
7) Remove step cap and final tidy-up.

