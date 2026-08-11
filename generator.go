// package protokit is the generic proto→IR engine shared by the generator
// plugins. It parses plugin options, builds the schema IR (see build.go), and
// dispatches to a chosen schema.Target.
//
// protokit reads two vocabularies itself: the standard google.api.* (AIP)
// annotations, and protokit.v1 — the neutral structure that decides what things
// are named (see protobuf/protokit/v1/). Everything past that is supplied by the
// caller, so this package imports no generator: each plugin binary passes its own
// target registry, its own facet readers (which read that generator's annotation
// package), and its own layout resolver to Run. The same frontend thus drives the
// database (orm) and chain (web3) generators without either being visible here.
package protokit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/the-protobuf-project/protokit/schema"
	"google.golang.org/protobuf/compiler/protogen"
)

// Options holds the generic plugin parameters BuildIR needs. Anything backend-
// specific (Go module, stores, tracing, a layout-config path) is the generator's
// own concern: it configures its readers directly and passes generator-specific
// settings to its targets, so protokit owns no such options and no config file.
//
// Example buf.gen.yaml snippet:
//
//	plugins:
//	  - local: protoc-gen-orm
//	    out: generated/
//	    opt:
//	      - target=sql
type Options struct {
	// Target selects the output backend, matched against the target's Name().
	Target string

	// Strict is the per-rule severity spec for recoverable schema problems.
	// "" warns on everything (default); "true" makes every rule a hard error;
	// "ref:error,collision:warn,index:error,lint:warn" sets severity per rule.
	// Rules: ref, collision, index, lint.
	Strict string

	// Version is the plugin build version, written into the generated-file banner.
	// Empty renders as "(unknown)".
	Version string
}

// Plugin is everything a generator binary contributes to one run: the targets it
// ships, the readers that bring its annotation package into the IR, and the layout
// policy it resolved from its own config.
//
// Registration is explicit — protokit keeps no global registry and runs no init(),
// so what a run sees is exactly what its caller passed. Two plugins in one process
// cannot leak into each other, and a test can construct a Plugin with one reader
// and get a build with one reader.
type Plugin struct {
	// Registry holds the targets this binary ships, keyed by the buf.gen.yaml
	// opt: [target=<key>] value.
	Registry map[string]schema.Target

	// Readers bring the generator's own annotation packages into the IR as facets.
	// Order does not matter: Build sorts them by Key.
	Readers []FacetReader

	// Layout is the naming policy resolved from the plugin's config. Nil means the
	// plugin has none, and protokit's package-path defaults apply.
	Layout LayoutResolver
}

// Build builds the IR for every generate-flagged proto file: it infers databases
// from the generic AIP structure plus protokit.v1 and the layout policy, collects
// each reader's facets, runs any enrichment, finalizes indexes, lints, resolves
// diagnostics per opts.Strict, and stamps the plugin/protoc versions onto each
// database. It performs no rendering — pass the result to a schema.Target.
//
// readers are the generator's bridge to its own annotation packages; layout is the
// naming policy it resolved from its own config. protokit loads no config file of
// its own. Both may be empty/nil, which yields a pure AIP + protokit.v1 build.
//
// Enrichment runs on the core IR (tables, columns, relations, key/timestamp
// synthesis) before index finalization, so any index a reader declares is named
// and ordered alongside the synthesized foreign-key indexes exactly as a
// single-pass build would produce.
func Build(p *protogen.Plugin, opts Options, readers []FacetReader, layout LayoutResolver) (*IR, error) {
	sorted := sortReaders(readers)
	if err := checkDuplicateKeys(sorted); err != nil {
		return nil, err
	}

	diags := &diagnostics{}
	dbs, err := buildDatabases(p, diags, sorted, layout)
	if err != nil {
		return nil, fmt.Errorf("protokit: schema inference failed: %w", err)
	}

	ir := &IR{Databases: dbs}
	if err := collectFacets(p, ir, sorted); err != nil {
		return nil, err
	}
	for _, r := range sorted {
		e, ok := r.(Enricher)
		if !ok {
			continue
		}
		if err := e.Enrich(ir); err != nil {
			return nil, fmt.Errorf("protokit: enrichment failed for %q: %w", r.Key(), err)
		}
	}

	finalizeIndexes(ir.Databases, diags)
	lint(p, diags)
	if err := diags.resolve(opts.Strict); err != nil {
		return nil, err
	}

	protoc := protocVersion(p)
	for _, db := range ir.Databases {
		db.PluginVersion = opts.Version
		db.ProtocVersion = protoc
	}
	return ir, nil
}

// RunPlugin builds the IR and dispatches to the target named by opts.Target,
// selected from pl.Registry.
//
// Flow:
//  1. Validate opts.Target against the registry.
//  2. Build the IR (Build: generic build + the readers' structure and facets +
//     enrichment + index finalization).
//  3. Hand the IR to the resolved target for rendering.
//
// A target implementing schema.IRTarget receives the whole IR and can read any
// reader's facets; one implementing only schema.Target receives the databases
// alone. Both are supported so a generator can migrate a target at a time.
func RunPlugin(p *protogen.Plugin, opts Options, pl Plugin) error {
	target, err := resolveTarget(opts.Target, pl.Registry)
	if err != nil {
		return err
	}
	ir, err := Build(p, opts, pl.Readers, pl.Layout)
	if err != nil {
		return err
	}
	return generate(p, target, ir)
}

// generate dispatches one target, preferring the IR-aware form.
func generate(p *protogen.Plugin, target schema.Target, ir *IR) error {
	if t, ok := target.(schema.IRTarget); ok {
		return t.GenerateIR(p, ir)
	}
	return target.Generate(p, ir.Databases)
}

// resolveTarget looks name up in registry, with an error naming the valid keys.
func resolveTarget(name string, registry map[string]schema.Target) (schema.Target, error) {
	if name == "" {
		return nil, fmt.Errorf(
			"protokit: required option \"target\" is missing — "+
				"add opt: [target=%s] to your buf.gen.yaml plugin entry",
			targetNames(registry, "|"),
		)
	}
	target, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf(
			"protokit: unknown target %q — valid targets: %s",
			name, targetNames(registry, ", "),
		)
	}
	return target, nil
}

// sortReaders returns the non-nil readers ordered by Key. Every pass that
// consults readers walks them in this order, so a build's output cannot depend on
// the order a plugin happened to register them in.
//
// Nil entries are dropped rather than rejected: a plugin that assembles its
// reader list conditionally ("include the compat reader only when...") should not
// have to compact the slice itself.
func sortReaders(readers []FacetReader) []FacetReader {
	out := make([]FacetReader, 0, len(readers))
	for _, r := range readers {
		if r != nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

// checkDuplicateKeys rejects two readers claiming the same facet key: the second
// would silently overwrite the first's facets, and which one won would depend on
// sort order among equals. readers must already be sorted.
func checkDuplicateKeys(readers []FacetReader) error {
	for i := 1; i < len(readers); i++ {
		if k := readers[i].Key(); k == readers[i-1].Key() {
			return fmt.Errorf("protokit: two facet readers registered under the key %q; keys must be unique", k)
		}
	}
	return nil
}

// collectFacets walks every generate-flagged file, message, and field, asking each
// reader what it wants attached to that node. A reader returning (nil, nil)
// contributes nothing and costs no map entry, which is the common case: most nodes
// carry no annotation.
//
// Only generate-flagged files are walked. An imported file's messages can still be
// materialized into tables (see newBuildCtx), but its annotations belong to the
// module that owns it, and reading them here would let a dependency's options
// leak into a build that never asked for them.
func collectFacets(p *protogen.Plugin, ir *IR, readers []FacetReader) error {
	if len(readers) == 0 {
		return nil
	}
	for _, r := range readers {
		key := r.Key()
		for _, f := range p.Files {
			if !f.Generate {
				continue
			}
			if err := putFacet(ir, key, schema.NodeIDOfFile(f.Desc), func() (any, error) {
				return r.ReadFile(f.Desc)
			}); err != nil {
				return err
			}
			if err := collectMessageFacets(ir, key, r, f.Messages); err != nil {
				return err
			}
		}
	}
	return nil
}

// collectMessageFacets reads one reader's message and field facets, recursing into
// nested messages so a nested type is as addressable as a top-level one.
func collectMessageFacets(ir *IR, key string, r FacetReader, msgs []*protogen.Message) error {
	for _, m := range msgs {
		if m.Desc.IsMapEntry() {
			continue // a compiler-synthesized entry type, not part of the schema
		}
		if err := putFacet(ir, key, schema.NodeIDOfMessage(m.Desc), func() (any, error) {
			return r.ReadMessage(m.Desc)
		}); err != nil {
			return err
		}
		for _, f := range m.Fields {
			if err := putFacet(ir, key, schema.NodeIDOfField(f.Desc), func() (any, error) {
				return r.ReadField(f.Desc)
			}); err != nil {
				return err
			}
		}
		if err := collectMessageFacets(ir, key, r, m.Messages); err != nil {
			return err
		}
	}
	return nil
}

// putFacet stores one facet value under (key, id), skipping a nil value. The read
// is a closure so the error can be wrapped with the node and key that produced it
// — a facet read that fails deep in a descriptor tree is otherwise very hard to
// place.
func putFacet(ir *IR, key string, id NodeID, read func() (any, error)) error {
	v, err := read()
	if err != nil {
		return fmt.Errorf("protokit: facet %q on %s: %w", key, id, err)
	}
	if v == nil {
		return nil
	}
	if ir.Facets == nil {
		ir.Facets = map[string]map[NodeID]any{}
	}
	byNode, ok := ir.Facets[key]
	if !ok {
		byNode = map[NodeID]any{}
		ir.Facets[key] = byNode
	}
	byNode[id] = v
	return nil
}

// BuildIR builds the IR and returns only its databases.
//
// Deprecated: use [Build], which returns the full [IR] including the facets each
// reader contributed. This shim adapts backend to a facet reader plus a layout
// resolver ([schema.AdaptBackend]) and discards the facets; it will be removed one
// major after the [schema.Backend] SPI.
func BuildIR(p *protogen.Plugin, opts Options, backend schema.Backend) ([]*schema.Database, error) {
	reader, layout := schema.AdaptBackend(backend)
	ir, err := Build(p, opts, adaptedReaders(reader), layout)
	if err != nil {
		return nil, err
	}
	return ir.Databases, nil
}

// Run builds the IR and dispatches to the target named by opts.Target, selected
// from registry.
//
// Deprecated: use [RunPlugin] with a [Plugin], which carries facet readers and a
// layout resolver instead of a [schema.Backend]. This shim adapts backend and will
// be removed one major after the Backend SPI.
func Run(p *protogen.Plugin, opts Options, registry map[string]schema.Target, backend schema.Backend) error {
	reader, layout := schema.AdaptBackend(backend)
	return RunPlugin(p, opts, Plugin{Registry: registry, Readers: adaptedReaders(reader), Layout: layout})
}

// adaptedReaders wraps the adapter in a slice, or returns none when there was no
// backend to adapt. A nil backend is legitimate — it means "build from AIP and
// protokit.v1 alone" — and putting the resulting nil reader in the slice would
// turn that into a panic on the first Key() call.
func adaptedReaders(r FacetReader) []FacetReader {
	if r == nil {
		return nil
	}
	return []FacetReader{r}
}

// targetNames returns registry's keys, sorted and joined by sep, for use in
// error messages.
func targetNames(registry map[string]schema.Target, sep string) string {
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, sep)
}

// protocVersion formats the compiler version from the CodeGeneratorRequest the
// way protoc-gen-go does: "v<major>.<minor>.<patch>[-suffix]", or "(unknown)"
// when the invoker (e.g. the in-process test harness) supplies none.
func protocVersion(p *protogen.Plugin) string {
	v := p.Request.GetCompilerVersion()
	if v == nil {
		return ""
	}
	suffix := ""
	if s := v.GetSuffix(); s != "" {
		suffix = "-" + s
	}
	return fmt.Sprintf("v%d.%d.%d%s", v.GetMajor(), v.GetMinor(), v.GetPatch(), suffix)
}
