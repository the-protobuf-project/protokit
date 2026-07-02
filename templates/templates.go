// Package templates holds the shared text/template rendering infrastructure.
// Each output target owns and embeds its own .tpl files and builds a set with
// MustParse at package init; templates contain presentation only — no naming or
// type logic. This package deliberately holds no target templates itself, so a
// target package can live in (and move to) its own module while reusing the
// rendering helpers here.
package templates

import (
	"io"
	"io/fs"
	"text/template"
)

// MustParse parses the templates matching patterns from fsys into a single set,
// naming each template by its file base name (e.g. "models.go.tpl"), which is
// how Render and {{template}} references address them. It panics on error, so
// call it once at package initialization.
func MustParse(fsys fs.FS, patterns ...string) *template.Template {
	return template.Must(template.ParseFS(fsys, patterns...))
}

// Render executes the template named name (its file base name) from set into w.
func Render(set *template.Template, w io.Writer, name string, data any) error {
	return set.ExecuteTemplate(w, name, data)
}
