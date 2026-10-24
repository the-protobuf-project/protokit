package protokit

// resolve.go is the second IR pass: after every file has been folded into the
// database, FK references are resolved to real target columns and HasMany
// back-references are populated so generators have both sides of every relation.
// A reference whose target is generated into another database of the same build
// is handed to resolveExternal (external_fk.go).

import (
	"github.com/the-protobuf-project/protokit/naming"
	"github.com/the-protobuf-project/protokit/schema"
)

// resolveRelations resolves every database's foreign keys. The tables of every
// database are indexed first, so a reference that crosses databases sees the
// final names of its target (de-stuttered and qualified before this runs). disc
// is searched only for a target no database of the run has.
func resolveRelations(dbs []*schema.Database, disc *discovery, diags *diagnostics) {
	idx := make(map[*schema.Database]*tableIndex, len(dbs))
	for _, db := range dbs {
		idx[db] = indexTables(db)
	}
	for _, db := range dbs {
		resolveDatabase(db, dbs, idx, disc, diags)
	}
}

// resolveDatabase corrects each ForeignKey against the live referenced table:
// the real schema (datasource.schema overrides shift tables away from the
// resource-type-derived schema), the real table name, and the real target
// column. It also appends a HasManyRef to the referenced table so both relation
// sides exist.
//
// Only a reference the model-name rule resolves becomes a relation. One that
// resolves in db only by resource type or parent pattern, one whose target is in
// another database of the build, and one whose target is discovered outside the
// run become constraint-only FKs (constrain); one with no usable target degrades
// to a soft FK (its scalar column kept and indexed, no relation), and the reason
// is recorded on diags (a hard error under --strict). Emitting a relation to a
// non-existent model produces a schema that fails Prisma validation, so no
// dangling FK is ever emitted. A FK whose scalar column went missing is likewise
// dropped defensively.
func resolveDatabase(db *schema.Database, dbs []*schema.Database, idx map[*schema.Database]*tableIndex, disc *discovery, diags *diagnostics) {
	// link pairs each resolved FK with the HasManyRef it produced, plus the model
	// pair they connect, so relation names can be assigned once all FKs are known.
	type link struct {
		fk         *schema.ForeignKey
		hm         *schema.HasManyRef
		childModel string
	}
	var links []link

	for _, s := range db.Schemas {
		for _, t := range s.Tables {
			resolved := t.ForeignKeys[:0]
			for _, fk := range t.ForeignKeys {
				if !hasColumn(t, fk.Column) {
					diags.warnf("ref", "table %q FK names missing column %q; relation dropped",
						t.Name, fk.Column)
					continue
				}
				ref, byName, ok := idx[db].lookup(fk)
				if !ok {
					resolveExternal(db, dbs, idx, disc, t, fk, diags)
					continue
				}
				if !byName {
					// The model-name rule never resolved this reference, so the column
					// has always been stored as written; a relation would change that.
					constrain(db, t, fk, ref, schema.ReferenceLocal, diags)
					continue
				}
				col, ok := referencedColumn(fk, ref.table, false)
				if !ok {
					unresolved(t, fk, schema.UnresolvedNoNameColumn, diags,
						"table %q column %q stores %q resource names, but %s.%s has no unique "+
							"name column to reference; kept as a soft FK (scalar column + index, no relation)",
						t.Name, fk.Column, ref.table.ModelName, ref.schema.Name, ref.table.Name)
					continue
				}
				// Reflect any model-name qualification onto the FK and its column.
				fk.ReferencedModel = ref.table.ModelName
				setColumnFKModel(t, fk.Column, ref.table.ModelName)
				fk.ReferencedSchema = ref.schema.Name
				fk.ReferencedTable = ref.table.Name
				fk.ReferencedColumn = col
				fk.Kind = schema.ReferenceLocal
				alignFKType(t, fk, ref.table)
				hm := &schema.HasManyRef{Model: t.ModelName, Field: t.Name, ViaFK: fk.Column}
				ref.table.HasMany = append(ref.table.HasMany, hm)
				resolved = append(resolved, fk)
				links = append(links, link{fk: fk, hm: hm, childModel: t.ModelName})
			}
			t.ForeignKeys = resolved
		}
	}

	// Name relations only where a model pair is connected by more than one
	// relation — Prisma rejects ambiguous unnamed relations, but a single
	// relation per pair needs no name. The name is derived from the child model
	// and the FK column (minus its _id suffix) so it is stable and unique, and is
	// mirrored onto both relation sides.
	pairCount := map[string]int{}
	for _, l := range links {
		pairCount[modelPairKey(l.childModel, l.fk.ReferencedModel)]++
	}
	for _, l := range links {
		if pairCount[modelPairKey(l.childModel, l.fk.ReferencedModel)] < 2 {
			continue
		}
		name := l.childModel + naming.PascalGo(naming.StripIDSuffix(l.fk.Column))
		l.fk.RelationName = name
		l.hm.RelationName = name
	}
}

// modelPairKey is an order-independent key for the two models a relation connects.
func modelPairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "\x00" + b
}

// hasColumn reports whether t declares a column named name.
func hasColumn(t *schema.Table, name string) bool {
	for _, c := range t.Columns {
		if c.Name == name {
			return true
		}
	}
	return false
}

// setColumnFKModel updates the FK column's association model name so targets
// (GORM struct types) track any model-name qualification applied after build.
func setColumnFKModel(t *schema.Table, col, model string) {
	for _, c := range t.Columns {
		if c.Name == col {
			c.FKModel = model
			return
		}
	}
}

// alignFKType makes the referencing column's neutral type match the referenced PK
// column (e.g. TypeULID when the parent uses a ULID id strategy), so each
// generator projects the FK and the PK to the same storage type. Enum columns are
// left alone; a generator that pins its own column type overrides this at render
// time (protokit does not track that override).
func alignFKType(t *schema.Table, fk *schema.ForeignKey, ref *schema.Table) {
	var child, pk *schema.Column
	for _, c := range t.Columns {
		if c.Name == fk.Column {
			child = c
		}
	}
	for _, c := range ref.Columns {
		if c.Name == fk.ReferencedColumn {
			pk = c
		}
	}
	if child == nil || pk == nil || child.Enum != nil {
		return
	}
	child.Type = pk.Type
}
