package pubgrub

import "testing"

func TestDeriveRangeForTerm_NegativeUnion(t *testing.T) {
    cmp := semverCmp{}
    v1 := semver{1, 0, 0, ""}
    v2 := semver{2, 0, 0, ""}

    prev := negatedRange{Disallow: singletonRange{V: v1}}
    term := Term{Pkg: "a", Allowed: singletonRange{V: v2}, Positive: false}

    got := deriveRangeForTerm(term, prev, cmp)

    if got.Contains(v1, cmp) {
        t.Fatalf("expected to continue disallowing %s, but got range allowing it: %s", v1.String(), got.String())
    }
    if got.Contains(v2, cmp) {
        t.Fatalf("expected to now also disallow %s, but got range allowing it: %s", v2.String(), got.String())
    }
}

