package pubgrub

import "fmt"

// anyRange allows all versions.
type anyRange struct{}

func (a anyRange) Contains(v Version, _ Comparator) bool     { return true }
func (a anyRange) Intersect(other Range, _ Comparator) Range { return other }
func (a anyRange) IsEmpty() bool                             { return false }
func (a anyRange) String() string                            { return "*" }

// emptyRange forbids all versions.
type emptyRange struct{}

func (e emptyRange) Contains(v Version, _ Comparator) bool     { return false }
func (e emptyRange) Intersect(other Range, _ Comparator) Range { return e }
func (e emptyRange) IsEmpty() bool                             { return true }
func (e emptyRange) String() string                            { return "∅" }

// singletonRange restricts to an exact version.
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

// intervalRange restricts to (possibly unbounded) interval with inclusivity flags.
type intervalRange struct {
	Lower    Version // nil => -inf
	Upper    Version // nil => +inf
	IncLower bool
	IncUpper bool
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
		if oo.Lower != nil {
			if lo == nil || cmp.Compare(oo.Lower, lo) > 0 {
				lo = oo.Lower
				incLo = oo.IncLower
			} else if cmp.Compare(oo.Lower, lo) == 0 {
				incLo = incLo && oo.IncLower
			}
		}
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

// negatedRange disallows a subrange from any.
type negatedRange struct{ Disallow Range }

func (n negatedRange) Contains(v Version, cmp Comparator) bool { return !n.Disallow.Contains(v, cmp) }
func (n negatedRange) Intersect(o Range, cmp Comparator) Range {
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
		inter := oo.Intersect(n.Disallow, cmp)
		if !inter.IsEmpty() && rangeString(inter) == rangeString(oo) {
			return emptyRange{}
		}
		return o
	}
}
func (n negatedRange) IsEmpty() bool  { return false }
func (n negatedRange) String() string { return fmt.Sprintf("!*%s", n.Disallow.String()) }

// unionRange models a union of two ranges (used to accumulate disallowed parts).
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

// Exported helpers
func Any() Range                { return anyRange{} }
func Empty() Range              { return emptyRange{} }
func Singleton(v Version) Range { return singletonRange{V: v} }
func Interval(lower Version, incLower bool, upper Version, incUpper bool) Range {
	return intervalRange{Lower: lower, Upper: upper, IncLower: incLower, IncUpper: incUpper}
}

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
