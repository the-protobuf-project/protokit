package protokit

// facet.go re-exports the facet vocabulary from package schema so a generator can
// spell these types either way — protokit.IR or schema.IR, protokit.NodeID or
// schema.NodeID.
//
// They are *defined* in schema because schema.IRTarget must name IR, and IR must
// name schema.Database; schema importing protokit would be a cycle. They are
// *aliased* here because a generator's plugin code already imports the root
// package for Options and Run, and should not need a second import to name the
// thing Build returns.
//
// These are type aliases, not definitions: protokit.IR and schema.IR are the same
// type, and a value of one satisfies any signature written in terms of the other.

import "github.com/the-protobuf-project/protokit/schema"

// IR is the complete output of a build: the neutral schema tree plus the facet
// side-tables each registered reader contributed. See [schema.IR].
type IR = schema.IR

// NodeID identifies one node of the proto graph by its fully-qualified name.
// See [schema.NodeID].
type NodeID = schema.NodeID

// FacetReader contributes a generator's own annotation package to the build.
// See [schema.FacetReader].
type FacetReader = schema.FacetReader

// StructureReader is the optional half of a FacetReader that supplies structure
// protokit acts on while building. See [schema.StructureReader].
type StructureReader = schema.StructureReader

// Enricher is the optional half of a FacetReader that folds a generator's
// rendering into the neutral IR. See [schema.Enricher].
type Enricher = schema.Enricher

// LayoutResolver supplies the naming policy a plugin resolves from its own
// configuration. See [schema.LayoutResolver].
type LayoutResolver = schema.LayoutResolver

// Facet returns the facet of type T that reader key attached to node id,
// reporting false when the node carries none. See [schema.Facet].
//
//	opts, ok := protokit.Facet[*storev1.ColumnFacet](ir, "store.v1", col.Node)
func Facet[T any](ir *IR, key string, id NodeID) (T, bool) {
	return schema.Facet[T](ir, key, id)
}

// FacetKeys returns the reader keys present in ir, sorted. See [schema.FacetKeys].
func FacetKeys(ir *IR) []string { return schema.FacetKeys(ir) }

// FacetNodes returns the nodes carrying a facet under key, sorted.
// See [schema.FacetNodes].
func FacetNodes(ir *IR, key string) []NodeID { return schema.FacetNodes(ir, key) }
