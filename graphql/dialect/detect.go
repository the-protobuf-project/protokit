package dialect

import "strings"

// Detect guesses the engine behind a schema from its type names, returning
// the matching registered dialect or nil when no engine convention is
// recognized (a hand-written schema). typeNames is every named type.
func Detect(typeNames []string) Dialect {
	hits := 0
	for _, n := range typeNames {
		if strings.HasSuffix(n, "_bool_exp") || strings.HasSuffix(n, "BoolExp") ||
			strings.HasSuffix(n, "_mutation_response") || strings.HasSuffix(n, "_comparison_exp") {
			hits++
		}
	}
	if hits >= 2 {
		return registry["hasura"]
	}
	return nil
}
