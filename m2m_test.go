package protokit_test

// m2m_test.go builds testdata/m2m and checks that each repeated reference's
// join table records what it stores (schema.Table.Join), so a target can
// persist the field without re-deriving the join's naming.

import (
	"testing"

	"google.golang.org/protobuf/compiler/protogen"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/golden"
	"github.com/the-protobuf-project/protokit/schema"
)

func TestJoinTableMetadata(t *testing.T) {
	p, err := protogen.Options{}.New(golden.BuildRequest(t, "testdata/m2m"))
	if err != nil {
		t.Fatalf("protogen: %v", err)
	}
	ir, err := protokit.Build(p, protokit.Options{}, []schema.FacetReader{oneSchemaReader{}}, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	joins := map[string]*schema.Table{}
	for _, db := range ir.Databases {
		for _, s := range db.Schemas {
			for _, tb := range s.Tables {
				if tb.Join != nil {
					joins[tb.Join.Field] = tb
				} else if tb.Source == nil {
					t.Errorf("%s: synthesized table without join metadata", tb.Name)
				}
			}
		}
	}
	want := map[string]schema.Join{
		"books": {
			OwnerProto: "library.v1.Reader", Field: "books", FieldNode: "library.v1.Reader.books",
			OwnerColumn: "reader_id", TargetProto: "library.v1.Book", TargetType: "library.v1/Book",
			TargetColumn: "book_id", NameColumn: "book_name",
		},
		"shelves": {
			OwnerProto: "library.v1.Reader", Field: "shelves", FieldNode: "library.v1.Reader.shelves",
			OwnerColumn: "reader_id", TargetProto: "library.v1.Shelf", TargetType: "library.v1/Shelf",
			TargetColumn: "shelf_id",
		},
	}
	for field, w := range want {
		tb := joins[field]
		if tb == nil {
			t.Errorf("Reader.%s: no join table", field)
			continue
		}
		if *tb.Join != w {
			t.Errorf("Reader.%s: join %+v, want %+v", field, *tb.Join, w)
		}
		for _, col := range []string{w.OwnerColumn, w.TargetColumn, w.NameColumn} {
			if col == "" {
				continue
			}
			found := false
			for _, c := range tb.Columns {
				found = found || c.Name == col
			}
			if !found {
				t.Errorf("Reader.%s: join metadata names column %q, which %s lacks", field, col, tb.Name)
			}
		}
	}
}
