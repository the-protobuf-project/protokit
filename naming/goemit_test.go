package naming

import "testing"

func TestDoc(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"   \n\t ", ""},
		{"Human-readable name.", "\t// Human-readable name.\n"},
		{"line one\nline two", "\t// line one\n\t// line two\n"},
		{"trailing space   \nkept trimmed  ", "\t// trailing space\n\t// kept trimmed\n"},
	}
	for _, c := range cases {
		if got := Doc(c.in); got != c.want {
			t.Errorf("Doc(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGoFileName(t *testing.T) {
	used := map[string]bool{}
	cases := []struct {
		name, guard, want string
	}{
		{"VenueKind", "enum", "venue_kind.go"},
		{"Kind", "enum", "kind.go"},
		{"PropertyUnits", "schema", "property_units.go"},
		// trailing word is a GOOS ("windows") → guarded so it isn't build-constrained.
		{"FooWindows", "enum", "foo_windows_enum.go"},
		// single word that is itself a constraint ("test") → guarded.
		{"Test", "schema", "test_schema.go"},
		// no guard requested: constrained name is left as-is.
		{"BarLinux", "", "bar_linux.go"},
	}
	for _, c := range cases {
		if got := GoFileName(c.name, c.guard, used); got != c.want {
			t.Errorf("GoFileName(%q, %q) = %q, want %q", c.name, c.guard, got, c.want)
		}
	}
}

func TestGoFileNameDedup(t *testing.T) {
	used := map[string]bool{}
	// Two different type names that snake to the same base get distinct files.
	if got := GoFileName("Order", "schema", used); got != "order.go" {
		t.Fatalf("first = %q, want order.go", got)
	}
	if got := GoFileName("Order", "schema", used); got != "order2.go" {
		t.Fatalf("second = %q, want order2.go", got)
	}
}

func TestUnique(t *testing.T) {
	used := map[string]bool{}
	for i, want := range []string{"name", "name2", "name3"} {
		if got := Unique("name", used); got != want {
			t.Errorf("call %d = %q, want %q", i, got, want)
		}
	}
	// A pre-occupied base skips straight to a suffix.
	used2 := map[string]bool{"x": true}
	if got := Unique("x", used2); got != "x2" {
		t.Errorf("occupied base = %q, want x2", got)
	}
}

func TestGoKeyword(t *testing.T) {
	for _, kw := range []string{"type", "range", "map", "func", "go"} {
		if !GoKeyword(kw) {
			t.Errorf("GoKeyword(%q) = false, want true", kw)
		}
	}
	for _, ok := range []string{"typ", "author", "id", ""} {
		if GoKeyword(ok) {
			t.Errorf("GoKeyword(%q) = true, want false", ok)
		}
	}
}
