// Package schema defines the intermediate representation (IR) that every
// code-generation backend consumes. The IR is built once from the proto
// descriptor set, then each target renders it independently.
//
// Inference sources (highest → lowest priority):
//  1. google.api.resource   → Database, Schema, Table names
//  2. google.api.field_behavior → NotNull, PrimaryKey on Column
//  3. google.api.resource_reference → ForeignKey
//  4. protokit.v1.datasource / .table / .column → generic structure AIP can't express
//  5. the plugin's LayoutResolver, then protokit's package-path defaults
//
// protokit reads 1-4 itself. Generator-specific rendering (orm.v1, web3.v1) is
// folded in afterward by a generator-supplied Enricher and carried per node as
// facets (see facet.go), keeping this IR and its builder generic.
package schema

import (
	"sort"

	"google.golang.org/protobuf/compiler/protogen"
)

// Target is implemented by every output backend (prisma, gorm, sql).
// It receives the built schema tree and writes output via protogen.Plugin.
//
// A target that needs more than the neutral tree — its generator's own per-node
// options, carried as facets — implements [IRTarget] as well and receives the
// whole [IR]. Target stays the minimum a renderer must satisfy so a generator can
// migrate one target at a time.
type Target interface {
	// Name returns the short identifier matched against the "target" plugin opt.
	// Example: "prisma", "gorm", "sql".
	Name() string

	// Generate writes one or more output files for every database in dbs.
	Generate(p *protogen.Plugin, dbs []*Database) error
}

// IRTarget is a Target that renders from the full IR rather than the databases
// alone, so it can read the facets its generator's readers attached to each node
// (a column's SQL type, a table's access model) with [Facet].
//
// protokit prefers GenerateIR when a target implements it and falls back to
// Generate otherwise. Implement both — the usual shape is a Generate that wraps
// its argument and delegates:
//
//	func (g *Generator) Generate(p *protogen.Plugin, dbs []*schema.Database) error {
//		return g.GenerateIR(p, &schema.IR{Databases: dbs})
//	}
//
// which keeps the target usable from the deprecated Backend path, where no facets
// exist to read.
type IRTarget interface {
	Target

	// GenerateIR writes one or more output files from the full IR.
	GenerateIR(p *protogen.Plugin, ir *IR) error
}

// Database is the top-level output unit. Proto files that declare the same
// datasource name merge into a single Database, so a multi-file proto package
// produces one schema tree.
type Database struct {
	Name     string    // e.g. "bookstore_db"
	URL      string    // connection URL; the backend reads it from its datasource option
	Provider string    // "postgres" (default), "mongodb", "evm", …; from the backend's datasource option
	Schemas  []*Schema // grouped by datasource.schema or resource.type prefix

	// PluginVersion and ProtocVersion fill the generated-file banner. Set by
	// BuildIR from opts.Version and the CodeGeneratorRequest; an empty value
	// renders as "(unknown)".
	PluginVersion string
	ProtocVersion string

	// Opts carries generator-specific database settings that protokit itself never
	// interprets: the backend's Enrich stamps them (e.g. the gorm target reads
	// "go_module"/"stores"/"otel" here), and the generator's own targets read them
	// back at render time. Keeping them in one opaque map is what lets the neutral
	// IR carry a backend's knobs without protokit growing a field per generator.
	Opts map[string]string
}

// Opt returns the Opts value for key, or "" when unset (nil-map safe).
func (d *Database) Opt(key string) string {
	if d.Opts == nil {
		return ""
	}
	return d.Opts[key]
}

// Schema groups tables that share a namespace. Maps to a PostgreSQL schema.
type Schema struct {
	Name   string   // e.g. "bookstore_v1"
	Tables []*Table // every resource-annotated message under this namespace
	Enums  []*Enum  // every proto enum referenced by a column in this namespace
}

// SourceProtos returns the distinct proto import paths contributing tables to
// the schema, in sorted order — used for the generated-file banner's source
// line when one output merges several protos.
func (s *Schema) SourceProtos() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range s.Tables {
		if t.SourceProto != "" && !seen[t.SourceProto] {
			seen[t.SourceProto] = true
			out = append(out, t.SourceProto)
		}
	}
	sort.Strings(out)
	return out
}

// Enum maps one proto enum referenced by a column to a database enum type.
type Enum struct {
	// Name is the PascalCase enum type name from the proto enum simple name.
	// Globally unique within a database: when two distinct proto enums share a
	// simple name, the later one is qualified with a schema-derived prefix
	// (Prisma enums occupy one global namespace regardless of @@schema).
	Name string

	// SQLName is the snake_case type name used in DDL ("Genre" → "genre").
	// Qualified alongside Name on a cross-schema collision (Prisma's global enum
	// namespace); LocalSQLName keeps the bare form for schema-namespaced targets.
	SQLName string

	// LocalName / LocalSQLName are the bare, schema-local enum names captured at
	// build time, before any global-namespace qualification. Schema-namespaced
	// targets (GORM, SQL) render these so the schema/package already
	// disambiguating the enum isn't restated in its name; Prisma uses the
	// possibly-qualified Name / SQLName.
	LocalName    string
	LocalSQLName string

	// ProtoName is the fully-qualified proto enum name ("bookstore.v1.Genre"),
	// used to deduplicate the same enum referenced from multiple schemas.
	ProtoName string

	// PgSchema is the postgres schema this enum is placed in (its home schema),
	// used for the per-model/per-enum @@schema directive.
	PgSchema string

	// Comment is the proto enum's leading comment, normalized for embedding.
	Comment string

	// SourceFile is the proto file base name the enum was declared in.
	SourceFile string

	// SourceProto is the full proto import path the enum was declared in.
	SourceProto string

	// SourceDir is the enum's proto directory with the version segment dropped,
	// mirroring Table.SourceDir for fragment file placement.
	SourceDir string

	Values []*EnumValue
}

// EnumValue is a single member of an Enum.
type EnumValue struct {
	// Name is the proto value name with the enum-name prefix stripped
	// ("GENRE_FICTION" → "FICTION"). Stays SCREAMING_SNAKE for Prisma/Go consts.
	Name string

	// MapName is the canonical SCREAMING_SNAKE storage form written to the
	// database ("FICTION"), identical across the Prisma, GORM, and SQL targets.
	// Equals Name except when Name was sanitized to a valid identifier (a
	// digit-leading value: Name "_9S", MapName "9S").
	MapName string

	// Comment is the proto value's leading comment, normalized for embedding.
	Comment string
}
