package protokit

// table.go assembles a *schema.Table from a proto message: its column list,
// synthesized id/timestamp columns, and the foreign keys inferred from
// resource_reference. Scalar/enum column mapping lives in column.go.

import (
	"google.golang.org/protobuf/compiler/protogen"

	"github.com/the-protobuf-project/protokit/schema"
	"github.com/the-protobuf-project/protokit/types"
)

// buildTable maps one resource-annotated message to a *schema.Table.
func (ctx *buildCtx) buildTable(db *schema.Database, s *schema.Schema, msg *protogen.Message, name, src, srcPath string) *schema.Table {
	t := &schema.Table{
		Name:         name,
		Comment:      cleanComment(msg.Comments.Leading),
		ModelName:    string(msg.Desc.Name()),
		LocalName:    string(msg.Desc.Name()),
		ProtoMessage: string(msg.Desc.FullName()),
		SourceFile:   src,
		SourceProto:  srcPath,
		Source:       msg.Desc, // provenance handle for a generator's enrichment pass
		Node:         schema.NodeIDOfMessage(msg.Desc),
	}

	ctx.populateColumns(db, s, t, msg)
	applyAIPSystemFields(t)
	if res := resourceOf(msg); res != nil {
		materializeParents(t, res)
	}

	ts := ctx.tableOf(msg.Desc)
	applyIDStrategy(t, idStrategyOf(ts.ID, types.Provider(db.Provider)))
	applyTimestamps(t, ts.Timestamps)
	appendDeclaredIndexes(t, ts.Indexes)
	return t
}

// appendDeclaredIndexes appends the message's declared indexes to t. Runs last in
// table assembly, so a declared index lands after any protokit synthesized while
// mapping columns (the soft-delete index on delete_time) and before the
// foreign-key indexes finalizeIndexes adds — which is what lets a declared index
// on an FK column suppress the redundant single-column one.
//
// The indexes are copied rather than shared: the resolved TableStructure is
// memoized per message, and nameIndexes later writes a table-qualified name into
// each index. One message materialized into two databases would otherwise have
// the second run's names overwrite the first's.
func appendDeclaredIndexes(t *schema.Table, declared []*schema.Index) {
	for _, idx := range declared {
		cols := make([]string, len(idx.Columns))
		copy(cols, idx.Columns)
		t.Indexes = append(t.Indexes, &schema.Index{
			Name:    idx.Name,
			Columns: cols,
			Unique:  idx.Unique,
		})
	}
}

// populateColumns maps msg's fields onto t. Scalar/enum fields become columns;
// string fields with google.api.resource_reference become FK columns; and
// user message-typed fields always become embed requests (normalized into
// related tables with a primary key + foreign key by normalizeEmbeds) instead
// of lossy JSONB blobs — unless the field is skipped or pins an explicit column
// type. Maps and google.* well-known types are not normalizable and keep their
// scalar/JSONB mapping. Shared by buildTable (resources) and materialize
// (embedded children).
func (ctx *buildCtx) populateColumns(db *schema.Database, s *schema.Schema, t *schema.Table, msg *protogen.Message) {
	for _, f := range msg.Fields {
		cs := ctx.columnOf(f.Desc)
		if target := normalizableMessage(f); target != "" {
			if cs.Skip {
				continue
			}
			ctx.embeds = append(ctx.embeds, &embedReq{
				db: db, schemaName: s.Name, parent: t, field: f,
				targetMsg: target, repeated: f.Desc.IsList(),
				optional: !isRequiredField(f),
				onDelete: cs.OnDelete,
				onUpdate: cs.OnUpdate,
			})
			continue
		}

		// A repeated resource_reference is a many-to-many relation: a scalar FK
		// can't hold many parents. Defer it to normalizeM2M, which synthesizes a
		// join table (or falls back to an array column). No scalar column here.
		if ref := resourceRef(f); ref != nil && f.Desc.IsList() && !cs.Skip {
			ctx.m2m = append(ctx.m2m, &m2mReq{
				db: db, schemaName: s.Name, parent: t, field: f,
				targetModel: modelNameFromType(ref.GetType()),
				onDelete:    cs.OnDelete,
				onUpdate:    cs.OnUpdate,
			})
			continue
		}

		col := ctx.buildColumn(s, f)
		if col == nil {
			continue
		}
		t.Columns = append(t.Columns, col)
		if col.PrimaryKey && t.PKColumn == "" {
			t.PKColumn = col.Name
		}
		// Singular resource_reference → belongs-to FK. The repeated case is
		// intercepted above and modeled as a join table by normalizeM2M.
		if ref := resourceRef(f); ref != nil {
			refSchema, refTable := schemaTable(ref.GetType(), "")
			refModel := modelNameFromType(ref.GetType())
			col.FKModel = refModel
			t.ForeignKeys = append(t.ForeignKeys, &schema.ForeignKey{
				Column:           col.Name,
				ReferencedSchema: refSchema,
				ReferencedTable:  refTable,
				ReferencedModel:  refModel,
				OnDelete:         cs.OnDelete,
				OnUpdate:         cs.OnUpdate,
				// ReferencedColumn filled by resolveRelations after all tables built.
			})
		}
	}
	ctx.addOneofDiscriminators(s, t, msg)
}
