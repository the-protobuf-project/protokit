package httprule

// parser.go is the recursive-descent scanner behind Parse: cursor management
// and one function per production of the grammar.

import (
	"fmt"
	"strings"
)

type parser struct {
	src string
	pos int
}

func (p *parser) eof() bool { return p.pos >= len(p.src) }

func (p *parser) peek() byte {
	if p.eof() {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) accept(b byte) bool {
	if p.peek() != b {
		return false
	}
	p.pos++
	return true
}

func (p *parser) errf(pos int, format string, args ...any) error {
	return &SyntaxError{Template: p.src, Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

// parseSegments reads a "/"-joined segment sequence. nested is true inside a
// variable, where a further variable is not allowed.
func (p *parser) parseSegments(nested bool) ([]Segment, error) {
	var out []Segment
	for {
		s, err := p.parseSegment(nested)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
		if !p.accept('/') {
			return out, nil
		}
	}
}

func (p *parser) parseSegment(nested bool) (Segment, error) {
	switch {
	case p.accept('{'):
		if nested {
			return Segment{}, p.errf(p.pos-1, "a variable may not contain another variable")
		}
		return p.parseVariable()

	case p.peek() == '*':
		p.pos++
		if p.accept('*') {
			return Segment{Kind: KindMulti}, nil
		}
		return Segment{Kind: KindSingle}, nil

	default:
		lit, err := p.parseLiteral()
		if err != nil {
			return Segment{}, err
		}
		return Segment{Kind: KindLiteral, Literal: lit}, nil
	}
}

// parseLiteral reads a fixed path component. The terminator set is what the
// grammar reserves; anything else — including percent escapes and sub-delims —
// is content.
func (p *parser) parseLiteral() (string, error) {
	start := p.pos
	for !p.eof() && !strings.ContainsRune("/{}*:=", rune(p.src[p.pos])) {
		p.pos++
	}
	if p.pos == start {
		if p.eof() {
			return "", p.errf(p.pos, "expected a path component, found end of template")
		}
		return "", p.errf(p.pos, "expected a path component, found %q", p.src[p.pos])
	}
	return p.src[start:p.pos], nil
}

func (p *parser) parseVariable() (Segment, error) {
	field, err := p.parseFieldPath()
	if err != nil {
		return Segment{}, err
	}

	seg := Segment{Kind: KindVariable, Field: field}
	if p.accept('=') {
		sub, err := p.parseSegments(true)
		if err != nil {
			return Segment{}, err
		}
		seg.Sub = sub
	} else {
		// A bare {name} is defined to mean {name=*}.
		seg.Sub = []Segment{{Kind: KindSingle}}
	}
	if !p.accept('}') {
		return Segment{}, p.errf(p.pos, "expected %q to close the variable", "}")
	}
	return seg, nil
}

func (p *parser) parseFieldPath() ([]string, error) {
	var out []string
	for {
		ident, err := p.parseIdent()
		if err != nil {
			return nil, err
		}
		out = append(out, ident)
		if !p.accept('.') {
			return out, nil
		}
	}
}

func (p *parser) parseIdent() (string, error) {
	start := p.pos
	for !p.eof() && isIdent(p.src[p.pos], p.pos == start) {
		p.pos++
	}
	if p.pos == start {
		if p.eof() {
			return "", p.errf(p.pos, "expected a field name, found end of template")
		}
		return "", p.errf(p.pos, "expected a field name, found %q", p.src[p.pos])
	}
	return p.src[start:p.pos], nil
}

func isIdent(b byte, first bool) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b == '_':
		return true
	case b >= '0' && b <= '9':
		return !first
	}
	return false
}
