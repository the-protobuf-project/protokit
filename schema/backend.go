package schema

// backend.go defines the original seam between protokit's generic IR builder and
// a generator's own annotation package (orm.v1, web3.v1): a Backend read that
// package into protokit's neutral config during the build and folded in its own
// rendering afterward.
//
// Deprecated: implement [FacetReader] instead — optionally with [StructureReader]
// and [Enricher] — plus a [LayoutResolver], and drive the build with
// protokit.Build / protokit.RunPlugin. See facet.go.
//
// Backend conflated three separable concerns, and the middle one was the problem:
//
//   - reading a generator's annotations                  → FacetReader
//   - deciding *neutral* names (database, schema, table) → protokit.v1, which
//     protokit now reads itself, so two generators over one proto agree
//   - a naming policy resolved from plugin config        → LayoutResolver
//
// Backend keeps working: [AdaptBackend] presents one as a FacetReader plus a
// LayoutResolver, so an existing generator builds unchanged. The neutral config
// types below (Datasource, TableStructure, ColumnStructure, IDStrategy) are not
// deprecated — StructureReader still uses them.

import (
	"google.golang.org/protobuf/reflect/protoreflect"
)

// IDStrategy is protokit's neutral surrogate-primary-key strategy. A Backend maps
// its own annotation enum onto it; protokit applies the synthesis.
type IDStrategy int

const (
	// IDUnspecified leaves the choice to protokit's default (a ULID surrogate for
	// a relational datasource; the AIP identifier kept as the key otherwise).
	IDUnspecified IDStrategy = iota
	// IDULID synthesizes a ULID surrogate id as the primary key.
	IDULID
	// IDUUID synthesizes a UUID surrogate id as the primary key.
	IDUUID
)

// Datasource is the file-level grouping/identity a Backend reads from its own
// datasource option. Empty fields fall back to protokit's defaults (package-path
// grouping, resource-type-derived schema).
type Datasource struct {
	Database string
	Schema   string
	Provider string
	URL      string

	// SchemaStrip requests that protokit flatten a trailing API version out of the
	// schema name it derives for this file's tables (bookstore_v1 → bookstore). The
	// Backend sets it from its own config: false for an authoritative per-file
	// schema override, its layout config's strip_version otherwise. protokit owns
	// no config, so the decision arrives here already made.
	SchemaStrip bool
}

// TableStructure is the message-level generic structure a Backend reads from its
// own table option: the persisted entity's name, whether it is emitted, and the
// key/timestamp synthesis every backend consumes.
type TableStructure struct {
	Table      string     // table/entity name override ("" = derive from the resource)
	Skip       bool       // exclude this message from all output
	ID         IDStrategy // surrogate primary-key synthesis
	Timestamps bool       // append created_at / updated_at

	// Indexes are multi-column indexes declared on the message, beyond those
	// protokit infers from AIP. Appended to the table after its columns are
	// mapped and before foreign-key indexes are synthesized, so a declared index
	// covering an FK column suppresses the redundant single-column one.
	Indexes []*Index
}

// ColumnStructure is the field-level generic structure a Backend reads from its
// own column option: the column name, whether it is emitted, and the referential
// behavior of any foreign key the field forms. OnDelete/OnUpdate are the resolved
// SQL clause form ("CASCADE", "SET NULL", …) or "" for the backend default — the
// Backend converts its own enum, keeping protokit free of it.
type ColumnStructure struct {
	Column   string
	Skip     bool
	OnDelete string
	OnUpdate string
}

// Backend is what a generator supplies so protokit can build the IR from the
// generator's own annotation package (orm.v1, web3.v1) without importing it.
// The three Read* methods run during the build, mapping the generator's options
// onto protokit's neutral config; Enrich runs afterward on the assembled IR to
// fold in the generator's rendering (types, indexes, access, fingerprints). A
// method reads its option off the descriptor's Options(); it must return a safe
// zero value when the option is absent.
//
// Deprecated: implement [FacetReader] (optionally with [StructureReader] and
// [Enricher]) plus a [LayoutResolver], and build with protokit.Build. A Backend
// still works via [AdaptBackend]; this interface will be removed one major after
// its consumers have migrated.
type Backend interface {
	ReadDatasource(protoreflect.FileDescriptor) Datasource
	ReadTable(protoreflect.MessageDescriptor) TableStructure
	ReadColumn(protoreflect.FieldDescriptor) ColumnStructure
	Enrich(dbs []*Database) error

	// DedupeSchemaTable reports whether protokit should rename tables whose name
	// stutters with their schema (a build-wide naming policy the generator drives
	// from its own config; false when it has none).
	DedupeSchemaTable() bool
}

// AdaptBackend presents a deprecated [Backend] as the pair that replaced it: a
// [FacetReader] (which also implements [StructureReader] and [Enricher]) and a
// [LayoutResolver].
//
// The split is not quite even, because a Backend resolves grouping *internally* —
// its ReadDatasource has already merged the generator's annotation with its config
// file and decided version-stripping by the time protokit sees the result. So the
// returned LayoutResolver reports no datasource opinion: applying one on top of an
// already-resolved Datasource would resolve the same policy twice. It forwards
// DedupeSchemaTable, which a Backend keeps as a separate method.
//
// The reader contributes no facets — a Backend has no notion of them — so
// [Facet] finds nothing under its key. Its structure and enrichment behave exactly
// as before, which is what lets a generator upgrade protokit without touching its
// own code.
//
// A nil Backend yields (nil, nil): a build with no generator vocabulary at all,
// which is a valid pure-AIP + protokit.v1 build.
func AdaptBackend(b Backend) (FacetReader, LayoutResolver) {
	if b == nil {
		return nil, nil
	}
	a := backendAdapter{b}
	return a, a
}

// backendAdapter implements FacetReader, StructureReader, Enricher, and
// LayoutResolver over a deprecated Backend.
type backendAdapter struct{ b Backend }

// Key namespaces the adapter's (empty) facet table. It is deliberately generic:
// a Backend never declared a vocabulary name, and inventing one per generator
// would let a migrating plugin collide with the real reader it later registers.
func (backendAdapter) Key() string { return "backend" }

// A Backend contributes no facets — it predates them.
func (backendAdapter) ReadFile(protoreflect.FileDescriptor) (any, error)       { return nil, nil }
func (backendAdapter) ReadMessage(protoreflect.MessageDescriptor) (any, error) { return nil, nil }
func (backendAdapter) ReadField(protoreflect.FieldDescriptor) (any, error)     { return nil, nil }

func (a backendAdapter) ReadDatasource(d protoreflect.FileDescriptor) Datasource {
	return a.b.ReadDatasource(d)
}

func (a backendAdapter) ReadTable(d protoreflect.MessageDescriptor) TableStructure {
	return a.b.ReadTable(d)
}

func (a backendAdapter) ReadColumn(d protoreflect.FieldDescriptor) ColumnStructure {
	return a.b.ReadColumn(d)
}

// Enrich forwards to the Backend, which only ever saw the databases.
func (a backendAdapter) Enrich(ir *IR) error { return a.b.Enrich(ir.Databases) }

// ResolveDatasource reports no opinion: ReadDatasource above already returned a
// fully-resolved Datasource, config included.
func (backendAdapter) ResolveDatasource(string) (database, schema string, stripVersion, ok bool) {
	return "", "", false, false
}

func (a backendAdapter) DedupeSchemaTable() bool { return a.b.DedupeSchemaTable() }
