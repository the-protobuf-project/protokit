package golden_test

// backend_shim_test.go is the end-to-end golden case for the deprecated
// [schema.Backend] path.
//
// web3 builds through Backend and will keep doing so for months. Nothing else in
// this repository exercises that path — the facet SPI has its own tests, and the
// adapter that bridges the two is a handful of forwarding methods that look
// correct whether or not they are. So a change to the build could quietly break
// every Backend consumer while protokit's own suite stayed green, and the first
// report would arrive from web3 at upgrade time.
//
// This case closes that gap. It drives golden.RunCase — the Backend entry point —
// through a fake Backend that exercises everything a real one contributes:
//
//	ReadDatasource     database name, provider, and version-stripped schema
//	ReadTable          skip, table rename, ULID surrogate key, audit timestamps
//	ReadColumn         column rename, and a foreign key's ON DELETE action
//	Enrich             writes to the assembled IR, before index finalization
//	DedupeSchemaTable  the build-wide de-stuttering policy
//
// and diffs the resulting IR against a committed golden. If the adapter stops
// forwarding any one of them, the golden moves and this test says which.
//
// The fake Backend reads no annotation module of its own, which is deliberate:
// importing one here would violate the invariant TestNoPluginProtoImports
// enforces, and would test that module rather than the shim.

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/golden"
	"github.com/the-protobuf-project/protokit/schema"
)

// TestBackendShimGolden builds the case through the deprecated Backend path and
// compares the dumped IR against testdata/backend_shim/golden/.
//
// Run with -update to rewrite the golden after an intentional change. A diff here
// is a change in what a Backend consumer sees, so it deserves a look before it is
// accepted.
func TestBackendShimGolden(t *testing.T) {
	//nolint:staticcheck // SA1019: exercising the deprecated Backend path is the point.
	golden.RunCase(t,
		"testdata/backend_shim",
		map[string]schema.Target{irDump{}.Name(): irDump{}},
		[]string{"irdump"},
		func(string) schema.Backend { return bookstoreBackend{} },
	)
}

// TestBackendShimDeterminism additionally proves the Backend path is
// order-independent: the adapter presents one reader, and a map ranged into
// output anywhere along it would show up as a diff between two runs of the same
// input.
func TestBackendShimDeterminism(t *testing.T) {
	//nolint:staticcheck // SA1019: exercising the deprecated Backend path is the point.
	reader, layout := schema.AdaptBackend(bookstoreBackend{})
	golden.Determinism(t, "testdata/backend_shim", protokit.Plugin{
		Registry: map[string]schema.Target{irDump{}.Name(): irDump{}},
		Readers:  []protokit.FacetReader{reader},
		Layout:   layout,
	})
}

// bookstoreBackend is a fake generator, standing in for orm or web3. It keys off
// proto names rather than reading options, so the case needs no annotation module
// and the test stays about the shim.
type bookstoreBackend struct{}

// ReadDatasource names the database and asks for the trailing API version to be
// stripped out of the derived schema — "bookstore_v1" becomes "bookstore", which
// is what makes BookstoreBranch's table stutter and the de-stuttering pass fire.
func (bookstoreBackend) ReadDatasource(protoreflect.FileDescriptor) schema.Datasource {
	return schema.Datasource{
		Database:    "bookstore_db",
		Provider:    "postgres",
		SchemaStrip: true,
	}
}

// ReadTable supplies per-message structure: one message is dropped entirely, one
// is renamed, and one gets the surrogate-key and timestamp synthesis.
func (bookstoreBackend) ReadTable(md protoreflect.MessageDescriptor) schema.TableStructure {
	switch md.Name() {
	case "AuditEntry":
		return schema.TableStructure{Skip: true}
	case "Book":
		return schema.TableStructure{
			ID:         schema.IDULID,
			Timestamps: true,
			Indexes: []*schema.Index{
				{Name: "idx_book_title_genre", Columns: []string{"title", "genre"}},
			},
		}
	case "Author":
		return schema.TableStructure{Table: "writers", ID: schema.IDUUID}
	default:
		return schema.TableStructure{}
	}
}

// ReadColumn renames one column and attaches a referential action to the book →
// author foreign key. OnDelete is the resolved SQL clause: a real Backend
// converts its own enum here, which is what keeps protokit free of it.
func (bookstoreBackend) ReadColumn(fd protoreflect.FieldDescriptor) schema.ColumnStructure {
	switch fd.FullName() {
	case "bookstore.v1.Book.author":
		return schema.ColumnStructure{OnDelete: "CASCADE", OnUpdate: "RESTRICT"}
	case "bookstore.v1.Author.display_name":
		return schema.ColumnStructure{Column: "full_name"}
	default:
		return schema.ColumnStructure{}
	}
}

// Enrich folds the generator's own rendering into the assembled IR. It runs
// before index finalization, so the unique flag set here suppresses the redundant
// single-column index protokit would otherwise synthesize for that column — the
// ordering guarantee the Enricher contract exists to provide.
func (bookstoreBackend) Enrich(dbs []*schema.Database) error {
	for _, db := range dbs {
		db.Opts = map[string]string{
			"go_module": "example.com/test/gen",
			"stores":    "true",
		}
		for _, s := range db.Schemas {
			for _, tb := range s.Tables {
				for _, c := range tb.Columns {
					if c.Node == "bookstore.v1.Book.title" {
						c.Index = true
					}
					if c.Node == "bookstore.v1.Author.email" {
						c.Unique = true
					}
				}
			}
		}
	}
	return nil
}

// DedupeSchemaTable opts the build into renaming schema-stuttering tables.
func (bookstoreBackend) DedupeSchemaTable() bool { return true }

// irDump renders the parts of the IR a Backend influences as sorted, stable text.
// A generator's real target would emit Prisma or Go; what matters here is that
// every value the shim forwards appears in the output, so a regression in the
// adapter cannot hide behind a renderer that ignores it.
type irDump struct{}

func (irDump) Name() string { return "irdump" }

func (irDump) Generate(p *protogen.Plugin, dbs []*schema.Database) error {
	for _, db := range dbs {
		g := p.NewGeneratedFile(db.Name+".ir.txt", "")
		g.P(dumpDatabase(db))
	}
	return nil
}

func dumpDatabase(db *schema.Database) string {
	var b strings.Builder
	fmt.Fprintf(&b, "database %s\n", db.Name)
	fmt.Fprintf(&b, "  provider: %s\n", db.Provider)

	keys := make([]string, 0, len(db.Opts))
	for k := range db.Opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "  opt %s = %s\n", k, db.Opts[k])
	}

	for _, s := range db.Schemas {
		fmt.Fprintf(&b, "\n  schema %s\n", s.Name)
		for _, t := range s.Tables {
			dumpTable(&b, t)
		}
		for _, e := range s.Enums {
			vals := make([]string, len(e.Values))
			for i, v := range e.Values {
				vals[i] = v.Name
			}
			fmt.Fprintf(&b, "    enum %s [%s]\n", e.Name, strings.Join(vals, " "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func dumpTable(b *strings.Builder, t *schema.Table) {
	fmt.Fprintf(b, "    table %s (model %s, node %s)\n", t.Name, t.ModelName, t.Node)
	fmt.Fprintf(b, "      pk: %s\n", t.PKColumn)

	for _, c := range t.Columns {
		var flags []string
		if c.PrimaryKey {
			flags = append(flags, "pk")
		}
		if c.NotNull {
			flags = append(flags, "notnull")
		}
		if c.Optional {
			flags = append(flags, "optional")
		}
		if c.Unique {
			flags = append(flags, "unique")
		}
		if c.Index {
			flags = append(flags, "index")
		}
		if c.Generated != "" {
			flags = append(flags, "generated="+c.Generated)
		}
		if c.AutoCreate {
			flags = append(flags, "autocreate")
		}
		if c.AutoUpdate {
			flags = append(flags, "autoupdate")
		}
		if c.FKModel != "" {
			flags = append(flags, "fk->"+c.FKModel)
		}
		fmt.Fprintf(b, "      column %-12s type=%-10s %s\n", c.Name, typeName(c.Type), strings.Join(flags, " "))
	}

	for _, fk := range t.ForeignKeys {
		fmt.Fprintf(b, "      fk %s -> %s.%s.%s on_delete=%s on_update=%s\n",
			fk.Column, fk.ReferencedSchema, fk.ReferencedTable, fk.ReferencedColumn,
			blank(fk.OnDelete), blank(fk.OnUpdate))
	}
	for _, idx := range t.Indexes {
		unique := ""
		if idx.Unique {
			unique = " unique"
		}
		fmt.Fprintf(b, "      index %s [%s]%s\n", idx.Name, strings.Join(idx.Columns, " "), unique)
	}
	for _, hm := range t.HasMany {
		fmt.Fprintf(b, "      hasmany %s via %s\n", hm.Model, hm.ViaFK)
	}
}

// fieldTypeNames spells schema.FieldType for the golden. The type is an unnamed
// int with no String method, and a golden full of bare integers would be
// unreadable and would renumber silently if a constant were ever inserted
// mid-list. An unmapped value still renders distinctly, so nothing goes unnoticed.
var fieldTypeNames = map[schema.FieldType]string{
	schema.TypeUnknown:   "unknown",
	schema.TypeString:    "string",
	schema.TypeBool:      "bool",
	schema.TypeInt32:     "int32",
	schema.TypeUint32:    "uint32",
	schema.TypeInt64:     "int64",
	schema.TypeUint64:    "uint64",
	schema.TypeFloat:     "float",
	schema.TypeDouble:    "double",
	schema.TypeBytes:     "bytes",
	schema.TypeEnum:      "enum",
	schema.TypeTimestamp: "timestamp",
	schema.TypeDuration:  "duration",
	schema.TypeDate:      "date",
	schema.TypeTimeOfDay: "timeofday",
	schema.TypeDecimal:   "decimal",
	schema.TypeLatLng:    "latlng",
	schema.TypeInterval:  "interval",
	schema.TypeText:      "text",
	schema.TypeJSON:      "json",
	schema.TypeULID:      "ulid",
	schema.TypeUUID:      "uuid",
}

func typeName(t schema.FieldType) string {
	if n, ok := fieldTypeNames[t]; ok {
		return n
	}
	return fmt.Sprintf("type(%d)", int(t))
}

// blank renders an unset referential action as a visible token, so a golden line
// distinguishes "the Backend supplied nothing" from "the Backend supplied an
// empty string it should have filled in".
func blank(s string) string {
	if s == "" {
		return "(default)"
	}
	return s
}
