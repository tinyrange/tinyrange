package pubgrub

import "testing"

func TestIntervalIntersectBoundaryInclusivity_LowerExclusiveKillsSingleton(t *testing.T) {
	cmp := semverCmp{}
	v := semver{1, 0, 0, ""}
	// [1.0.0, 1.0.0]
	r1 := intervalRange{Lower: v, IncLower: true, Upper: v, IncUpper: true}
	// (1.0.0, +inf)
	r2 := intervalRange{Lower: v, IncLower: false, Upper: nil, IncUpper: false}

	inter := r1.Intersect(r2, cmp)
	if !inter.IsEmpty() {
		t.Fatalf("expected empty intersection at boundary, got %s", inter.String())
	}
}

func TestIntervalIntersectBoundaryInclusivity_UpperExclusiveKillsSingleton(t *testing.T) {
	cmp := semverCmp{}
	v := semver{1, 0, 0, ""}
	// [1.0.0, 1.0.0]
	r1 := intervalRange{Lower: v, IncLower: true, Upper: v, IncUpper: true}
	// (-inf, 1.0.0)
	r2 := intervalRange{Lower: nil, IncLower: false, Upper: v, IncUpper: false}

	inter := r1.Intersect(r2, cmp)
	if !inter.IsEmpty() {
		t.Fatalf("expected empty intersection at boundary, got %s", inter.String())
	}
}

func TestIntervalIntersectBoundaryInclusivity_TouchingInclusiveYieldsSingleton(t *testing.T) {
	cmp := semverCmp{}
	v1 := semver{1, 0, 0, ""}
	v2 := semver{2, 0, 0, ""}
	// [1.0.0, 2.0.0]
	r1 := intervalRange{Lower: v1, IncLower: true, Upper: v2, IncUpper: true}
	// [2.0.0, 3.0.0]
	r2 := intervalRange{Lower: v2, IncLower: true, Upper: semver{3, 0, 0, ""}, IncUpper: true}
	inter := r1.Intersect(r2, cmp)
	// Accept either explicit singletonRange or a degenerate interval [v,v]
	if _, ok := inter.(singletonRange); ok {
		return
	}
	if ir, ok := inter.(intervalRange); ok {
		if ir.Lower == nil || ir.Upper == nil || !ir.IncLower || !ir.IncUpper || cmp.Compare(ir.Lower, ir.Upper) != 0 {
			t.Fatalf("expected degenerate closed interval [v,v], got %s", inter.String())
		}
		return
	}
	t.Fatalf("expected singleton-like result, got %T: %s", inter, inter.String())
}
