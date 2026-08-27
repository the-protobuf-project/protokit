package protokit

// boundary_test.go enforces protokit's central architectural invariant: the
// engine imports no annotation module at all.
//
// protokit is the neutral layer. It reads AIP (google.api.*), and everything past
// that arrives through the SPI — a FacetReader the plugin implements, over an
// annotation module the plugin owns. The moment protokit imports entity.v1,
// store.v1, or web3.v1 directly, that arrangement inverts: the engine gains a
// compile-time dependency on one of its consumers, the vocabularies stop being
// pluggable, and golden.IRAgreement's claim — that two plugins over one set of
// protos derive the same neutral names — stops being structural and becomes a
// thing someone has to remember.
//
// The rule has no exemptions, and that is a recent and deliberate state. protokit
// used to own one vocabulary of its own, protokit.v1, and this file allowlisted
// the import of it — the test that enforces "protokit imports no annotation
// module" carrying an exemption for the one it did import. That vocabulary was
// persistence-shaped (datasource, table, column, id strategy) in an engine that is
// not, so it moved to store as entity.v1 and the exemption went with it. What
// keeps two plugins agreeing on a name is now a shared *reader* they both import,
// not a proto protokit owns.
//
// The invariant is easy to state and easy to violate by accident: importing a
// generated stub package is one line, and it compiles. So it is checked here
// rather than documented. See docs/ownership.md for the rule this defends.

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/the-protobuf-project/protokit/buffers"
	"github.com/the-protobuf-project/protokit/schema"
)

// apiVersion matches a trailing path segment that is an *API* version rather than
// a Go module major version. The two are spelled identically and mean opposite
// things, so the distinction is load bearing:
//
//	github.com/acme/entity/gen/entity/v1   an annotation module — forbidden
//	github.com/vektah/gqlparser/v2         a Go major version — ordinary dependency
//
// Go's semantic import versioning never emits "/v1" (v0 and v1 carry no suffix)
// and never emits a channel suffix, so "v1", "v1beta1", and "v2beta1" are
// unambiguously API versions. A bare "/v2" or "/v3" is ambiguous and is not
// flagged on that basis alone — the goStubs pattern below still catches the shape
// a generated annotation package actually takes ("entitypbv2"), which is what a
// plugin's vocabulary compiles to in practice.
var apiVersion = regexp.MustCompile(`^(v1|v[0-9]+(alpha|beta)[0-9]*)$`)

// goStubs matches the flattened package name protoc-gen-go's module= option
// produces for a versioned proto package: entity/v1 becomes entitypbv1, and
// plain bindings become somethingpb.
//
// This is deliberately wider than the "*/pb/*" and "*/v1" of the rule as
// written, because those two patterns miss the case that actually matters. A
// plugin's stubs are generated the ordinary way — web3's annotation module
// compiles to web3pbv1, store's to storepbv1, entity's to entitypbv1 — and none
// has a "pb" path segment or a trailing "/v1". Checking only the literal patterns
// would leave the one import this test exists to prevent undetected.
var goStubs = regexp.MustCompile(`pb(v[0-9]+((alpha|beta)[0-9]*)?)?$`)

// allowedPrefixes are the two proto module trees protokit is permitted to
// import: the protobuf runtime, and the AIP annotations protokit reads as a
// primary inference source. Neither belongs to a plugin.
var allowedPrefixes = []string{
	"google.golang.org/protobuf",
	"google.golang.org/genproto/googleapis/api",
}

// There is deliberately no per-path exemption list beside the prefixes above.
//
// The one that used to exist covered protokit.v1, and an exemption is exactly how
// this gate fails quietly: a vocabulary protokit owns is indistinguishable, at the
// import site, from a vocabulary protokit merely vendored. Allowing anything under
// "github.com/the-protobuf-project/protokit" would be worse still — it would let a
// future change copy a plugin's vocabulary into this repository and pass, which is
// precisely the failure this test exists to catch with an extra directory in front
// of it.

// TestNoPluginProtoImports walks every non-test Go file in the module and fails
// on any import of a proto module that is not the protobuf runtime or the AIP
// annotations.
//
// Test files are exempt because a test may legitimately construct a descriptor
// from a vocabulary it is exercising. The invariant is about what protokit's
// compiled surface depends on, and a _test.go file is not part of it.
func TestNoPluginProtoImports(t *testing.T) {
	type violation struct {
		file   string
		imp    string
		lineNo int
	}
	var found []violation

	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// testdata holds case fixtures, not module code; the rest are not Go.
			if name := d.Name(); name == ".git" || name == "testdata" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			if !protoShaped(imp) || allowed(imp) {
				continue
			}
			found = append(found, violation{
				file:   filepath.ToSlash(path),
				imp:    imp,
				lineNo: fset.Position(spec.Pos()).Line,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	if len(found) == 0 {
		return
	}

	sort.Slice(found, func(i, j int) bool {
		if found[i].file != found[j].file {
			return found[i].file < found[j].file
		}
		return found[i].lineNo < found[j].lineNo
	})

	var b strings.Builder
	b.WriteString("protokit imports an annotation module, which breaks the neutral layer:\n\n")
	for _, v := range found {
		fmt.Fprintf(&b, "\t%s:%d\timports %s\n", v.file, v.lineNo, v.imp)
	}
	b.WriteString("\nprotokit reads AIP and nothing else. Every annotation vocabulary reaches\n" +
		"the build through the SPI, never through an import:\n\n" +
		"\timplement schema.FacetReader over your annotation module, in the repo\n" +
		"\tthat owns it, and pass it to protokit.Build via protokit.Plugin.Readers.\n\n" +
		"If the value is structural — something protokit acts on while building rather\n" +
		"than a value a target reads back — add schema.StructureReader and map it onto\n" +
		"the neutral types in schema/backend.go (Datasource, TableStructure,\n" +
		"ColumnStructure, IDStrategy). Those stay plain Go structs precisely so this\n" +
		"import is never necessary.\n\n" +
		"See docs/ownership.md.")
	t.Error(b.String())
}

// protoShaped reports whether an import path looks like a proto module: generated
// Go bindings, or a package addressed by its API version.
func protoShaped(path string) bool {
	segs := strings.Split(path, "/")
	last := segs[len(segs)-1]

	if apiVersion.MatchString(last) || goStubs.MatchString(last) {
		return true
	}
	// "*/pb/*" — bindings collected under a pb directory rather than suffixed.
	return slices.Contains(segs[:len(segs)-1], "pb")
}

// allowed reports whether a proto-shaped import is one of the two protokit may
// depend on.
func allowed(path string) bool {
	for _, p := range allowedPrefixes {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// TestBoundaryPatterns pins the classifier itself. The walk above is only as good
// as what protoShaped recognizes, and a test that silently stopped matching
// anything would pass forever while enforcing nothing.
func TestBoundaryPatterns(t *testing.T) {
	violations := []string{
		"github.com/the-protobuf-project/web3/protobuf/web3pbv1",
		"github.com/the-protobuf-project/store/plugin/pb/storepbv1",
		"github.com/the-protobuf-project/store/entity/pb/entitypbv1",
		"github.com/acme/entity/gen/entity/v1",
		"github.com/acme/entity/gen/pb/entity",
		"example.com/thing/gen/thingpb",
		"example.com/api/v2beta1",
		"example.com/thing/gen/entitypbv2",

		// protokit's own former vocabulary, pinned as a violation rather than
		// omitted. It sat in the allowlist until entity.v1 replaced it, and the
		// cheapest way to undo that removal by accident is to re-add the exemption
		// while re-adding the module. This line fails if anyone does.
		"github.com/the-protobuf-project/protokit/protobuf/protokitpbv1",
	}
	for _, imp := range violations {
		t.Run(imp, func(t *testing.T) {
			if !protoShaped(imp) {
				t.Errorf("protoShaped(%q) = false, want true", imp)
			}
			if allowed(imp) {
				t.Errorf("allowed(%q) = true, want false", imp)
			}
		})
	}

	permitted := []string{
		"google.golang.org/protobuf/compiler/protogen",
		"google.golang.org/protobuf/reflect/protoreflect",
		"google.golang.org/protobuf/types/pluginpb",
		"google.golang.org/protobuf/types/descriptorpb",
		"google.golang.org/genproto/googleapis/api/annotations",
	}
	for _, imp := range permitted {
		t.Run(imp, func(t *testing.T) {
			if protoShaped(imp) && !allowed(imp) {
				t.Errorf("allowed(%q) = false, want true", imp)
			}
		})
	}

	// Ordinary imports are not proto-shaped at all, so they never reach the
	// allowlist and cannot be whitelisted into it by accident. A Go module major
	// version is the case that matters here: "/v2" is how every Go dependency past
	// v1 is spelled, and flagging it would make the gate unusable.
	for _, imp := range []string{
		"strings",
		"github.com/the-protobuf-project/protokit/schema",
		"github.com/bufbuild/protocompile",
		"github.com/vektah/gqlparser/v2",
		"github.com/vektah/gqlparser/v2/ast",
	} {
		if protoShaped(imp) {
			t.Errorf("protoShaped(%q) = true, want false — that is a Go module major version, not an annotation module", imp)
		}
	}
}

// TestNeutralTypesStayPlain asserts that the four types a StructureReader maps
// onto — the neutral layer a plugin's vocabulary is translated *into* — are built
// from Go builtins and protokit's own types, with no proto message anywhere in
// their field graph.
//
// This is the same invariant as the import walk, checked from the other side. An
// import of entity.v1 would be caught above; a field typed *entitypbv1.IdStrategy
// reached through a type alias, or a proto message embedded via any, would not
// necessarily be. These four structs are the whole reason protokit needs no such
// import, so their shape is worth pinning directly.
func TestNeutralTypesStayPlain(t *testing.T) {
	for _, rt := range []reflect.Type{
		reflect.TypeFor[schema.Datasource](),
		reflect.TypeFor[schema.TableStructure](),
		reflect.TypeFor[schema.ColumnStructure](),

		// The buffers IR's seam, pinned for the same reason. Its reader hands the
		// walk a struct per node instead of the plugin's option message, and a
		// field typed *bufferspbv1.FieldOptions here would defeat that entirely
		// while still compiling.
		reflect.TypeFor[buffers.FileAnnotations](),
		reflect.TypeFor[buffers.MessageAnnotations](),
		reflect.TypeFor[buffers.FieldAnnotations](),
		reflect.TypeFor[buffers.EnumAnnotations](),
		reflect.TypeFor[buffers.EnumValueAnnotations](),
		reflect.TypeFor[buffers.OneofAnnotations](),
		reflect.TypeFor[buffers.ServiceAnnotations](),
		reflect.TypeFor[buffers.MethodAnnotations](),
		reflect.TypeFor[buffers.Vocabulary](),
	} {
		t.Run(rt.Name(), func(t *testing.T) {
			assertPlain(t, rt, rt.Name(), 0)
		})
	}

	// IDStrategy is protokit's own neutral enum, not a proto enum aliased into
	// place. A plugin maps its own enum onto these constants.
	if k := reflect.TypeOf(schema.IDULID).Kind(); k != reflect.Int {
		t.Errorf("IDStrategy kind = %v, want int — it must not alias a proto enum", k)
	}
}

// assertPlain walks a type's field graph and fails on anything that is not a Go
// builtin or a protokit type. depth guards against a cycle in the graph.
func assertPlain(t *testing.T, rt reflect.Type, path string, depth int) {
	t.Helper()
	if depth > 6 {
		return
	}

	switch rt.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		assertPlain(t, rt.Elem(), path+"[]", depth+1)
		return
	case reflect.Map:
		assertPlain(t, rt.Key(), path+"[key]", depth+1)
		assertPlain(t, rt.Elem(), path+"[val]", depth+1)
		return
	case reflect.Interface:
		t.Errorf("%s is an interface — the neutral types must not carry an open value "+
			"through which a proto message could reach protokit", path)
		return
	case reflect.Struct:
		if pkg := rt.PkgPath(); pkg != "" && !strings.HasPrefix(pkg, "github.com/the-protobuf-project/protokit") {
			t.Errorf("%s is %s.%s, from outside protokit — the neutral types must be "+
				"plain Go structs so no plugin vocabulary is needed to name them",
				path, pkg, rt.Name())
			return
		}
		for f := range rt.Fields() {
			assertPlain(t, f.Type, path+"."+f.Name, depth+1)
		}
	}
}
