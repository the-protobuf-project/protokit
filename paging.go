package protokit

// paging.go indexes the order a nested resource's List pages in.

import (
	"slices"

	"github.com/the-protobuf-project/protokit/schema"
)

// indexParentPaging adds a (parent, primary key) index to every nested
// resource. An AIP List reads one parent's children a page at a time, and
// its default order is the primary key, so the page query is
//
//	WHERE <parent>_id = ? AND <pk> > ? ORDER BY <pk> LIMIT n
//
// A single-column index on the parent finds the rows but not in that order:
// PostgreSQL reads every one of the parent's children and sorts them for each
// page, so a page costs more as the parent grows. With the primary key behind
// the parent column, the index hands back the page already ordered and stops
// after n rows.
//
// The index leads with the parent column, so it also serves every lookup the
// single-column parent index did; that index is dropped rather than kept
// alongside it, and indexForeignKeys sees the column covered and adds none.
// Only the immediate parent is indexed: List scopes by it, never by a
// grandparent. A table that already declares an index starting with
// (parent, pk) is left as it is.
//
// Runs in finalizeIndexes before indexForeignKeys and nameIndexes, so the
// index is named like any other (idx_<table>_<parent>_<pk>).
func indexParentPaging(db *schema.Database) {
	for _, s := range db.Schemas {
		for _, t := range s.Tables {
			if len(t.Parents) == 0 || t.PKColumn == "" {
				continue
			}
			parent := t.Parents[len(t.Parents)-1] + "_id"
			if !hasColumn(t, parent) || parent == t.PKColumn {
				continue
			}
			paging := []string{parent, t.PKColumn}
			if slices.ContainsFunc(t.Indexes, func(idx *schema.Index) bool {
				return len(idx.Columns) >= 2 && slices.Equal(idx.Columns[:2], paging)
			}) {
				continue
			}
			// The single-column index a soft FK synthesized on the parent is
			// what the paging index replaces. A declared one -- named, or
			// unique -- is the schema author's and stays.
			t.Indexes = slices.DeleteFunc(t.Indexes, func(idx *schema.Index) bool {
				return idx.Name == "" && !idx.Unique && slices.Equal(idx.Columns, []string{parent})
			})
			t.Indexes = append(t.Indexes, &schema.Index{Columns: paging})
		}
	}
}
