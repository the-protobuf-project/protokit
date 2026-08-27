package buffers

import (
	"strings"
	"testing"
)

func TestParseStrictRejectsARuleSetTwice(t *testing.T) {
	// Same reasoning as the unknown-rule check: the spec is hand-typed in a
	// buf.gen.yaml, and quietly keeping the last of two settings means the one the
	// author believed they wrote never applied.
	_, err := ParseStrict("ordinal:error,ordinal:warn")
	if err == nil {
		t.Fatal("a rule set twice parsed successfully")
	}
	if !strings.Contains(err.Error(), "ordinal") {
		t.Errorf("error does not name the duplicated rule: %v", err)
	}

	if _, err := ParseStrict("ordinal:error,lint:warn"); err != nil {
		t.Errorf("distinct rules must still parse: %v", err)
	}
}
