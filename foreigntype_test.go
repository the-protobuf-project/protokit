package protokit_test

// foreigntype_test.go builds testdata/foreigntype, where a resource named
// Reservation references another service's Reservation, and checks that the
// reference does not resolve to the build's own table by its model name:
// single, it keeps no foreign key; repeated, it is an array column, not a join
// table. A reference to the build's own type still resolves.

import (
	"testing"

	"google.golang.org/protobuf/compiler/protogen"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/golden"
	"github.com/the-protobuf-project/protokit/schema"
)

func TestReferenceToAnotherServicesType(t *testing.T) {
	p, err := protogen.Options{}.New(golden.BuildRequest(t, "testdata/foreigntype"))
	if err != nil {
		t.Fatalf("protogen: %v", err)
	}
	ir, err := protokit.Build(p, protokit.Options{}, []schema.FacetReader{oneSchemaReader{}}, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var res *schema.Table
	tables := 0
	for _, db := range ir.Databases {
		for _, s := range db.Schemas {
			for _, tb := range s.Tables {
				tables++
				if tb.ProtoMessage == "stay.v1.Reservation" {
					res = tb
				}
			}
		}
	}
	if res == nil {
		t.Fatal("no Reservation table")
	}
	if tables != 1 {
		t.Errorf("%d tables, want 1: a repeated reference to another service's type is no join table", tables)
	}
	fks := map[string]string{}
	for _, fk := range res.ForeignKeys {
		fks[fk.Column] = fk.ReferencedTable
	}
	if tbl, ok := fks["engine_reservation"]; ok {
		t.Errorf("engine_reservation references %q; another service's type must not resolve to this build's table", tbl)
	}
	if _, ok := fks["replaces"]; !ok {
		t.Error("replaces: no foreign key; a reference to the build's own type still resolves")
	}
	var arr *schema.Column
	for _, c := range res.Columns {
		if c.Name == "engine_reservations" {
			arr = c
		}
	}
	if arr == nil || !arr.List {
		t.Errorf("engine_reservations: %+v, want an array column", arr)
	}
}
