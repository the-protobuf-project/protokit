package golden

// determinism.go asserts that a case generates the same bytes twice.
//
// The failure this catches is specific and recurring: Go randomizes map iteration
// order on every range, so a pass that ranges a map straight into output produces
// output that differs run to run. A golden test does not reliably catch it — the
// committed golden was written by one particular run, and a rerun agrees with it
// often enough to look stable. Generating twice in one process and comparing is
// what turns "usually passes" into a hard signal.

import (
	"sort"
	"testing"

	"github.com/the-protobuf-project/protokit"
)

// Determinism generates dir's case twice with the same plugin and fails on any
// byte difference between the two runs.
//
// It runs the targets named by the case's optional "targets" file, defaulting to
// every target in pl.Registry — a case that pins a subset for its goldens is still
// worth checking for determinism on that subset only.
//
// Each run gets a fresh protogen.Plugin from the same CodeGeneratorRequest, so the
// two differ in nothing but map iteration order and any state a target carries
// between runs. Both are bugs, and both show up here as a diff.
func Determinism(t *testing.T, dir string, pl protokit.Plugin) {
	t.Helper()
	req := BuildRequest(t, dir)

	for _, target := range CaseTargets(t, dir, registryTargets(pl)) {
		if _, ok := pl.Registry[target]; !ok {
			continue // a target this module doesn't ship
		}
		t.Run(target, func(t *testing.T) {
			first := runTarget(t, req, target, pl)
			second := runTarget(t, req, target, pl)
			compareRuns(t, first, second)
		})
	}
}

// compareRuns reports every file that differs between two generations of the same
// case, in sorted order so the report itself is deterministic.
func compareRuns(t *testing.T, first, second map[string]string) {
	t.Helper()

	names := map[string]bool{}
	for n := range first {
		names[n] = true
	}
	for n := range second {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	for _, name := range sorted {
		a, inA := first[name]
		b, inB := second[name]
		switch {
		case !inA:
			t.Errorf("non-deterministic output: %s generated on the second run only", name)
		case !inB:
			t.Errorf("non-deterministic output: %s generated on the first run only", name)
		case a != b:
			t.Errorf("non-deterministic output: %s differs between two runs of the same input.\n%s\n"+
				"This is almost always a map ranged directly into output — sort the keys first.",
				name, firstDiff(a, b))
		}
	}
}

// registryTargets returns pl's target keys, sorted — the default target set when a
// case ships no "targets" file.
func registryTargets(pl protokit.Plugin) []string {
	out := make([]string, 0, len(pl.Registry))
	for k := range pl.Registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
