package httprule

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ErrNoMatch is returned by [Route.Match] when the path does not match.
var ErrNoMatch = errors.New("httprule: path does not match route")

// DecodeError reports a captured segment that could not be decoded. Each is a
// 400 with INVALID_ARGUMENT and reason MALFORMED_PATH.
type DecodeError struct {
	// Field is the dotted field path the segment belongs to, so a caller can
	// raise a FieldViolation naming what the client actually sent.
	Field   string
	Segment string
	Reason  string
}

func (e *DecodeError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("httprule: path segment %q: %s", e.Segment, e.Reason)
	}
	return fmt.Sprintf("httprule: field %q: path segment %q: %s", e.Field, e.Segment, e.Reason)
}

// DecodeSegment percent-decodes one path segment, preserving %2F.
//
// That exception is the rule, not a detail: "/" separates the segments of an
// AIP-122 resource name, so decoding %2F would make a captured name ambiguous
// with a genuinely longer one — "shelves/a%2Fb" and "shelves/a/b" would arrive
// identical, and nothing downstream could tell a two-segment name holding a
// slash from a three-segment name. Every other escape decodes, including
// multi-byte UTF-8, which is encoded one byte at a time.
//
// See README §1.2 in the gateway repository.
func DecodeSegment(s string) (string, error) {
	if !strings.ContainsRune(s, '%') {
		return s, nil
	}

	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		if s[i] != '%' {
			out = append(out, s[i])
			i++
			continue
		}
		if i+2 >= len(s) {
			return "", &DecodeError{Segment: s, Reason: "truncated percent-escape"}
		}
		hi, ok1 := hexVal(s[i+1])
		lo, ok2 := hexVal(s[i+2])
		if !ok1 || !ok2 {
			return "", &DecodeError{Segment: s, Reason: "percent-escape is not two hex digits"}
		}
		if b := hi<<4 | lo; b == '/' {
			out = append(out, s[i:i+3]...) // left encoded, on purpose
		} else {
			out = append(out, b)
		}
		i += 3
	}

	if !utf8.Valid(out) {
		return "", &DecodeError{Segment: s, Reason: "decodes to invalid UTF-8"}
	}
	return string(out), nil
}

func hexVal(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	}
	return 0, false
}

// Match matches raw path segments against the route and returns the decoded
// captures. It is the reference implementation of README §1, and the
// behaviour every generated runtime is held to by the conformance suite.
//
// It returns [ErrNoMatch] when the path does not match, and a *[DecodeError]
// when it matches but a captured segment is malformed — a distinction that
// matters, because the first is a 404 and the second a 400.
func (r *Route) Match(segs []string, verb string) (map[string]string, error) {
	if !r.matches(segs, verb) {
		return nil, ErrNoMatch
	}

	out := make(map[string]string, len(r.Captures))
	for _, c := range r.Captures {
		end := c.End
		if end == ToEnd {
			end = len(segs)
		}
		parts := make([]string, 0, end-c.Start)
		for _, seg := range segs[c.Start:end] {
			decoded, err := DecodeSegment(seg)
			if err != nil {
				var de *DecodeError
				if errors.As(err, &de) {
					de.Field = c.Name()
				}
				return nil, err
			}
			parts = append(parts, decoded)
		}
		out[c.Name()] = strings.Join(parts, "/")
	}
	return out, nil
}
