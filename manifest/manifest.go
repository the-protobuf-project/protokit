// Package manifest defines the file a protokit plugin ships to declare what it
// is: the vocabulary it provides, the modules it needs, the facets it reads, and
// the outputs it writes.
//
// This package is schema and validation only. It parses a manifest, checks that
// it is well formed, and stops. It resolves nothing: it does not fetch a module,
// does not compare two version constraints, and does not decide whether a set of
// plugins can run together. Those are real problems, and solving them badly —
// early, inside the type that everything else will depend on — is how a format
// becomes impossible to change. The declaration comes first; whatever consumes it
// comes later and separately.
//
// A manifest is therefore useful today for exactly two things: telling a human
// what a plugin expects, and failing fast when a plugin's own declaration is
// self-contradictory.
package manifest

import (
	"bytes"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manifest is a plugin's declaration of itself.
//
// Example:
//
//	provides: store
//	requires: [protokit]
//	annotations:
//	  buf.build/the-protobuf-project/protokit: ">=1.2.0"
//	runtime:
//	  go: ">=1.26"
//	facets:
//	  reads: [protokit.v1]
//	  optional_reads: [store.v1]
//	outputs: ["**/*.go", "**/*.sql"]
type Manifest struct {
	// Provides is the plugin's own vocabulary key — the name other plugins and
	// manifests refer to it by, and the key its FacetReader registers under.
	// Required.
	Provides string `yaml:"provides"`

	// Requires lists the plugins this one needs present in a run, by their
	// Provides name. It expresses composition, not versions; version constraints
	// on proto modules belong in Annotations.
	Requires []string `yaml:"requires"`

	// Annotations maps a proto module path to the version constraint this plugin
	// needs of it, e.g. "buf.build/the-protobuf-project/protokit": ">=1.2.0".
	Annotations map[string]string `yaml:"annotations"`

	// Runtime maps a language to the toolchain version constraint the plugin's
	// generated output requires, e.g. "go": ">=1.26". This is a property of what
	// the plugin *emits*, not of what builds the plugin.
	Runtime map[string]string `yaml:"runtime"`

	// Facets declares which facet vocabularies the plugin consumes.
	Facets Facets `yaml:"facets"`

	// Outputs are glob patterns describing the files the plugin writes, relative
	// to its output directory. They document a plugin's footprint — enough to spot
	// two plugins that would write the same path.
	Outputs []string `yaml:"outputs"`
}

// Facets declares the facet vocabularies a plugin reads from the IR.
//
// The distinction between the two lists is a statement about failure: a missing
// required facet means the plugin cannot do its job, while a missing optional one
// means it does less. Nothing in this package enforces that — it is a declaration
// for whatever eventually composes plugins.
type Facets struct {
	// Reads lists facet keys the plugin needs present.
	Reads []string `yaml:"reads"`

	// OptionalReads lists facet keys the plugin uses when present and does
	// without otherwise.
	OptionalReads []string `yaml:"optional_reads"`
}

// Parse decodes a manifest from YAML and validates it.
//
// Decoding is strict: an unknown field is an error, not a silently ignored line.
// A manifest is small, hand-written, and rarely read back, which is exactly the
// shape of file where a typo'd key goes unnoticed for months — `optional_read`
// for `optional_reads` would otherwise parse cleanly and declare nothing.
func Parse(data []byte) (*Manifest, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest: parse: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate reports every problem it finds in one error, rather than the first.
// A manifest is edited by hand and checked in a batch, so surfacing one problem
// per run turns a single fix into several round trips.
//
// It checks that the declaration is well formed and self-consistent. It does not
// check that anything referenced exists or is satisfiable — no module is fetched
// and no constraint is compared against a real version.
func (m *Manifest) Validate() error {
	var problems []string
	report := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if strings.TrimSpace(m.Provides) == "" {
		report("provides is required (the plugin's own vocabulary key, e.g. \"store\")")
	} else if !validKey(m.Provides) {
		report("provides %q is not a valid vocabulary key: %s", m.Provides, keySyntax)
	}

	checkList(report, "requires", m.Requires, validKey, keySyntax)
	checkConstraints(report, "annotations", m.Annotations)
	checkConstraints(report, "runtime", m.Runtime)
	checkList(report, "facets.reads", m.Facets.Reads, validKey, keySyntax)
	checkList(report, "facets.optional_reads", m.Facets.OptionalReads, validKey, keySyntax)

	// A facet cannot be both required and optional: the two lists give opposite
	// answers to "may this plugin run without it?".
	optional := map[string]bool{}
	for _, f := range m.Facets.OptionalReads {
		optional[f] = true
	}
	for _, f := range m.Facets.Reads {
		if optional[f] {
			report("facet %q is listed in both facets.reads and facets.optional_reads; it is either required or it is not", f)
		}
	}

	// Reading one's own facets is not wrong, but it is always a mistake in a
	// manifest: the plugin produces them, so declaring a dependency on them says
	// nothing and hides what it actually needs from others.
	for _, f := range m.Facets.Reads {
		if f == m.Provides {
			report("facets.reads lists %q, which this plugin provides; list only vocabularies it consumes from others", f)
		}
	}

	for i, g := range m.Outputs {
		switch {
		case strings.TrimSpace(g) == "":
			report("outputs[%d] is empty", i)
		case path.IsAbs(g):
			report("outputs[%d] %q is absolute; output globs are relative to the plugin's output directory", i, g)
		default:
			// path.Match rejects a malformed pattern (an unclosed [ or a bad
			// range) independently of what it is matched against.
			if _, err := path.Match(g, ""); err != nil {
				report("outputs[%d] %q is not a valid glob: %v", i, g, err)
			}
		}
	}

	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("manifest: %d problem(s):\n  - %s", len(problems), strings.Join(problems, "\n  - "))
}

// checkList reports empty, duplicate, and malformed entries in a list of keys.
func checkList(report func(string, ...any), field string, list []string, valid func(string) bool, syntax string) {
	seen := map[string]bool{}
	for i, v := range list {
		switch {
		case strings.TrimSpace(v) == "":
			report("%s[%d] is empty", field, i)
		case seen[v]:
			report("%s lists %q more than once", field, v)
		case !valid(v):
			report("%s[%d] %q is not a valid key: %s", field, i, v, syntax)
		}
		seen[v] = true
	}
}

// checkConstraints reports malformed entries in a name → version-constraint map.
func checkConstraints(report func(string, ...any), field string, m map[string]string) {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names) // deterministic problem order

	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			report("%s has an empty key", field)
			continue
		}
		c := m[name]
		if strings.TrimSpace(c) == "" {
			report("%s.%s has an empty version constraint (use \"*\" to accept any version)", field, name)
			continue
		}
		if err := validConstraint(c); err != nil {
			report("%s.%s constraint %q is malformed: %v", field, name, c, err)
		}
	}
}

// keySyntax describes the vocabulary-key grammar, quoted in error messages.
const keySyntax = `expected lowercase letters, digits, ".", "-", or "_" (e.g. "protokit.v1")`

// keyPattern matches a vocabulary key: a facet key, a plugin name. Deliberately
// narrow — these become map keys, file names, and diagnostic text, so anything
// needing quoting or case-folding is rejected at the door.
var keyPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`)

func validKey(s string) bool { return keyPattern.MatchString(s) }

// comparatorPattern matches one version comparator: an optional operator followed
// by a dotted version, with an optional prerelease or build suffix. "1.26",
// ">=1.2.0", "^1", "~1.2.3-rc.1" all match.
var comparatorPattern = regexp.MustCompile(`^(=|!=|>|<|>=|<=|\^|~)?[0-9]+(\.[0-9]+)*([.-][0-9A-Za-z.-]+)?$`)

// validConstraint checks a version constraint's *syntax* only.
//
// The grammar is a space-separated conjunction of comparators (">=1.2 <2.0"), or
// the wildcard "*". Whether any version satisfies it — indeed whether the
// comparators contradict each other — is a resolver's question, and this package
// deliberately has no resolver. Catching a typo'd constraint is worth doing here;
// deciding what it means is not.
func validConstraint(c string) error {
	c = strings.TrimSpace(c)
	if c == "*" {
		return nil
	}
	for part := range strings.FieldsSeq(c) {
		if !comparatorPattern.MatchString(part) {
			return fmt.Errorf("%q is not a version comparator (want e.g. \">=1.2.0\", \"^1.2\", \"1.26\", or \"*\")", part)
		}
	}
	return nil
}
