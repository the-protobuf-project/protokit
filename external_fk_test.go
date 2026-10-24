package protokit_test

// external_fk_test.go builds testdata/xref — four packages routed into three
// databases — and checks how each kind of reference lands:
//
//	same database        Booking.owner_id → owners.id, Booking.hold → holds.name
//	same database, type  Asset.resource → the qualified catalog Resource,
//	                     constraint only (the model-name rule never resolved it)
//	other database       Hold.resource → catalog resources.name, Hold.site_id → sites.id
//	unresolvable         Hold.ghost (no such type), Hold.remote (other physical DB)

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/golden"
	"github.com/the-protobuf-project/protokit/schema"
)

// xrefReader routes each package into a database: catalog and catalogx share
// "catalog", booking has its own, and remote sits in another physical database
// (its own URL). Every other read contributes nothing.
type xrefReader struct{}

func (xrefReader) Key() string                                       { return "xref" }
func (xrefReader) ReadFile(protoreflect.FileDescriptor) (any, error) { return nil, nil }
func (xrefReader) ReadMessage(protoreflect.MessageDescriptor) (any, error) {
	return nil, nil
}
func (xrefReader) ReadField(protoreflect.FieldDescriptor) (any, error) { return nil, nil }

func (xrefReader) ReadDatasource(fd protoreflect.FileDescriptor) schema.Datasource {
	pkg := string(fd.Package())
	ds := schema.Datasource{Provider: "postgres", SchemaStrip: true}
	switch {
	case strings.HasPrefix(pkg, "catalog"):
		ds.Database = "catalog"
	case strings.HasPrefix(pkg, "booking"):
		ds.Database = "booking"
	default:
		ds.Database, ds.URL = "remote", "postgres://elsewhere/remote"
	}
	return ds
}

func (xrefReader) ReadTable(protoreflect.MessageDescriptor) schema.TableStructure {
	return schema.TableStructure{}
}

func (xrefReader) ReadColumn(protoreflect.FieldDescriptor) schema.ColumnStructure {
	return schema.ColumnStructure{}
}

func buildXref(t *testing.T, strict string) (*schema.IR, error) {
	t.Helper()
	p, err := protogen.Options{}.New(golden.BuildRequest(t, "testdata/xref"))
	if err != nil {
		t.Fatalf("protogen: %v", err)
	}
	return protokit.Build(p, protokit.Options{Strict: strict}, []schema.FacetReader{xrefReader{}}, nil)
}

func xrefTable(t *testing.T, ir *schema.IR, db, table string) *schema.Table {
	t.Helper()
	for _, d := range ir.Databases {
		if d.Name != db {
			continue
		}
		for _, s := range d.Schemas {
			for _, tb := range s.Tables {
				if tb.Name == table {
					return tb
				}
			}
		}
	}
	t.Fatalf("no table %s.%s", db, table)
	return nil
}

func fkOn(fks []*schema.ForeignKey, col string) *schema.ForeignKey {
	for _, fk := range fks {
		if fk.Column == col {
			return fk
		}
	}
	return nil
}

func columnOf(tb *schema.Table, name string) *schema.Column {
	for _, c := range tb.Columns {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func indexed(tb *schema.Table, col string) bool {
	for _, idx := range tb.Indexes {
		if len(idx.Columns) > 0 && idx.Columns[0] == col {
			return true
		}
	}
	return false
}

// target renders an FK's target as db.schema.table.column (db empty in-database).
func target(fk *schema.ForeignKey) string {
	return fk.ReferencedDatabase + ":" + fk.ReferencedSchema + "." + fk.ReferencedTable + "." + fk.ReferencedColumn
}

func TestForeignKeysSameDatabase(t *testing.T) {
	ir, err := buildXref(t, "")
	if err != nil {
		t.Fatal(err)
	}
	bookings := xrefTable(t, ir, "booking", "bookings")

	// A parent in the same database: its id, cascading as an owned child does.
	owner := fkOn(bookings.ForeignKeys, "owner_id")
	if owner == nil || target(owner) != ":booking.owners.id" || owner.OnDelete != "CASCADE" {
		t.Fatalf("owner_id FK = %+v, want :booking.owners.id ON DELETE CASCADE", owner)
	}
	// A reference to a nested resource stores its whole name, so it references
	// the unique name column, not the id.
	hold := fkOn(bookings.ForeignKeys, "hold")
	if hold == nil || target(hold) != ":booking.holds.name" {
		t.Fatalf("hold FK = %+v, want :booking.holds.name", hold)
	}
	if c := columnOf(bookings, "hold"); c.FKModel != "Hold" {
		t.Fatalf("hold column FKModel = %q, want Hold (a relation)", c.FKModel)
	}
	for _, fk := range bookings.ForeignKeys {
		if fk.Kind != schema.ReferenceLocal {
			t.Errorf("bookings.%s kind %s, want local", fk.Column, fk.Kind)
		}
	}
	if len(bookings.ExternalForeignKeys) != 0 {
		t.Fatalf("bookings has external FKs %+v, want none", bookings.ExternalForeignKeys)
	}

	// The catalog Resource was qualified on a name collision, so the model-name
	// rule never resolved this reference and the column has always stored the
	// full name as written. The resource type now finds the table; the FK is a
	// constraint on the name column, and the column gains no association (which
	// would make a repository store the bare id instead).
	assets := xrefTable(t, ir, "catalog", "assets")
	if fk := fkOn(assets.ForeignKeys, "resource"); fk != nil {
		t.Fatalf("asset resource is a relation %+v, want constraint only", fk)
	}
	res := fkOn(assets.ExternalForeignKeys, "resource")
	if res == nil || target(res) != "catalog:catalog.resources.name" || res.Kind != schema.ReferenceLocal {
		t.Fatalf("asset resource FK = %+v, want local catalog:catalog.resources.name", res)
	}
	if res.ReferencedModel == "Resource" {
		t.Fatalf("asset resource FK model = %q, want the qualified name", res.ReferencedModel)
	}
	if c := columnOf(assets, "resource"); c.FKModel != "" {
		t.Fatalf("asset resource FKModel = %q, want none (stored as written)", c.FKModel)
	}
}

func TestForeignKeysOtherDatabase(t *testing.T) {
	ir, err := buildXref(t, "")
	if err != nil {
		t.Fatal(err)
	}
	holds := xrefTable(t, ir, "booking", "holds")

	for _, tc := range []struct {
		col, want string
	}{
		// External: stored as written (the full name), so the name column.
		{"resource", "catalog:catalog.resources.name"},
		// A parent holds the bare parent id, so the primary key.
		{"site_id", "catalog:catalog.sites.id"},
	} {
		if fk := fkOn(holds.ForeignKeys, tc.col); fk != nil {
			t.Errorf("%s is a relation FK %+v; an external FK must carry no relation", tc.col, fk)
		}
		fk := fkOn(holds.ExternalForeignKeys, tc.col)
		if fk == nil {
			t.Fatalf("%s: no external FK", tc.col)
		}
		if got := target(fk); got != tc.want {
			t.Errorf("%s → %s, want %s", tc.col, got, tc.want)
		}
		// The parent's synthesized CASCADE gives way: another database's purge
		// must not delete these rows, and a soft delete fires no action at all.
		if fk.OnDelete != "NO ACTION" {
			t.Errorf("%s ON DELETE %q, want NO ACTION", tc.col, fk.OnDelete)
		}
		if fk.Kind != schema.ReferenceCrossDatabase {
			t.Errorf("%s kind %s, want cross-database", tc.col, fk.Kind)
		}
		c := columnOf(holds, tc.col)
		if c.FKModel != "" {
			t.Errorf("%s FKModel = %q, want none (no association across databases)", tc.col, c.FKModel)
		}
		if !strings.Contains(c.Comment, "constraint only") {
			t.Errorf("%s comment = %q, want the external-FK note", tc.col, c.Comment)
		}
		if !indexed(holds, tc.col) {
			t.Errorf("%s is not indexed", tc.col)
		}
	}
	// Nothing in the catalog grows a back-relation to a table it cannot name.
	// (Asset's in-database reference is the resources table's only one.)
	for _, name := range []string{"resources", "sites"} {
		for _, hm := range xrefTable(t, ir, "catalog", name).HasMany {
			if hm.Model == "Hold" {
				t.Errorf("catalog %s has a back-relation to Hold via %s", name, hm.ViaFK)
			}
		}
	}
}

func TestForeignKeysUnresolvable(t *testing.T) {
	ir, err := buildXref(t, "")
	if err != nil {
		t.Fatal(err)
	}
	holds := xrefTable(t, ir, "booking", "holds")
	for _, tc := range []struct {
		col string
		why schema.UnresolvedReason
	}{
		{"ghost", schema.UnresolvedNotFound},
		{"remote", schema.UnresolvedOtherDatabase},
	} {
		col := tc.col
		if fk := fkOn(holds.UnresolvedForeignKeys, col); fk == nil ||
			fk.Kind != schema.ReferenceUnresolved || fk.Unresolved != tc.why {
			t.Errorf("%s unresolved record = %+v, want unresolved (%s)", col, fk, tc.why)
		}
		if fk := fkOn(holds.ForeignKeys, col); fk != nil {
			t.Errorf("%s has FK %+v, want none", col, fk)
		}
		if fk := fkOn(holds.ExternalForeignKeys, col); fk != nil {
			t.Errorf("%s has external FK %+v, want none (never dangling)", col, fk)
		}
		c := columnOf(holds, col)
		if c == nil || c.FKModel != "" || !strings.Contains(c.Comment, "TODO: unresolved") {
			t.Errorf("%s column = %+v, want a soft FK", col, c)
		}
		if !indexed(holds, col) {
			t.Errorf("%s is not indexed", col)
		}
	}

	// Each skip says why, and fails a strict build.
	_, err = buildXref(t, "ref:error")
	if err == nil {
		t.Fatal("strict ref build passed, want the unresolved references reported")
	}
	for _, want := range []string{
		`column "ghost" references unknown model "Ghost"`,
		`column "remote" references "remote.v1/Remote" in database "remote", which is not the same physical database`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("strict error lacks %q:\n%v", want, err)
		}
	}
	for _, unwanted := range []string{`column "resource"`, `column "site_id"`, `column "hold"`} {
		if strings.Contains(err.Error(), unwanted) {
			t.Errorf("strict error reports resolved %s:\n%v", unwanted, err)
		}
	}
}
