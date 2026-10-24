package schema

// ReferenceKind is how a foreign key's target was found. A generator reads it to
// decide what to render: a relation for ReferenceLocal entries of
// Table.ForeignKeys, a constraint for Table.ExternalForeignKeys, nothing for
// ReferenceUnresolved.
type ReferenceKind int

const (
	// ReferenceLocal: the target is a table of the referencing table's own
	// Database, built in this run. The zero value, so a foreign key a generator
	// synthesizes itself (an embed's, a join table's) is local by default.
	ReferenceLocal ReferenceKind = iota

	// ReferenceCrossDatabase: the target is built in this run into another
	// Database that shares the physical database. Constraint only.
	ReferenceCrossDatabase

	// ReferenceDiscovered: the target is not generated in this run. It was
	// found in a file the run only looks up — one the request carries as an
	// import, or one from a reference image (Options.References) — and built
	// with the same naming and routing as when its own module is generated, so
	// the constraint names the real table. Constraint only.
	ReferenceDiscovered

	// ReferenceUnresolved: no usable target; the column is a soft FK (indexed,
	// no constraint). ForeignKey.Unresolved says why.
	ReferenceUnresolved
)

// String names the kind as it appears in diagnostics.
func (k ReferenceKind) String() string {
	switch k {
	case ReferenceLocal:
		return "local"
	case ReferenceCrossDatabase:
		return "cross-database"
	case ReferenceDiscovered:
		return "discovered"
	case ReferenceUnresolved:
		return "unresolved"
	}
	return "unknown"
}

// UnresolvedReason is why a reference stayed a soft FK.
type UnresolvedReason string

const (
	// UnresolvedNotFound: no table in the run, and none discoverable, has the
	// referenced resource type (or parent pattern, or model name).
	UnresolvedNotFound UnresolvedReason = "not-found"

	// UnresolvedAmbiguous: more than one table outside the referencing
	// Database matches, and nothing says which is meant.
	UnresolvedAmbiguous UnresolvedReason = "ambiguous"

	// UnresolvedOtherDatabase: the target lives in another physical database
	// (provider or URL differ, or the provider has no foreign keys), where no
	// constraint can reach.
	UnresolvedOtherDatabase UnresolvedReason = "other-physical-database"

	// UnresolvedNoNameColumn: the column stores full resource names but the
	// target has no unique name column for them to match.
	UnresolvedNoNameColumn UnresolvedReason = "no-name-column"
)
