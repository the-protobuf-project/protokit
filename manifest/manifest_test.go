package manifest

import (
	"strings"
	"testing"
)

const valid = `
provides: store
requires: [protokit]
annotations:
  buf.build/the-protobuf-project/protokit: ">=1.2.0"
runtime:
  go: ">=1.26"
facets:
  reads: [protokit.v1]
  optional_reads: [store.v1]
outputs: ["**/*.go", "**/*.sql"]
`

func TestParse(t *testing.T) {
	m, err := Parse([]byte(valid))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Provides != "store" {
		t.Errorf("Provides = %q, want %q", m.Provides, "store")
	}
	if got := m.Annotations["buf.build/the-protobuf-project/protokit"]; got != ">=1.2.0" {
		t.Errorf("annotation constraint = %q, want %q", got, ">=1.2.0")
	}
	if len(m.Facets.Reads) != 1 || m.Facets.Reads[0] != "protokit.v1" {
		t.Errorf("Facets.Reads = %v, want [protokit.v1]", m.Facets.Reads)
	}
	if len(m.Outputs) != 2 {
		t.Errorf("Outputs = %v, want 2 entries", m.Outputs)
	}
}

// A minimal manifest is valid: only `provides` is required. A plugin that reads
// no facets and declares no outputs is unusual but not malformed.
func TestParseMinimal(t *testing.T) {
	if _, err := Parse([]byte("provides: store\n")); err != nil {
		t.Fatalf("Parse minimal: %v", err)
	}
}

// An unknown key must fail rather than be ignored — the whole reason for the
// strict decode.
func TestParseRejectsUnknownField(t *testing.T) {
	src := "provides: store\nfacets:\n  optional_read: [store.v1]\n"
	_, err := Parse([]byte(src))
	if err == nil {
		t.Fatal("Parse accepted an unknown field; want an error")
	}
	if !strings.Contains(err.Error(), "optional_read") {
		t.Errorf("error should name the unknown field, got: %v", err)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		m    Manifest
		want string // substring the error must contain; "" means valid
	}{
		{
			name: "valid",
			m:    Manifest{Provides: "store"},
		},
		{
			name: "provides missing",
			m:    Manifest{},
			want: "provides is required",
		},
		{
			name: "provides not a key",
			m:    Manifest{Provides: "Store V1"},
			want: "not a valid vocabulary key",
		},
		{
			name: "facet both required and optional",
			m: Manifest{
				Provides: "store",
				Facets:   Facets{Reads: []string{"protokit.v1"}, OptionalReads: []string{"protokit.v1"}},
			},
			want: "either required or it is not",
		},
		{
			name: "reads own vocabulary",
			m: Manifest{
				Provides: "store",
				Facets:   Facets{Reads: []string{"store"}},
			},
			want: "which this plugin provides",
		},
		{
			name: "duplicate require",
			m:    Manifest{Provides: "store", Requires: []string{"protokit", "protokit"}},
			want: "more than once",
		},
		{
			name: "empty constraint",
			m:    Manifest{Provides: "store", Runtime: map[string]string{"go": ""}},
			want: "empty version constraint",
		},
		{
			name: "malformed constraint",
			m:    Manifest{Provides: "store", Runtime: map[string]string{"go": "at least 1.26"}},
			want: "malformed",
		},
		{
			name: "absolute output glob",
			m:    Manifest{Provides: "store", Outputs: []string{"/etc/passwd"}},
			want: "is absolute",
		},
		{
			name: "malformed output glob",
			m:    Manifest{Provides: "store", Outputs: []string{"**/[a-.go"}},
			want: "not a valid glob",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.m.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Validate: unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate: no error; want one containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate error = %v\nwant it to contain %q", err, tc.want)
			}
		})
	}
}

// Validate reports every problem at once, so a hand-edited manifest takes one
// round trip to fix rather than one per mistake.
func TestValidateReportsAllProblems(t *testing.T) {
	m := Manifest{
		Requires: []string{""},
		Runtime:  map[string]string{"go": "at least 1.26"},
		Outputs:  []string{"/abs"},
	}
	err := m.Validate()
	if err == nil {
		t.Fatal("Validate: no error; want several")
	}
	for _, want := range []string{"provides is required", "requires[0] is empty", "malformed", "is absolute"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got:\n%v", want, err)
		}
	}
}

func TestValidConstraint(t *testing.T) {
	ok := []string{"*", "1.26", "1.2.3", ">=1.2.0", "<=2", "^1.2", "~1.2.3", "!=1.0.0", ">=1.2 <2.0", "1.2.3-rc.1"}
	for _, c := range ok {
		if err := validConstraint(c); err != nil {
			t.Errorf("validConstraint(%q) = %v, want nil", c, err)
		}
	}
	bad := []string{"latest", ">= 1.2.0 or 2", "v1.2.3", "1.2.3+", "=>1.0"}
	for _, c := range bad {
		if err := validConstraint(c); err == nil {
			t.Errorf("validConstraint(%q) = nil, want an error", c)
		}
	}
}
