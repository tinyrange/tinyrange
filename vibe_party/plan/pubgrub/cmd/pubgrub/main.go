package main

import (
    "fmt"
    "os"
    "sort"
    "strings"

    pg "github.com/tinyrange/tinyrange/vibe_party/plan/pubgrub"
)

func main() {
    if len(os.Args) >= 2 && os.Args[1] == "resolve" {
        runResolveCLI(os.Args[2:])
        return
    }
    fmt.Println("Usage:")
    fmt.Println("  go run ./vibe_party/plan/pubgrub/cmd/pubgrub -- resolve root@\">=1.0 <3.0\"")
}

func runResolveCLI(args []string) {
    if len(args) < 1 {
        fmt.Println("resolve requires at least one root like root@\">=1.0 <3.0\"")
        os.Exit(2)
    }
    roots := []pg.Constraint{}
    for _, a := range args {
        p, r, err := parseRootConstraint(a)
        if err != nil {
            fmt.Println("bad root:", err)
            os.Exit(2)
        }
        roots = append(roots, pg.Constraint{Pkg: p, Allow: r, Note: "root"})
    }

    // Build demo provider & add root deps so we can create the classic conflict.
    prov := demoUniverse()
    // Add root dependencies tailored to the demo: root -> dropdown >=2.0.0, icons <2.0.0
    if prov.deps["root"] == nil {
        prov.deps["root"] = map[string][]pg.Constraint{}
    }
    prov.deps["root"]["1.0.0"] = []pg.Constraint{
        {Pkg: "dropdown", Allow: semverInterval(&semver{2, 0, 0, ""}, true, nil, false), Note: "root needs dropdown >=2.0.0"},
        {Pkg: "icons", Allow: semverInterval(nil, false, &semver{2, 0, 0, ""}, false), Note: "root needs icons <2.0.0"},
    }

    cmp := semverCmp{}
    prior := pg.FewestVersionsPrioritizer{Prov: prov, Cmp: cmp}
    chooser := pg.NewestFirstChooser{Prov: prov, Cmp: cmp}
    solver := pg.NewSolver(prov, cmp, prior, chooser)

    // Add the root package itself as a decision subject (pin to its sole version).
    roots = append(roots, pg.Constraint{Pkg: "root", Allow: pg.Singleton(semver{1, 0, 0, ""}), Note: "pin root"})

    solution, err := solver.Solve(roots)
    if err != nil {
        fmt.Println("Resolution failed:")
        fmt.Println("  " + err.Error())
        os.Exit(1)
    }
    fmt.Println("Resolution succeeded:")
    // Print in stable order
    keys := make([]string, 0, len(solution))
    for k := range solution {
        keys = append(keys, string(k))
    }
    sort.Strings(keys)
    for _, k := range keys {
        fmt.Printf("  %s  %s\n", k, solution[pg.PackageID(k)].String())
    }
}

// Lightweight helpers that mirror package internals for the demo.

// Note: These are copied from the pubgrub package with minimal exposure.

type semver struct { Mj, Mi, P int; Pre string }

func (s semver) String() string {
    if s.Pre != "" { return fmt.Sprintf("%d.%d.%d-%s", s.Mj, s.Mi, s.P, s.Pre) }
    return fmt.Sprintf("%d.%d.%d", s.Mj, s.Mi, s.P)
}

type semverCmp struct{}

func (semverCmp) Compare(a, b pg.Version) int {
    aa := a.(semver)
    bb := b.(semver)
    if aa.Mj != bb.Mj { if aa.Mj < bb.Mj { return -1 } ; return 1 }
    if aa.Mi != bb.Mi { if aa.Mi < bb.Mi { return -1 } ; return 1 }
    if aa.P != bb.P { if aa.P < bb.P { return -1 } ; return 1 }
    if aa.Pre == "" && bb.Pre != "" { return 1 }
    if aa.Pre != "" && bb.Pre == "" { return -1 }
    return strings.Compare(aa.Pre, bb.Pre)
}

func semverInterval(lo *semver, loInc bool, hi *semver, hiInc bool) pg.Range {
    var l pg.Version
    var u pg.Version
    if lo != nil { l = *lo }
    if hi != nil { u = *hi }
    return pg.Interval(l, loInc, u, hiInc)
}

func parseRootConstraint(s string) (pg.PackageID, pg.Range, error) {
    // Parse something like name@"RANGE"
    name, rs, ok := strings.Cut(s, "@")
    if !ok { return "", nil, fmt.Errorf("missing '@' in %q", s) }
    rs = strings.Trim(rs, "\"'")
    pkg := pg.PackageID(name)
    if rs == "*" || rs == "" { return pkg, pg.Any(), nil }
    toks := strings.Fields(rs)
    if len(toks) == 1 && strings.HasPrefix(toks[0], "=") {
        v := parseSemver(strings.TrimPrefix(toks[0], "="))
        return pkg, pg.Singleton(v), nil
    }
    var lo *semver
    var hi *semver
    loInc := false
    hiInc := false
    for _, t := range toks {
        switch {
        case strings.HasPrefix(t, ">="):
            v := parseSemver(strings.TrimPrefix(t, ">="))
            lo = &v; loInc = true
        case strings.HasPrefix(t, ">"):
            v := parseSemver(strings.TrimPrefix(t, ">"))
            lo = &v; loInc = false
        case strings.HasPrefix(t, "<="):
            v := parseSemver(strings.TrimPrefix(t, "<="))
            hi = &v; hiInc = true
        case strings.HasPrefix(t, "<"):
            v := parseSemver(strings.TrimPrefix(t, "<"))
            hi = &v; hiInc = false
        default:
            return "", nil, fmt.Errorf("unsupported token in range: %q", t)
        }
    }
    return pkg, semverInterval(lo, loInc, hi, hiInc), nil
}

func parseSemver(s string) semver {
    core, pre, _ := strings.Cut(s, "-")
    ps := strings.Split(core, ".")
    asInt := func(x string) int { n := 0; for i := 0; i < len(x); i++ { if x[i] < '0' || x[i] > '9' { break }; n = n*10 + int(x[i]-'0') }; return n }
    var mj, mi, p int
    if len(ps) > 0 { mj = asInt(ps[0]) }
    if len(ps) > 1 { mi = asInt(ps[1]) }
    if len(ps) > 2 { p = asInt(ps[2]) }
    return semver{mj, mi, p, pre}
}

// demoUniverse mirrors the in-package demo provider, redefined locally for CLI.
type memProvider struct {
    versions map[pg.PackageID][]pg.Version
    deps     map[pg.PackageID]map[string][]pg.Constraint
    yanked   map[pg.PackageID]map[string]bool
    cmp      pg.Comparator
}

func (m *memProvider) ListVersions(pkg pg.PackageID) ([]pg.Version, error) {
    vs := append([]pg.Version(nil), m.versions[pkg]...)
    sort.Slice(vs, func(i, j int) bool { c := m.cmp.Compare(vs[i], vs[j]); if c != 0 { return c > 0 }; return vs[i].String() > vs[j].String() })
    return vs, nil
}
func (m *memProvider) Dependencies(pkg pg.PackageID, ver pg.Version) ([]pg.Constraint, error) {
    if m.deps[pkg] == nil { return nil, nil }
    cs := m.deps[pkg][ver.String()]
    out := append([]pg.Constraint(nil), cs...)
    return out, nil
}
func (m *memProvider) IsYanked(pkg pg.PackageID, ver pg.Version) bool { return m.yanked[pkg] != nil && m.yanked[pkg][ver.String()] }

func demoUniverse() *memProvider {
    cmp := semverCmp{}
    m := &memProvider{versions: map[pg.PackageID][]pg.Version{}, deps: map[pg.PackageID]map[string][]pg.Constraint{}, yanked: map[pg.PackageID]map[string]bool{}, cmp: cmp}
    add := func(p string, versions ...string) {
        for _, v := range versions { m.versions[pg.PackageID(p)] = append(m.versions[pg.PackageID(p)], parseSemver(v)) }
    }
    dep := func(p, v string, cons ...pg.Constraint) {
        if m.deps[pg.PackageID(p)] == nil { m.deps[pg.PackageID(p)] = map[string][]pg.Constraint{} }
        m.deps[pg.PackageID(p)][v] = append([]pg.Constraint(nil), cons...)
    }
    // Versions
    add("dropdown", "2.0.0", "2.1.0", "2.1.1", "2.2.0")
    add("icons", "1.0.0", "1.2.0", "2.0.0")
    add("intl", "1.0.0", "1.1.0")
    add("root", "1.0.0")
    // Constraints
    dep("dropdown", "2.1.1", pg.Constraint{Pkg: "icons", Allow: semverInterval(&semver{2,0,0,""}, true, nil, false), Note: ">=2.0.0 icons"})
    dep("dropdown", "2.2.0", pg.Constraint{Pkg: "intl", Allow: semverInterval(&semver{1,1,0,""}, true, nil, false), Note: ">=1.1.0 intl"})
    return m
}

