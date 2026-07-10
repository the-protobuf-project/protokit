// Package factory is protokit's source-agnostic co-generation core: a Source builds
// a model, a Target renders it in a chosen language, and a Registry wires the two
// together so one binary can drive many sources and targets from one config.
//
// It is generic over the model type M, so each plugin keeps its own richly-typed
// model (e.g. one carrying proto-schema and/or GraphQL facets) while reusing this
// orchestration unchanged. protokit's proto IR engine (Run/BuildIR/schema.Target)
// is the proto source+target underneath; this layer is what lets a plugin add more
// sources (GraphQL, …) and languages around it.
package factory

import (
	"sort"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
)

// Ctx threads generation context through the pipeline. Plugin is set in plugin
// (protoc) mode — where buf/protoc hand the plugin a CodeGeneratorRequest — and nil
// otherwise; targets that need it check and error when it is absent.
type Ctx struct {
	Plugin *protogen.Plugin
}

// Source builds a model of type M from some input (proto descriptors, a GraphQL
// endpoint/schema, …).
type Source[M any] interface {
	Name() string
	Build(Ctx) (M, error)
}

// Target renders a model of type M into output files for one language.
type Target[M any] interface {
	Name() string
	// Languages lists the languages this target can emit; the caller validates a
	// requested language against it.
	Languages() []string
	Generate(ctx Ctx, m M, lang string) error
}

// Registry holds the sources and targets a binary ships, each keyed by Name.
type Registry[M any] struct {
	Sources map[string]Source[M]
	Targets map[string]Target[M]
}

// NewRegistry returns an empty registry for model type M.
func NewRegistry[M any]() *Registry[M] {
	return &Registry[M]{Sources: map[string]Source[M]{}, Targets: map[string]Target[M]{}}
}

// AddSource registers s under s.Name().
func (r *Registry[M]) AddSource(s Source[M]) { r.Sources[s.Name()] = s }

// AddTarget registers t under t.Name().
func (r *Registry[M]) AddTarget(t Target[M]) { r.Targets[t.Name()] = t }

// TargetNames returns the registered target names, sorted and comma-joined, for
// error messages.
func (r *Registry[M]) TargetNames() string {
	names := make([]string, 0, len(r.Targets))
	for k := range r.Targets {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
