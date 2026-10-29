package protokit

import (
	"strings"
	"testing"

	"github.com/the-protobuf-project/protokit/schema"
)

func TestDeStutteredName(t *testing.T) {
	cases := []struct {
		name, schema string
		taken        []string
		want         string
	}{
		// Eponymous tables: stripping leaves nothing → generic word. "resource"
		// is preferred; the resource schema falls back to "entity" (it stutters).
		{"bookings", "booking", []string{"bookings"}, "resource"},
		{"schedules", "schedule", []string{"schedules"}, "resource"},
		{"organisations", "organisation", []string{"organisations"}, "resource"},
		{"promo_codes", "promocode", []string{"promo_codes"}, "resource"},
		{"resources", "resource", []string{"resources"}, "entity"},

		// Prefixed tables: drop the leading schema word, keep the remainder.
		{"schedule_exceptions", "schedule", []string{"schedule_exceptions"}, "exceptions"},
		{"promo_code_applicable_resources", "promocode", []string{"promo_code_applicable_resources"}, "applicable_resources"},

		// Prefixed table whose remainder collides with an existing table → _link.
		{"resource_offerings", "resource", []string{"resource_offerings", "offerings"}, "offerings_link"},

		// No stutter: left unchanged.
		{"contacts", "booking", []string{"contacts"}, "contacts"},
		{"users", "identity", []string{"users"}, "users"},
		{"books", "bookstore_v1", []string{"books"}, "books"},
	}
	for _, c := range cases {
		taken := map[string]bool{}
		for _, n := range c.taken {
			taken[n] = true
		}
		if got := deStutteredName(c.name, c.schema, taken); got != c.want {
			t.Errorf("deStutteredName(%q, %q) = %q, want %q", c.name, c.schema, got, c.want)
		}
	}
}

// TestDeStutterKeepEponymous: with the layout keeping eponymous tables, an
// eponymous table keeps its plural name while a prefixed one is still
// shortened, and without it the generic word is used as before.
func TestDeStutterKeepEponymous(t *testing.T) {
	build := func() *schema.Database {
		return &schema.Database{Schemas: []*schema.Schema{
			{Name: "venue", Tables: []*schema.Table{{Name: "venues"}, {Name: "spaces"}, {Name: "venue_photos"}}},
			{Name: "promocode", Tables: []*schema.Table{{Name: "promo_codes"}}},
		}}
	}
	names := func(db *schema.Database) []string {
		var out []string
		for _, s := range db.Schemas {
			for _, t := range s.Tables {
				out = append(out, s.Name+"."+t.Name)
			}
		}
		return out
	}
	db := build()
	deStutterTables(db, false)
	if got, want := strings.Join(names(db), ","), "venue.resource,venue.spaces,venue.photos,promocode.resource"; got != want {
		t.Errorf("default: got %s, want %s", got, want)
	}
	db = build()
	deStutterTables(db, true)
	if got, want := strings.Join(names(db), ","), "venue.venues,venue.spaces,venue.photos,promocode.promo_codes"; got != want {
		t.Errorf("keep eponymous: got %s, want %s", got, want)
	}
}

type eponymousLayout struct{ keep bool }

func (eponymousLayout) ResolveDatasource(string) (string, string, bool, bool) {
	return "", "", false, false
}
func (eponymousLayout) DedupeSchemaTable() bool     { return true }
func (l eponymousLayout) KeepEponymousTables() bool { return l.keep }

// TestKeepEponymousTablesOptional: the policy is an optional interface; a
// layout without it, or a nil one, keeps the old renaming.
func TestKeepEponymousTablesOptional(t *testing.T) {
	if schema.KeepEponymousTables(nil) {
		t.Error("nil layout keeps eponymous tables")
	}
	if !schema.KeepEponymousTables(eponymousLayout{keep: true}) {
		t.Error("layout asking to keep eponymous tables is ignored")
	}
	if schema.KeepEponymousTables(eponymousLayout{}) {
		t.Error("layout declining is read as keeping")
	}
}
