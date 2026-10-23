package protokit

import (
	"testing"

	"github.com/the-protobuf-project/protokit/schema"
)

// An implicit index and a declared one on the same columns are one index; a
// unique one and a plain one on the same columns are not.
func TestDedupeIndexes(t *testing.T) {
	tbl := &schema.Table{Indexes: []*schema.Index{
		{Columns: []string{"update_time"}},
		{Name: "holds_by_update", Columns: []string{"update_time"}},
		{Columns: []string{"state", "update_time"}},
		{Columns: []string{"name"}, Unique: true},
		{Columns: []string{"name"}},
	}}
	db := &schema.Database{Schemas: []*schema.Schema{{Tables: []*schema.Table{tbl}}}}
	dedupeIndexes(db)
	if len(tbl.Indexes) != 4 {
		t.Fatalf("got %d indexes, want 4: %+v", len(tbl.Indexes), tbl.Indexes)
	}
	if tbl.Indexes[0].Name != "holds_by_update" {
		t.Errorf("kept %+v, want the named update_time index", tbl.Indexes[0])
	}
}
