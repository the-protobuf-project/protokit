package httprule

import (
	"strings"
)

// Kind classifies one segment of a template.
type Kind uint8

const (
	// KindLiteral is a fixed path component, e.g. "v1" or "books".
	KindLiteral Kind = iota
	// KindSingle is "*": exactly one path component, whatever it contains.
	KindSingle
	// KindMulti is "**": zero or more path components. It may appear only as
	// the final segment, which is what makes capture spans addressable by index
	// from the start of the path.
	KindMulti
	// KindVariable is "{field=...}": a capture binding a request-message field
	// path to the sub-template it wraps. It exists only in the parsed form;
	// [Compile] flattens it away.
	KindVariable
)

func (k Kind) String() string {
	switch k {
	case KindLiteral:
		return "literal"
	case KindSingle:
		return "*"
	case KindMulti:
		return "**"
	case KindVariable:
		return "variable"
	}
	return "invalid"
}

// rank orders segment kinds from most to least specific. It is the basis of
// route precedence: a literal beats a "*", which beats a "**".
func (k Kind) rank() int {
	switch k {
	case KindLiteral:
		return 0
	case KindSingle:
		return 1
	case KindMulti:
		return 2
	}
	return 3
}

// Segment is one element of a parsed template.
//
// It is a tagged struct rather than an interface because every consumer past
// this package is a code generator that needs to switch on the kind and read
// the payload, and because the whole value has to survive being written to a
// golden file and compared.
type Segment struct {
	Kind Kind

	// Literal is set when Kind is KindLiteral.
	Literal string

	// Field is the dotted request-message field path, set when Kind is
	// KindVariable: {book.name=*} yields ["book", "name"].
	Field []string

	// Sub is the variable's inner template, set when Kind is KindVariable.
	// A bare {name} parses as a single KindSingle, so Sub is never empty.
	// Sub may not itself contain a KindVariable.
	Sub []Segment
}

// Template is a parsed google.api.http path template.
type Template struct {
	// Raw is the template exactly as it appeared in the annotation, kept so
	// diagnostics and OpenAPI output can quote the author's own text.
	Raw string

	// Segments is the top-level segment sequence, variables unexpanded.
	Segments []Segment

	// Verb is the AIP-136 custom-method suffix without its colon, or "".
	Verb string
}

// String renders the template back to its source form. Parse and String round
// trip: parsing String's output yields an equal Template.
func (t *Template) String() string {
	var b strings.Builder
	writeSegments(&b, t.Segments)
	if t.Verb != "" {
		b.WriteByte(':')
		b.WriteString(t.Verb)
	}
	return b.String()
}

func writeSegments(b *strings.Builder, segs []Segment) {
	for _, s := range segs {
		b.WriteByte('/')
		writeSegment(b, s)
	}
}

func writeSegment(b *strings.Builder, s Segment) {
	switch s.Kind {
	case KindLiteral:
		b.WriteString(s.Literal)
	case KindSingle:
		b.WriteByte('*')
	case KindMulti:
		b.WriteString("**")
	case KindVariable:
		b.WriteByte('{')
		b.WriteString(strings.Join(s.Field, "."))
		// A bare {name} is exactly {name=*}; render it in the short form so
		// String reproduces what an author would have written. Anything else
		// needs the explicit "=" and its sub-template.
		if len(s.Sub) != 1 || s.Sub[0].Kind != KindSingle {
			b.WriteByte('=')
			for i, sub := range s.Sub {
				if i > 0 {
					b.WriteByte('/')
				}
				writeSegment(b, sub)
			}
		}
		b.WriteByte('}')
	}
}

// Fields returns every variable field path in the template, in order of
// appearance, each joined with ".".
func (t *Template) Fields() []string {
	var out []string
	for _, s := range t.Segments {
		if s.Kind == KindVariable {
			out = append(out, strings.Join(s.Field, "."))
		}
	}
	return out
}
