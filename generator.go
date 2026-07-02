// package protokit is the generic proto→IR engine shared by the generator
// plugins. It parses plugin options, builds the schema IR (see build.go), and
// dispatches to a chosen schema.Target.
//
// Everything generator-specific is supplied by the caller, so this package
// imports no generator: each plugin binary passes its own target registry and its
// own schema.Backend (which reads that generator's annotation package and folds
// in its rendering) to Run. The same frontend thus drives the database (orm) and
// chain (web3) generators without either being visible here.
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
// own concern: it configures its Backend directly and passes generator-specific
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

// BuildIR builds the schema IR for every generate-flagged proto file: it infers
// databases from the generic AIP structure plus the backend's grouping, runs the
// backend's enrichment (folding in its own annotations, and stamping any
// generator-specific settings onto Database.Opts), finalizes indexes, lints,
// resolves diagnostics per opts.Strict, and stamps the plugin/protoc versions
// onto each database. It performs no rendering — pass the result to a
// schema.Target.
//
// backend is the generator's bridge to its own annotation package and config:
// protokit reads the generic google.api.* structure itself and calls backend for
// grouping and the rest, so protokit loads no config file of its own. The
// backend's Enrich runs on the core IR (tables, columns, relations, key/timestamp
// synthesis) before index finalization, so any index it declares is named and
// ordered alongside the synthesized foreign-key indexes exactly as a single-pass
// build would produce.
func BuildIR(p *protogen.Plugin, opts Options, backend schema.Backend) ([]*schema.Database, error) {
	diags := &diagnostics{}
	dbs, err := buildDatabases(p, diags, backend)
	if err != nil {
		return nil, fmt.Errorf("protokit: schema inference failed: %w", err)
	}
	if err := backend.Enrich(dbs); err != nil {
		return nil, fmt.Errorf("protokit: enrichment failed: %w", err)
	}
	finalizeIndexes(dbs, diags)
	lint(p, diags)
	if err := diags.resolve(opts.Strict); err != nil {
		return nil, err
	}

	protoc := protocVersion(p)
	for _, db := range dbs {
		db.PluginVersion = opts.Version
		db.ProtocVersion = protoc
	}
	return dbs, nil
}

// Run builds the IR and dispatches to the target named by opts.Target, selected
// from registry. Each plugin binary passes the registry of targets it ships
// (keyed by the buf.gen.yaml opt: [target=<key>] value) and the backend that
// reads its own annotation package into the IR.
//
// Flow:
//  1. Validate opts.Target against registry.
//  2. Build the IR (BuildIR: generic build + the backend's structure reading and
//     enrichment + index finalization).
//  3. Hand the IR to the resolved target for rendering.
func Run(p *protogen.Plugin, opts Options, registry map[string]schema.Target, backend schema.Backend) error {
	if opts.Target == "" {
		return fmt.Errorf(
			"protokit: required option \"target\" is missing — "+
				"add opt: [target=%s] to your buf.gen.yaml plugin entry",
			targetNames(registry, "|"),
		)
	}

	target, ok := registry[opts.Target]
	if !ok {
		return fmt.Errorf(
			"protokit: unknown target %q — valid targets: %s",
			opts.Target, targetNames(registry, ", "),
		)
	}

	dbs, err := BuildIR(p, opts, backend)
	if err != nil {
		return err
	}
	return target.Generate(p, dbs)
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
