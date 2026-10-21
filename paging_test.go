package protokit

import (
	"slices"
	"testing"

	"github.com/the-protobuf-project/protokit/schema"
)

func pagingTable(indexes ...*schema.Index) *schema.Table {
	return &schema.Table{
		Name:     "events",
		PKColumn: "id",
		Parents:  []string{"organization", "user"},
		Columns: []*schema.Column{
			{Name: "id", PrimaryKey: true},
			{Name: "organization_id"},
			{Name: "user_id"},
			{Name: "start_time"},
		},
		ForeignKeys: []*schema.ForeignKey{{Column: "user_id"}, {Column: "organization_id"}},
		Indexes:     indexes,
	}
}

func finalized(t *schema.Table) [][]string {
	db := &schema.Database{Schemas: []*schema.Schema{{Tables: []*schema.Table{t}}}}
	indexParentPaging(db)
	indexForeignKeys(db)
	nameIndexes(db)
	var out [][]string
	for _, idx := range t.Indexes {
		out = append(out, append([]string{idx.Name}, idx.Columns...))
	}
	return out
}

func TestIndexParentPaging(t *testing.T) {
	cases := []struct {
		name  string
		table *schema.Table
		want  [][]string
	}{
		{
			name:  "the immediate parent leads, the pk follows, and no lone parent index remains",
			table: pagingTable(),
			want: [][]string{
				{"idx_events_user_id_id", "user_id", "id"},
				{"idx_events_organization_id", "organization_id"},
			},
		},
		{
			name:  "a soft FK's synthesized index is replaced",
			table: pagingTable(&schema.Index{Columns: []string{"user_id"}}),
			want: [][]string{
				{"idx_events_user_id_id", "user_id", "id"},
				{"idx_events_organization_id", "organization_id"},
			},
		},
		{
			name:  "a declared index on the parent stays",
			table: pagingTable(&schema.Index{Name: "by_user", Columns: []string{"user_id"}}),
			want: [][]string{
				{"by_user", "user_id"},
				{"idx_events_user_id_id", "user_id", "id"},
				{"idx_events_organization_id", "organization_id"},
			},
		},
		{
			name:  "a declared (parent, pk, ...) index already serves paging",
			table: pagingTable(&schema.Index{Columns: []string{"user_id", "id", "start_time"}}),
			want: [][]string{
				{"idx_events_user_id_id_start_time", "user_id", "id", "start_time"},
				{"idx_events_organization_id", "organization_id"},
			},
		},
		{
			name: "a top-level resource gets nothing",
			table: &schema.Table{Name: "users", PKColumn: "id", Columns: []*schema.Column{{Name: "id", PrimaryKey: true}}},
			want:  nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := finalized(tc.table)
			if !slices.EqualFunc(got, tc.want, slices.Equal) {
				t.Fatalf("indexes = %v\nwant      %v", got, tc.want)
			}
		})
	}
}
