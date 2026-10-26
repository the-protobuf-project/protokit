package naming

import (
	"strings"
	"testing"
)

func TestFitIdentifier(t *testing.T) {
	if got := FitIdentifier("SHORT"); got != "SHORT" {
		t.Errorf("short: %q", got)
	}
	a := FitIdentifier(strings.Repeat("A", 70) + "_ONE")
	b := FitIdentifier(strings.Repeat("A", 70) + "_TWO")
	if len(a) != MaxIdentifier || len(b) != MaxIdentifier || a == b {
		t.Errorf("long: %q (%d), %q (%d)", a, len(a), b, len(b))
	}
	if a != FitIdentifier(strings.Repeat("A", 70)+"_ONE") {
		t.Error("not deterministic")
	}
}
