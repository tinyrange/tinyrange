package pubgrub

import (
	"errors"
	"fmt"
	"strings"
)

// Explainer formats narrative error messages from conflicts.
type Explainer struct{}

func (e *Explainer) ErrorFromConflict(ic *Incompatibility) error {
	var b strings.Builder
	b.WriteString("Because ")
	for i, t := range ic.Terms {
		if i > 0 {
			if i == len(ic.Terms)-1 {
				b.WriteString(" and ")
			} else {
				b.WriteString(", ")
			}
		}
		if t.Positive {
			b.WriteString(fmt.Sprintf("%s is constrained to %s", t.Pkg, t.Allowed))
		} else {
			b.WriteString(fmt.Sprintf("%s is incompatible with %s", t.Pkg, t.Allowed))
		}
	}
	b.WriteString(", resolution failed")
	if ic.Cause != nil {
		b.WriteString(" (")
		b.WriteString(ic.Cause.String())
		b.WriteString(")")
	}
	b.WriteString(".")
	return errors.New(b.String())
}
