package protokit

// external_fk.go resolves the foreign keys that get a constraint but no
// relation (Table.ExternalForeignKeys). A generator may route packages into
// separate databases (freebusy puts its own protos in "freebusy" and the RFC
// module's in "rfc") that still live in one physical database, as schemas of
// one Postgres. A reference from one to the other names a real row, so it gets
// a real constraint; it gets no relation, because each database renders on its
// own and cannot name another's models. The same holds for a target discovered
// outside the run (discover.go), and for a target in the referencing database
// that only the resource type finds: the model-name rule never resolved it, so
// its column has always stored the name as written, and a relation would change
// what is stored.
//
// The target must be identified without guessing: by AIP resource type for a
// resource_reference, by resource pattern for a parent from a nested pattern.
// Model names are not used across databases — two databases may each have a
// bare "Resource".

import (
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/the-protobuf-project/protokit/schema"
	"github.com/the-protobuf-project/protokit/types"
)

// located is one table and where it lives.
type located struct {
	db     *schema.Database
	schema *schema.Schema
	table  *schema.Table
}

// tableIndex finds one database's tables by each key a foreign key can name.
type tableIndex struct {
	byModel   map[string]located
	byProto   map[string]located
	byType    map[string][]located // google.api.resource type
	byPattern map[string][]located // every google.api.resource pattern
}

// indexTables indexes db's tables. Only resource tables carry a type and
// patterns; embedded value objects (which may embed a resource message) are
// left out of those two keys so they never shadow the resource's own table.
func indexTables(db *schema.Database) *tableIndex {
	idx := &tableIndex{
		byModel:   map[string]located{},
		byProto:   map[string]located{},
		byType:    map[string][]located{},
		byPattern: map[string][]located{},
	}
	for _, s := range db.Schemas {
		for _, t := range s.Tables {
			l := located{db: db, schema: s, table: t}
			idx.byModel[t.ModelName] = l
			idx.byProto[t.ProtoMessage] = l
			res := tableResource(t)
			if res == nil {
				continue
			}
			if typ := res.GetType(); typ != "" {
				idx.byType[typ] = append(idx.byType[typ], l)
			}
			for _, p := range res.GetPattern() {
				idx.byPattern[p] = append(idx.byPattern[p], l)
			}
		}
	}
	return idx
}

// lookup finds fk's target within one database. Embed relations pin the exact
// message; a resource_reference matches its resource type first, so a model
// name qualified on a collision still resolves; then the model name; then, for
// a parent FK, the parent's resource pattern. byName reports whether the
// model-name rule alone (exact message, else model name) finds a table — the
// rule that decided relations before type and pattern matching existed.
func (idx *tableIndex) lookup(fk *schema.ForeignKey) (l located, byName, ok bool) {
	byName = idx.byNameRule(fk)
	if fk.ReferencedProto != "" {
		if l, ok := idx.byProto[fk.ReferencedProto]; ok {
			return l, byName, true
		}
	}
	if fk.ReferencedType != "" {
		if ls := idx.byType[fk.ReferencedType]; len(ls) == 1 {
			return ls[0], byName, true
		}
	}
	if l, ok := idx.byModel[fk.ReferencedModel]; ok && !otherType(l.table, fk.ReferencedType) {
		return l, byName, true
	}
	if fk.ReferencedPattern != "" {
		if ls := idx.byPattern[fk.ReferencedPattern]; len(ls) == 1 {
			return ls[0], byName, true
		}
	}
	return located{}, false, false
}

// byNameRule reports whether the model-name rule resolves fk in idx.
func (idx *tableIndex) byNameRule(fk *schema.ForeignKey) bool {
	if _, ok := idx.byProto[fk.ReferencedProto]; ok && fk.ReferencedProto != "" {
		return true
	}
	l, ok := idx.byModel[fk.ReferencedModel]
	return ok && !otherType(l.table, fk.ReferencedType)
}

// otherType reports whether t is a resource of a type other than typ: a
// reference to "freebusy.reservation.v1/Reservation" must not resolve, by its
// model name, to a build's own "aperio.booking.v1/Reservation". A
// value-object table, or a reference that names no type, never conflicts.
func otherType(t *schema.Table, typ string) bool {
	if typ == "" {
		return false
	}
	res := tableResource(t)
	return res != nil && res.GetType() != "" && res.GetType() != typ
}

// external returns fk's matches in idx: by resource type for a
// resource_reference, by pattern for a parent. Embed FKs never cross databases.
func (idx *tableIndex) external(fk *schema.ForeignKey) []located {
	switch {
	case fk.ReferencedType != "":
		return idx.byType[fk.ReferencedType]
	case fk.ReferencedPattern != "":
		return idx.byPattern[fk.ReferencedPattern]
	}
	return nil
}

// resolveExternal handles a foreign key whose target is not in db. It looks in
// the build's other databases (ReferenceCrossDatabase), then — only when none
// matches — in the files the run does not generate (ReferenceDiscovered). A
// single match that shares db's physical database becomes a constraint-only FK
// (constrain). Otherwise — no target anywhere, more than one, a target in
// another physical database, or no column its values can match — it degrades to
// a soft FK and the reason is recorded under the "ref" rule. A dangling
// constraint is never emitted.
func resolveExternal(db *schema.Database, dbs []*schema.Database, idx map[*schema.Database]*tableIndex, disc *discovery, t *schema.Table, fk *schema.ForeignKey, diags *diagnostics) {
	kind := schema.ReferenceCrossDatabase
	var hits []located
	for _, other := range dbs {
		if other != db {
			hits = append(hits, idx[other].external(fk)...)
		}
	}
	if len(hits) == 0 {
		found, fidx := disc.databases()
		for _, d := range found {
			hits = append(hits, fidx[d].external(fk)...)
		}
		kind = schema.ReferenceDiscovered
	}
	target := fk.ReferencedModel
	if fk.ReferencedType != "" {
		target = fk.ReferencedType
	}
	switch len(hits) {
	case 0:
		// Most often the target is generated by another plugin run (buf invokes a
		// plugin once per input) from files this request neither imports nor has
		// a reference image for.
		unresolved(t, fk, schema.UnresolvedNotFound, diags,
			"table %q column %q references unknown model %q "+
				"(no table built in this run or discoverable from its imports and "+
				"reference images matches %q); kept as a soft FK (scalar column + index, no relation)",
			t.Name, fk.Column, fk.ReferencedModel, target)
		return
	case 1:
	default:
		var where []string
		for _, h := range hits {
			where = append(where, h.db.Name+"."+h.schema.Name+"."+h.table.Name)
		}
		unresolved(t, fk, schema.UnresolvedAmbiguous, diags,
			"table %q column %q references %q, which matches %d tables in "+
				"other databases (%s); kept as a soft FK (scalar column + index, no relation)",
			t.Name, fk.Column, target, len(hits), strings.Join(where, ", "))
		return
	}
	ref := hits[0]
	if !colocated(db, ref.db) {
		unresolved(t, fk, schema.UnresolvedOtherDatabase, diags,
			"table %q column %q references %q in database %q, which is not "+
				"the same physical database as %q (provider or URL differ, or the provider "+
				"has no foreign keys); kept as a soft FK (scalar column + index, no constraint)",
			t.Name, fk.Column, target, ref.db.Name, db.Name)
		return
	}
	constrain(db, t, fk, ref, kind, diags)
}

// constrain makes fk a constraint-only entry of t.ExternalForeignKeys, of the
// given kind, targeting ref. The column keeps no association (FKModel is
// cleared), so targets store it as written: a resource_reference holds the full
// resource name (AIP-122) and references the target's unique name column; a
// parent holds the bare parent id and references the target's primary key. A
// target with no name column for a resource_reference leaves a soft FK.
func constrain(db *schema.Database, t *schema.Table, fk *schema.ForeignKey, ref located, kind schema.ReferenceKind, diags *diagnostics) {
	col, ok := referencedColumn(fk, ref.table, true)
	if !ok {
		target := fk.ReferencedModel
		if fk.ReferencedType != "" {
			target = fk.ReferencedType
		}
		unresolved(t, fk, schema.UnresolvedNoNameColumn, diags,
			"table %q column %q stores %q resource names, but %s.%s has no "+
				"unique name column to reference; kept as a soft FK (scalar column + index, no constraint)",
			t.Name, fk.Column, target, ref.schema.Name, ref.table.Name)
		return
	}
	fk.Kind = kind
	fk.ReferencedDatabase = ref.db.Name
	fk.ReferencedSchema = ref.schema.Name
	fk.ReferencedTable = ref.table.Name
	fk.ReferencedModel = ref.table.ModelName
	fk.ReferencedColumn = col
	fk.OnDelete = externalOnDelete(fk)
	alignFKType(t, fk, ref.table)
	t.ExternalForeignKeys = append(t.ExternalForeignKeys, fk)
	externalFK(t, fk)
}

// unresolved degrades fk to a soft FK, records it on t with its reason, and
// reports why under the "ref" rule.
func unresolved(t *schema.Table, fk *schema.ForeignKey, why schema.UnresolvedReason, diags *diagnostics, format string, args ...any) {
	diags.warnf("ref", format, args...)
	softFK(t, fk.Column, fk.ReferencedModel)
	fk.Kind = schema.ReferenceUnresolved
	fk.Unresolved = why
	t.UnresolvedForeignKeys = append(t.UnresolvedForeignKeys, fk)
}

// externalOnDelete is the referential action of an external FK: NO ACTION
// unless a resource_reference sets one explicitly.
//
// The two databases have separate lifecycles, and their rows are soft-deleted
// (AIP-164: Delete sets delete_time; a purge removes the row later). Soft
// delete is an UPDATE, which fires no referential action, so a soft-deleted
// target stays referenceable and nothing here ever blocks it. The constraint
// acts only at the purge: NO ACTION refuses to remove a row that is still
// referenced, so a reference never dangles, and the purge removes (or
// archives) the referencing rows first — in the same statement, if it likes,
// which NO ACTION allows and RESTRICT would not. CASCADE would let one
// database's purge silently delete the other's rows (a catalogue purge erasing
// booking history), and SET NULL would break a NOT NULL column and lose the
// link. A parent FK's synthesized CASCADE is replaced for the same reason: an
// owner in another database does not own this database's rows' lifecycle.
func externalOnDelete(fk *schema.ForeignKey) string {
	if fk.ReferencedPattern == "" && fk.OnDelete != "" {
		return fk.OnDelete // explicit, from the generator's column option
	}
	return "NO ACTION"
}

// colocated reports whether a and b share one physical database, so a
// constraint can span them: the same relational provider and the same URL. An
// empty URL on both (each read from the environment at run time) counts as the
// same; a generator whose databases are separate servers gives them distinct
// URLs, or its targets ignore Table.ExternalForeignKeys.
func colocated(a, b *schema.Database) bool {
	if a.Provider != b.Provider || a.URL != b.URL {
		return false
	}
	switch types.Provider(a.Provider) {
	case types.MongoDB, types.EVM:
		return false
	}
	return true
}

// externalFK marks fk's column as the referencing side of a constraint-only
// FK: no association (FKModel cleared, so targets store the value as written),
// a comment naming the target, and a single-column index.
func externalFK(t *schema.Table, fk *schema.ForeignKey) {
	note := "Foreign key to " + fk.ReferencedSchema + "." + fk.ReferencedTable +
		"(" + fk.ReferencedColumn + ") in database " + fk.ReferencedDatabase +
		" (" + fk.Kind.String() + ", constraint only, no relation)."
	for _, c := range t.Columns {
		if c.Name != fk.Column {
			continue
		}
		c.FKModel = ""
		if c.Comment == "" {
			c.Comment = note
		} else {
			c.Comment += " " + note
		}
	}
	for _, idx := range t.Indexes {
		if len(idx.Columns) == 1 && idx.Columns[0] == fk.Column {
			return
		}
	}
	t.Indexes = append(t.Indexes, &schema.Index{Columns: []string{fk.Column}})
}

// referencedColumn picks the target column whose values fk's column stores, as
// the generated repositories write it. A parent or embed FK holds the target's
// id, and so does a relation (an association) to a top-level resource: those
// reference the primary key. A relation to a nested resource holds the whole
// name (a leaf id cannot rebuild it), and a constraint-only resource_reference
// holds whatever was written — the full name, since it has no association to
// map it — so those reference the target's unique name column; ok is false
// when it has none.
func referencedColumn(fk *schema.ForeignKey, ref *schema.Table, constraintOnly bool) (string, bool) {
	if fk.ReferencedType != "" && (constraintOnly || len(ref.Parents) > 0) {
		c := nameColumn(ref)
		return c, c != ""
	}
	return pkColumn(ref), true
}

// pkColumn is t's primary key column, "id" when none was resolved.
func pkColumn(t *schema.Table) string {
	if t.PKColumn != "" {
		return t.PKColumn
	}
	return "id"
}

// nameColumn is t's AIP resource-name column — the IDENTIFIER field, else a
// column called "name" — provided it is unique (or the primary key), which a
// foreign key needs of the column it references. Empty when there is none.
func nameColumn(t *schema.Table) string {
	keyed := func(c *schema.Column) bool { return c.Unique || c.PrimaryKey }
	for _, c := range t.Columns {
		if c.Source != nil && isIdentifier(c.Source) && keyed(c) {
			return c.Name
		}
	}
	for _, c := range t.Columns {
		if c.Name == "name" && keyed(c) {
			return c.Name
		}
	}
	return ""
}

// isIdentifier reports whether fd carries IDENTIFIER field_behavior.
func isIdentifier(fd protoreflect.FieldDescriptor) bool {
	if !proto.HasExtension(fd.Options(), annotations.E_FieldBehavior) {
		return false
	}
	for _, b := range proto.GetExtension(fd.Options(), annotations.E_FieldBehavior).([]annotations.FieldBehavior) {
		if b == annotations.FieldBehavior_IDENTIFIER {
			return true
		}
	}
	return false
}

// tableResource is the google.api.resource of the message t was built from;
// nil for value objects and synthesized tables.
func tableResource(t *schema.Table) *annotations.ResourceDescriptor {
	if t.Source == nil || t.ValueObject {
		return nil
	}
	if !proto.HasExtension(t.Source.Options(), annotations.E_Resource) {
		return nil
	}
	return proto.GetExtension(t.Source.Options(), annotations.E_Resource).(*annotations.ResourceDescriptor)
}
