package httprule

import (
	"fmt"
	"strings"
)

// SyntaxError reports a malformed template, with the offset of the offending
// character. Its message renders the template and a caret, because a template
// arrives in a build log with no other context and "unexpected '{'" alone does
// not locate anything.
type SyntaxError struct {
	Template string
	Pos      int
	Msg      string
}

func (e *SyntaxError) Error() string {
	pos := e.Pos
	if pos > len(e.Template) {
		pos = len(e.Template)
	}
	return fmt.Sprintf("invalid path template: %s\n\t%s\n\t%s^", e.Msg, e.Template, strings.Repeat(" ", pos))
}

// Parse parses a google.api.http path template.
//
// It enforces the grammar and the three structural rules the grammar itself
// does not express: "**" may appear only as the last segment, a variable may
// not contain another variable, and a field path may not be captured twice.
func Parse(s string) (*Template, error) {
	p := &parser{src: s}

	if !p.accept('/') {
		return nil, p.errf(p.pos, "template must begin with %q", "/")
	}
	segs, err := p.parseSegments(false)
	if err != nil {
		return nil, err
	}

	t := &Template{Raw: s, Segments: segs}
	if p.accept(':') {
		verb, err := p.parseLiteral()
		if err != nil {
			return nil, err
		}
		t.Verb = verb
	}
	if !p.eof() {
		return nil, p.errf(p.pos, "unexpected %q", p.src[p.pos])
	}
	if err := validate(t); err != nil {
		return nil, err
	}
	return t, nil
}
