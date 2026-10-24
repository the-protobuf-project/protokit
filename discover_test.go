package protokit_test

// discover_test.go builds testdata/discover, which mirrors freebusy and the RFC
// module: freebusy's allocation package is generated in one run and references
// the RFC organizational unit (as its holds' parent) and the RFC resource (by
// type), which a separate run generates. Discovery finds them in a reference
// image, or in an import the request carries.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/golden"
	"github.com/the-protobuf-project/protokit/schema"
)

// rfcReader routes packages the way freebusy's store.backend.yaml does:
// freebusy.<svc>.v1 into database "freebusy", schema <svc>; protobuf.* into
// database "rfc", the organizational unit into schema "orgunit". Both share
// the physical database.
type rfcReader struct{ xrefReader }

func (rfcReader) Key() string { return "rfc" }

func (rfcReader) ReadDatasource(fd protoreflect.FileDescriptor) schema.Datasource {
	parts := strings.Split(string(fd.Package()), ".")
	ds := schema.Datasource{Provider: "postgres", SchemaStrip: true}
	switch {
	case parts[0] == "freebusy":
		ds.Database, ds.Schema = "freebusy", parts[1]
	case parts[0] != "protobuf":
		ds.Database = "other" // google.api and the like: no resources
	case parts[2] == "organizational_unit":
		ds.Database, ds.Schema = "rfc", "orgunit"
	default:
		ds.Database, ds.Schema = "rfc", parts[2]
	}
	return ds
}

// writeImage compiles dir and writes it as a binary FileDescriptorSet, the
// format `buf build -o x.binpb` writes.
func writeImage(t *testing.T, dir string) string {
	t.Helper()
	req := golden.BuildRequest(t, dir)
	raw, err := proto.Marshal(&descriptorpb.FileDescriptorSet{File: req.GetProtoFile()})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rfc.binpb")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func buildDiscover(t *testing.T, dir, strict string, refs ...string) (*schema.IR, error) {
	t.Helper()
	p, err := protogen.Options{}.New(golden.BuildRequest(t, dir))
	if err != nil {
		t.Fatalf("protogen: %v", err)
	}
	opts := protokit.Options{Strict: strict, References: refs}
	return protokit.Build(p, opts, []schema.FacetReader{rfcReader{}}, nil)
}

// rfcOwn builds the RFC fixture as its own run would, for the names its tables
// really get.
func rfcOwn(t *testing.T) *schema.IR {
	t.Helper()
	ir, err := buildDiscover(t, "testdata/discover/rfc", "ref:error")
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func TestDiscoverFromReferenceImage(t *testing.T) {
	image := writeImage(t, "testdata/discover/rfc")
	_, err := buildDiscover(t, "testdata/discover/run", "ref:error", image)
	if err == nil || strings.Contains(err.Error(), `"resource"`) || strings.Contains(err.Error(), "organizational_unit") {
		t.Fatalf("strict build error = %v, want only the ghost reported", err)
	}
	ir, err := buildDiscover(t, "testdata/discover/run", "", image)
	if err != nil {
		t.Fatal(err)
	}

	// Nothing from the image is generated.
	for _, db := range ir.Databases {
		if db.Name != "freebusy" {
			t.Errorf("run built database %q from a reference image", db.Name)
		}
	}

	// The real names, as the RFC run builds them.
	own := rfcOwn(t)
	orgunits := xrefTable(t, own, "rfc", "organizational_units")
	resources := xrefTable(t, own, "rfc", "resources")

	holds := xrefTable(t, ir, "freebusy", "holds")
	allocations := xrefTable(t, ir, "freebusy", "allocations")
	for _, tc := range []struct {
		tb       *schema.Table
		col      string
		want     string
		storedBy string // the target column holding what the referencing column stores
	}{
		// A parent stores the bare parent id: the RFC table's primary key.
		{holds, "organizational_unit_id", "rfc:orgunit.organizational_units." + orgunits.PKColumn, "id"},
		{allocations, "organizational_unit_id", "rfc:orgunit.organizational_units." + orgunits.PKColumn, "id"},
		// A resource_reference with no association stores the full name.
		{allocations, "resource", "rfc:resource.resources.name", "name"},
	} {
		if fk := fkOn(tc.tb.ForeignKeys, tc.col); fk != nil {
			t.Errorf("%s.%s is a relation %+v, want constraint only", tc.tb.Name, tc.col, fk)
		}
		fk := fkOn(tc.tb.ExternalForeignKeys, tc.col)
		if fk == nil {
			t.Fatalf("%s.%s: no discovered FK", tc.tb.Name, tc.col)
		}
		if got := target(fk); got != tc.want {
			t.Errorf("%s.%s → %s, want %s", tc.tb.Name, tc.col, got, tc.want)
		}
		if fk.Kind != schema.ReferenceDiscovered || fk.OnDelete != "NO ACTION" {
			t.Errorf("%s.%s kind %s ON DELETE %q, want discovered NO ACTION", tc.tb.Name, tc.col, fk.Kind, fk.OnDelete)
		}
		if c := columnOf(tc.tb, tc.col); c.FKModel != "" {
			t.Errorf("%s.%s FKModel = %q, want none", tc.tb.Name, tc.col, c.FKModel)
		}
	}
	if resources.PKColumn == "name" {
		t.Fatal("fixture: resources keyed by name cannot tell the columns apart")
	}

	// Same run, nested target: a relation to the hold's name.
	hold := fkOn(allocations.ForeignKeys, "hold")
	if hold == nil || hold.Kind != schema.ReferenceLocal || target(hold) != ":allocation.holds.name" {
		t.Errorf("hold FK = %+v, want local :allocation.holds.name", hold)
	}
}

func TestDiscoverMiss(t *testing.T) {
	// Without a reference image nothing knows the RFC module: every reference
	// to it stays a soft FK, and says why.
	_, err := buildDiscover(t, "testdata/discover/run", "ref:error")
	if err == nil {
		t.Fatal("strict build passed, want the unresolved references reported")
	}
	for _, want := range []string{`column "resource"`, `column "organizational_unit_id"`, `column "ghost"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("strict error lacks %s:\n%v", want, err)
		}
	}

	// With the image, the ghost (defined nowhere) still misses.
	ir, err := buildDiscover(t, "testdata/discover/run", "", writeImage(t, "testdata/discover/rfc"))
	if err != nil {
		t.Fatal(err)
	}
	allocations := xrefTable(t, ir, "freebusy", "allocations")
	ghost := fkOn(allocations.UnresolvedForeignKeys, "ghost")
	if ghost == nil || ghost.Kind != schema.ReferenceUnresolved || ghost.Unresolved != schema.UnresolvedNotFound {
		t.Fatalf("ghost = %+v, want unresolved (not-found)", ghost)
	}
	if fkOn(allocations.ExternalForeignKeys, "ghost") != nil || fkOn(allocations.ForeignKeys, "ghost") != nil {
		t.Fatal("ghost has a constraint; a miss must never dangle")
	}
}

func TestDiscoverFromImport(t *testing.T) {
	// The request carries an imported file it does not generate: discovery
	// needs no image.
	ir, err := buildDiscover(t, "testdata/discover/importing", "ref:error")
	if err != nil {
		t.Fatal(err)
	}
	wards := xrefTable(t, ir, "freebusy", "wards")
	fk := fkOn(wards.ExternalForeignKeys, "organizational_unit")
	if fk == nil || fk.Kind != schema.ReferenceDiscovered || target(fk) != "rfc:orgunit.organizational_units.name" {
		t.Fatalf("ward organizational_unit = %+v, want discovered rfc:orgunit.organizational_units.name", fk)
	}
	for _, db := range ir.Databases {
		if db.Name != "freebusy" {
			t.Errorf("run built database %q from an import", db.Name)
		}
	}
}

func TestDiscoveredMatchesOwnRun(t *testing.T) {
	// A discovered table carries the names its own run gives it: the RFC run's
	// own Resource → OrganizationalUnit relation and the discovered FK name the
	// same table.
	own := rfcOwn(t)
	ref := fkOn(xrefTable(t, own, "rfc", "resources").ForeignKeys, "organizational_unit")
	if ref == nil || ref.Kind != schema.ReferenceLocal {
		t.Fatalf("rfc resource → orgunit = %+v", ref)
	}
	if ref.ReferencedSchema != "orgunit" || ref.ReferencedTable != "organizational_units" {
		t.Fatalf("rfc run names orgunit %s.%s", ref.ReferencedSchema, ref.ReferencedTable)
	}
}

// TestFKColumnMatchesStoredValue checks, over every fixture, that each
// constraint references the target column holding what the referencing column
// stores, as the generated repositories write it: a relation to a top-level
// resource and a parent store the bare id (the PK); every other
// resource_reference stores the full name (the name column).
func TestFKColumnMatchesStoredValue(t *testing.T) {
	irs := map[string]*schema.IR{}
	var err error
	if irs["xref"], err = buildXref(t, ""); err != nil {
		t.Fatal(err)
	}
	irs["rfc"] = rfcOwn(t)
	if irs["discover"], err = buildDiscover(t, "testdata/discover/run", "", writeImage(t, "testdata/discover/rfc")); err != nil {
		t.Fatal(err)
	}
	topLevel := map[string]bool{} // schema.table → no parents
	for _, ir := range irs {
		for _, db := range ir.Databases {
			for _, s := range db.Schemas {
				for _, tb := range s.Tables {
					topLevel[s.Name+"."+tb.Name] = len(tb.Parents) == 0
				}
			}
		}
	}
	n := 0
	for name, ir := range irs {
		for _, db := range ir.Databases {
			for _, s := range db.Schemas {
				for _, tb := range s.Tables {
					for _, fk := range append(append([]*schema.ForeignKey{}, tb.ForeignKeys...), tb.ExternalForeignKeys...) {
						c := columnOf(tb, fk.Column)
						storesID := fk.ReferencedType == "" ||
							c.FKModel != "" && topLevel[fk.ReferencedSchema+"."+fk.ReferencedTable]
						want := "name"
						if storesID {
							want = "id"
						}
						if fk.ReferencedColumn != want {
							t.Errorf("%s: %s.%s.%s → %s.%s.%s; the column stores the %s",
								name, s.Name, tb.Name, fk.Column, fk.ReferencedSchema, fk.ReferencedTable,
								fk.ReferencedColumn, map[bool]string{true: "bare id", false: "full name"}[storesID])
						}
						n++
					}
				}
			}
		}
	}
	if n < 8 {
		t.Fatalf("checked %d foreign keys, want the fixtures' full set", n)
	}
}
