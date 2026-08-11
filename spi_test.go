package protokit_test

// spi_test.go pins the SPI frozen at v1.3.0.
//
// Every declaration below is a compile-time assertion: if a signature in the
// frozen surface changes, this file stops building, and it stops building in the
// module that owns the contract rather than three repositories downstream at
// upgrade time. That is the whole mechanism — there is almost nothing to run.
//
// The freeze is additive-only until v1.4.0. Adding a method to Plugin, a field to
// Options, or a new function beside these is fine and needs no change here.
// Changing an existing signature is not, and will show up as a build failure with
// this file named in it.
//
// It is an external test package (protokit_test) so it can import golden, which
// imports protokit — the same direction a real generator's test package does.

import (
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/golden"
	"github.com/the-protobuf-project/protokit/manifest"
	"github.com/the-protobuf-project/protokit/schema"
)

// The build entry points. Build is the facet path; BuildIR and Run are the
// deprecated Backend shims, frozen alongside it because web3 calls them.
//
// The staticcheck suppressions below are the point of the file, not a workaround:
// naming a deprecated symbol is how its signature gets pinned, and web3 stays on
// these for months. When the shim is finally removed, these lines fail to compile
// and the suppression goes with them.
var (
	_ func(*protogen.Plugin, protokit.Options, []schema.FacetReader, schema.LayoutResolver) (*schema.IR, error) = protokit.Build
	_ func(*protogen.Plugin, protokit.Options, protokit.Plugin) error                                           = protokit.RunPlugin

	//nolint:staticcheck // SA1019: pinning the deprecated Backend shim is this file's purpose.
	_ func(*protogen.Plugin, protokit.Options, schema.Backend) ([]*schema.Database, error) = protokit.BuildIR
	//nolint:staticcheck // SA1019: pinning the deprecated Backend shim is this file's purpose.
	_ func(*protogen.Plugin, protokit.Options, map[string]schema.Target, schema.Backend) error = protokit.Run
)

// The facet accessors, in both spellings. protokit.Facet and schema.Facet are the
// same function over the same aliased types; a generator may use either.
var (
	_ func(*schema.IR, string, schema.NodeID) (*testFacet, bool) = protokit.Facet[*testFacet]
	_ func(*schema.IR, string, schema.NodeID) (*testFacet, bool) = schema.Facet[*testFacet]
	_ func(*schema.IR) []string                                  = protokit.FacetKeys
	_ func(*schema.IR, string) []schema.NodeID                   = protokit.FacetNodes
)

// NodeID constructors — the only supported way to derive a facet key, and
// therefore part of the frozen surface.
var (
	_ func(protoreflect.FileDescriptor) schema.NodeID    = schema.NodeIDOfFile
	_ func(protoreflect.MessageDescriptor) schema.NodeID = schema.NodeIDOfMessage
	_ func(protoreflect.FieldDescriptor) schema.NodeID   = schema.NodeIDOfField
)

// The deprecated Backend adapter, which is what keeps the shim honest.
//
//nolint:staticcheck // SA1019: pinning the deprecated Backend shim is this file's purpose.
var _ func(schema.Backend) (schema.FacetReader, schema.LayoutResolver) = schema.AdaptBackend

// The golden harness a downstream module drives its own testdata with.
var (
	_ func(*testing.T, string, protokit.Plugin)                        = golden.Determinism
	_ func(*testing.T, string, protokit.Plugin, protokit.Plugin)       = golden.IRAgreement
	_ func(*testing.T, string, []string, func(string) protokit.Plugin) = golden.RunPluginCase

	//nolint:staticcheck // SA1019: pinning the deprecated Backend shim is this file's purpose.
	_ func(*testing.T, string, map[string]schema.Target, []string, func(string) schema.Backend) = golden.RunCase
)

// The manifest surface: strict parse plus standalone validation.
var (
	_ func([]byte) (*manifest.Manifest, error) = manifest.Parse
	_ func() error                             = (&manifest.Manifest{}).Validate
)

// testFacet stands in for a generator's own facet type in the generic
// instantiations above. Facet is parametric over any type, so the concrete choice
// is arbitrary; what is pinned is the shape of the call.
type testFacet struct{ SQLType string }

// The IR's two fields, asserted by construction rather than by name lookup, so a
// rename or a type change fails the build.
var _ = schema.IR{
	Databases: []*schema.Database(nil),
	Facets:    map[string]map[schema.NodeID]any(nil),
}

// The root package's aliases must stay aliases, not fresh definitions: a
// generator writes protokit.IR in one file and schema.IR in another and expects
// them to be the same type. Assigning across the two proves it.
var (
	_ protokit.IR              = schema.IR{}
	_ schema.IR                = protokit.IR{}
	_ protokit.NodeID          = schema.NodeID("")
	_ protokit.FacetReader     = schema.FacetReader(nil)
	_ protokit.StructureReader = schema.StructureReader(nil)
	_ protokit.Enricher        = schema.Enricher(nil)
	_ protokit.LayoutResolver  = schema.LayoutResolver(nil)
)

// The four interfaces a plugin implements, asserted through a type that
// implements all of them at once — which is also the realistic shape, since a
// reader that supplies structure implements FacetReader and StructureReader
// together.
var (
	_ schema.FacetReader         = fullReader{}
	_ schema.StructureReader     = fullReader{}
	_ schema.Enricher            = fullReader{}
	_ schema.LayoutResolver      = fullReader{}
	_ schema.DeprecatedStructure = fullReader{}
)

type fullReader struct{}

func (fullReader) Key() string                                             { return "spi.test" }
func (fullReader) ReadFile(protoreflect.FileDescriptor) (any, error)       { return nil, nil }
func (fullReader) ReadMessage(protoreflect.MessageDescriptor) (any, error) { return nil, nil }
func (fullReader) ReadField(protoreflect.FieldDescriptor) (any, error)     { return nil, nil }
func (fullReader) ReadDatasource(protoreflect.FileDescriptor) schema.Datasource {
	return schema.Datasource{}
}
func (fullReader) ReadTable(protoreflect.MessageDescriptor) schema.TableStructure {
	return schema.TableStructure{}
}
func (fullReader) ReadColumn(protoreflect.FieldDescriptor) schema.ColumnStructure {
	return schema.ColumnStructure{}
}
func (fullReader) Enrich(*schema.IR) error      { return nil }
func (fullReader) StructureDeprecation() string { return "" }
func (fullReader) ResolveDatasource(string) (database, schema string, stripVersion, ok bool) {
	return "", "", false, false
}
func (fullReader) DedupeSchemaTable() bool { return false }

// Target and IRTarget stay two interfaces, with IRTarget embedding Target: a
// target implementing only Generate must keep satisfying schema.Target so a
// generator can migrate one renderer at a time.
var (
	_ schema.Target   = plainTarget{}
	_ schema.Target   = irTarget{}
	_ schema.IRTarget = irTarget{}
)

type plainTarget struct{}

func (plainTarget) Name() string                                        { return "plain" }
func (plainTarget) Generate(*protogen.Plugin, []*schema.Database) error { return nil }

type irTarget struct{ plainTarget }

func (irTarget) GenerateIR(*protogen.Plugin, *schema.IR) error { return nil }

// The deprecated Backend, still implementable exactly as it was.
//
//nolint:staticcheck // SA1019: pinning the deprecated Backend shim is this file's purpose.
var _ schema.Backend = legacyBackend{}

type legacyBackend struct{}

func (legacyBackend) ReadDatasource(protoreflect.FileDescriptor) schema.Datasource {
	return schema.Datasource{}
}
func (legacyBackend) ReadTable(protoreflect.MessageDescriptor) schema.TableStructure {
	return schema.TableStructure{}
}
func (legacyBackend) ReadColumn(protoreflect.FieldDescriptor) schema.ColumnStructure {
	return schema.ColumnStructure{}
}
func (legacyBackend) Enrich([]*schema.Database) error { return nil }
func (legacyBackend) DedupeSchemaTable() bool         { return false }

// TestSPIPresent exists so the assertions above are reported as a passing test
// rather than only as a successful compile. Everything it asserts has already
// been proven by the time it runs.
func TestSPIPresent(t *testing.T) {
	if protokit.FacetKeys(nil) != nil {
		t.Error("FacetKeys(nil) should be nil")
	}
}
