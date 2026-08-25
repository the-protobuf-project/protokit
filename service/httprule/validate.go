package httprule

// validate.go enforces the three structural rules the grammar itself cannot
// express: "**" only at the end, no nested variables, no field captured twice.

import (
	"fmt"
	"strings"
)

// validate enforces the rules the grammar cannot express.
func validate(t *Template) error {
	seen := map[string]bool{}
	flat := 0 // running index into the flattened segment sequence
	multiAt := -1

	note := func(k Kind) {
		if k == KindMulti && multiAt < 0 {
			multiAt = flat
		}
		flat++
	}

	for _, s := range t.Segments {
		if s.Kind != KindVariable {
			note(s.Kind)
			continue
		}
		key := strings.Join(s.Field, ".")
		if seen[key] {
			return &SyntaxError{Template: t.Raw,
				Msg: fmt.Sprintf("field %q is captured twice; each field may be bound by at most one variable", key)}
		}
		seen[key] = true
		for _, sub := range s.Sub {
			note(sub.Kind)
		}
	}

	if multiAt >= 0 && multiAt != flat-1 {
		return &SyntaxError{Template: t.Raw,
			Msg: fmt.Sprintf("%q must be the final segment, but %d more follow it", "**", flat-1-multiAt)}
	}
	return nil
}
