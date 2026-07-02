package protokit

// keys.go holds the column-synthesis passes that run after a table's proto
// fields are mapped: recognizing AIP system fields, choosing and applying the
// primary-key strategy, and appending audit timestamps. Kept separate from
// table.go so each file stays small and single-purpose.

import (
	"github.com/the-protobuf-project/protokit/schema"
	"github.com/the-protobuf-project/protokit/types"
)

// applyAIPSystemFields gives the AIP-148/164 standard fields their conventional
// database behavior with no annotation, matching the hand-written
// createdAt/updatedAt/deletedAt convention: create_time/update_time become
// auto-managed NOT NULL audit timestamps, delete_time becomes a nullable indexed
// soft-delete marker, and uid (the server-assigned id) is marked UNIQUE.
func applyAIPSystemFields(t *schema.Table) {
	for _, c := range t.Columns {
		switch c.Name {
		case "create_time":
			c.Type, c.Enum = schema.TypeTimestamp, nil
			c.NotNull, c.Optional, c.AutoCreate = true, false, true
			if c.Default == "" {
				c.Default = "now()"
			}
		case "update_time":
			c.Type, c.Enum = schema.TypeTimestamp, nil
			c.NotNull, c.Optional, c.AutoUpdate = true, false, true
		case "delete_time":
			c.Type, c.Enum = schema.TypeTimestamp, nil
			c.NotNull, c.Optional = false, true // null = live row
			t.Indexes = append(t.Indexes, &schema.Index{Columns: []string{"delete_time"}})
		case "uid":
			c.Unique = true
		}
	}
}

// idStrategyOf returns the table's id strategy. An explicit strategy from the
// backend's table option always wins. Otherwise the default depends on the
// datasource: a relational one synthesizes a ULID surrogate (the AIP resource
// name becomes a UNIQUE lookup key, a generated id is the storage PK — the
// production pattern), while an EVM datasource keeps the AIP IDENTIFIER as the
// key, so the contract and the evm driver address a record by its resource name
// with no surrogate to reconcile.
func idStrategyOf(id schema.IDStrategy, provider types.Provider) schema.IDStrategy {
	if id != schema.IDUnspecified {
		return id
	}
	if provider == types.EVM {
		return schema.IDUnspecified
	}
	return schema.IDULID
}

// applyIDStrategy synthesizes a generated `id` PK column and demotes any
// IDENTIFIER-derived primary key to a UNIQUE constraint. If the message already
// declares an `id` column, that one is promoted to the PK instead of
// synthesizing a duplicate (server-assigned ids carry no client-side default).
func applyIDStrategy(t *schema.Table, st schema.IDStrategy) {
	if st == schema.IDUnspecified {
		return
	}
	for _, c := range t.Columns {
		if c.Name == "id" {
			for _, o := range t.Columns {
				if o.PrimaryKey && o != c {
					o.PrimaryKey, o.Unique = false, true
				}
			}
			c.PrimaryKey, c.NotNull, c.Optional = true, true, false
			t.PKColumn = "id"
			return
		}
	}
	for _, c := range t.Columns {
		if c.PrimaryKey {
			c.PrimaryKey, c.Unique = false, true
		}
	}
	id := &schema.Column{
		Name:       "id",
		Comment:    "Unique identifier for the record.",
		PrimaryKey: true,
		NotNull:    true,
	}
	switch st {
	case schema.IDULID:
		id.Type, id.Generated = schema.TypeULID, "ulid"
	case schema.IDUUID:
		id.Type, id.Generated = schema.TypeUUID, "uuid"
	}
	t.Columns = append([]*schema.Column{id}, t.Columns...)
	t.PKColumn = "id"
}

// applyTimestamps appends created_at / updated_at TIMESTAMPTZ columns.
func applyTimestamps(t *schema.Table, on bool) {
	if !on {
		return
	}
	t.Columns = append(t.Columns,
		&schema.Column{
			Name: "created_at", Comment: "Timestamp when the record was created.",
			Type: schema.TypeTimestamp, NotNull: true, Default: "now()", AutoCreate: true,
		},
		&schema.Column{
			Name: "updated_at", Comment: "Timestamp when the record was last updated.",
			Type: schema.TypeTimestamp, NotNull: true, Default: "now()", AutoUpdate: true,
		},
	)
}
