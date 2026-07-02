package schema

// backend.go defines the seam between protokit's generic IR builder and a
// generator's own annotation package (orm.v1, web3.v1). protokit reads the
// standard google.api.* (AIP) structure itself, but it deliberately knows nothing
// of any generator's custom options — so a generator supplies a Backend that
// reads its own package into protokit's neutral config (during the build) and
// folds in its own rendering (afterward). This is what lets a user annotate with
// only orm.v1 (or only web3.v1) while protokit stays a generic, generator-neutral
// library that imports no backend proto.

import "google.golang.org/protobuf/reflect/protoreflect"

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
