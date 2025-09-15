package pubgrub

import "sort"

// FewestVersionsPrioritizer chooses the next package with fewest candidates.
type FewestVersionsPrioritizer struct {
	Prov Provider
	Cmp  Comparator
}

func (p FewestVersionsPrioritizer) ChooseNextPackage(allowed map[PackageID]Range, decided map[PackageID]bool) (PackageID, bool) {
	type cand struct {
		id    PackageID
		count int
	}
	cs := []cand{}
	for id, r := range allowed {
		if decided[id] {
			continue
		}
		vers, err := p.Prov.ListVersions(id)
		if err != nil {
			continue
		}
		count := len(filterByRange(vers, r, p.Cmp))
		cs = append(cs, cand{id: id, count: count})
	}
	if len(cs) == 0 {
		return "", false
	}
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].count != cs[j].count {
			return cs[i].count < cs[j].count
		}
		return cs[i].id < cs[j].id
	})
	return cs[0].id, true
}

// NewestFirstChooser orders candidate versions by Comparator descending.
type NewestFirstChooser struct {
	Prov Provider
	Cmp  Comparator
}

func (c NewestFirstChooser) CandidateVersions(pkg PackageID, allowed Range, all []Version, cmp Comparator) []Version {
	sorted := append([]Version(nil), all...)
	sort.Slice(sorted, func(i, j int) bool {
		ci := cmp.Compare(sorted[i], sorted[j])
		if ci != 0 {
			return ci > 0
		}
		return sorted[i].String() > sorted[j].String()
	})
	return sorted
}
