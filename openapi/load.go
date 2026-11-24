package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	yaml "gopkg.in/yaml.v3"
)

// Fetcher reads the document at an absolute location (a file path or URL).
type Fetcher func(ctx context.Context, location string) ([]byte, error)

// DefaultFetcher reads local files and http(s) URLs.
func DefaultFetcher(ctx context.Context, location string) ([]byte, error) {
	b, _, err := fetchDefault(ctx, location)
	return b, err
}

// fetchDefault is DefaultFetcher that also reports where the document was
// finally served from: after an HTTP redirect, relative references resolve
// against the new URL, not the one requested.
func fetchDefault(ctx context.Context, location string) ([]byte, string, error) {
	if isURL(location) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
		if err != nil {
			return nil, "", err
		}
		c := &http.Client{Timeout: 60 * time.Second}
		resp, err := c.Do(req)
		if err != nil {
			return nil, "", err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode/100 != 2 {
			return nil, "", fmt.Errorf("GET %s: %s", location, resp.Status)
		}
		final := location
		if resp.Request != nil && resp.Request.URL != nil {
			final = resp.Request.URL.String()
		}
		b, err := io.ReadAll(resp.Body)
		return b, final, err
	}
	b, err := os.ReadFile(location)
	return b, location, err
}

// loader holds every document reached so far, keyed by absolute location.
type loader struct {
	ctx    context.Context
	fetch  func(context.Context, string) ([]byte, string, error)
	docs   map[string]any
	finals map[string]string // requested location -> where it was served from, when they differ
}

func newLoader(ctx context.Context, f Fetcher) *loader {
	l := &loader{ctx: ctx, fetch: fetchDefault, docs: map[string]any{}, finals: map[string]string{}}
	if f != nil {
		l.fetch = func(ctx context.Context, loc string) ([]byte, string, error) {
			b, err := f(ctx, loc)
			return b, loc, err
		}
	}
	return l
}

// base is the location relative references inside the document at loc
// resolve against: where it was served from after any redirect.
func (l *loader) base(loc string) string {
	if f, ok := l.finals[loc]; ok {
		return f
	}
	return loc
}

// doc returns the parsed document at an absolute location, loading it once.
func (l *loader) doc(loc string) (any, error) {
	if d, ok := l.docs[loc]; ok {
		return d, nil
	}
	raw, final, err := l.fetch(l.ctx, loc)
	if err != nil {
		return nil, fmt.Errorf("openapi: read %s: %w", loc, err)
	}
	d, err := decode(raw)
	if err != nil {
		return nil, fmt.Errorf("openapi: parse %s: %w", loc, err)
	}
	l.docs[loc] = d
	if final != "" && final != loc {
		l.finals[loc] = final
		l.docs[final] = d
	}
	return d, nil
}

// decode parses YAML or JSON (JSON is YAML) into plain Go values with string map keys.
func decode(raw []byte) (any, error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	var n yaml.Node
	if err := yaml.Unmarshal(raw, &n); err != nil {
		// Some JSON (huge, or with escapes yaml dislikes) only parses as JSON.
		var v any
		if jerr := json.Unmarshal(raw, &v); jerr == nil {
			return v, nil
		}
		return nil, err
	}
	if len(n.Content) == 0 {
		return map[string]any{}, nil
	}
	cv := &yamlConverter{active: map[*yaml.Node]bool{}}
	return cv.value(n.Content[0])
}

// maxYAMLNodes bounds alias expansion: a few aliases nested in each other
// ("billion laughs") expand exponentially even without a cycle.
const maxYAMLNodes = 10_000_000

// yamlConverter turns yaml nodes into plain values, keeping every mapping key a
// string (yaml.v3 would produce int keys for "200:" response codes). Aliases
// are expanded; one that leads back into a node still being converted is a
// cycle and an error, while reusing an anchor in several places is fine.
type yamlConverter struct {
	active map[*yaml.Node]bool
	nodes  int
}

func (cv *yamlConverter) value(n *yaml.Node) (any, error) {
	if cv.active[n] {
		return nil, fmt.Errorf("line %d: YAML alias cycle", n.Line)
	}
	if cv.nodes++; cv.nodes > maxYAMLNodes {
		return nil, fmt.Errorf("YAML aliases expand to more than %d nodes", maxYAMLNodes)
	}
	cv.active[n] = true
	defer delete(cv.active, n)
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return cv.value(n.Content[0])
	case yaml.AliasNode:
		return cv.value(n.Alias)
	case yaml.MappingNode:
		return cv.mapping(n)
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := cv.value(c)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.ScalarNode:
		return scalarValue(n), nil
	}
	return nil, nil
}

// mapping converts a mapping. Explicit keys win over merged ("<<") ones
// wherever they appear, and among merge sources (<<: *a, or <<: [*a, *b])
// the earlier one wins, as the YAML merge-key spec says. Merged keys keep
// each source's own key order.
func (cv *yamlConverter) mapping(n *yaml.Node) (any, error) {
	m := make(map[string]any, len(n.Content)/2)
	var keys []string
	var merges []*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Tag == "!!merge" {
			merges = append(merges, v)
			continue
		}
		if k.Value == orderKey {
			return nil, fmt.Errorf("line %d: the mapping key %q is reserved", k.Line, orderKey)
		}
		val, err := cv.value(v)
		if err != nil {
			return nil, err
		}
		if _, exists := m[k.Value]; !exists {
			keys = append(keys, k.Value)
		}
		m[k.Value] = val
	}
	var sources []*yaml.Node
	for _, v := range merges {
		src := v
		for src.Kind == yaml.AliasNode && src.Alias != nil {
			src = src.Alias
		}
		if src.Kind == yaml.SequenceNode {
			sources = append(sources, src.Content...)
		} else {
			sources = append(sources, v)
		}
	}
	for _, s := range sources {
		sv, err := cv.value(s)
		if err != nil {
			return nil, err
		}
		sm, ok := sv.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("line %d: a merge key (<<) needs a mapping", s.Line)
		}
		for _, kk := range orderedKeys(sm) {
			if _, exists := m[kk]; !exists {
				m[kk] = sm[kk]
				keys = append(keys, kk)
			}
		}
	}
	m[orderKey] = keyOrder(keys)
	return m, nil
}

func orderedKeys(m map[string]any) []string {
	var out []string
	for _, k := range keys(m) {
		if k != orderKey {
			out = append(out, k)
		}
	}
	return out
}

func scalarValue(n *yaml.Node) any {
	switch n.Tag {
	case "!!null":
		return nil
	case "!!bool":
		b, _ := strconv.ParseBool(n.Value)
		return b
	case "!!int":
		if i, err := strconv.ParseInt(n.Value, 0, 64); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(n.Value, 64); err == nil {
			return f
		}
		return n.Value
	case "!!float":
		if f, err := strconv.ParseFloat(n.Value, 64); err == nil {
			return f
		}
		return n.Value
	}
	return n.Value
}

// orderKey stores a mapping's source key order inside the map itself, so
// properties and paths keep the order the author wrote them in. The value has
// its own type, keyOrder, so it is never mistaken for document data; a YAML
// document that uses the key itself is rejected by the decoder, and clean()
// strips the metadata before any value leaves the package.
const orderKey = "\x00order"

type keyOrder []string

// keys returns a mapping's keys in source order (sorted for JSON-decoded maps).
func keys(m map[string]any) []string {
	if ks, ok := m[orderKey].(keyOrder); ok {
		return ks
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// ref is a resolved reference: the document location and the JSON pointer in it.
type ref struct {
	Loc     string
	Pointer string
}

func (r ref) String() string { return r.Loc + "#" + r.Pointer }

// resolveRef turns a $ref string relative to base into an absolute ref.
func resolveRef(base, s string) (ref, error) {
	file, frag, _ := strings.Cut(s, "#")
	// The fragment is a URI fragment: percent-decode it once, then it is a
	// plain JSON pointer (RFC 6901 §6), so "#/components%2Fschemas/Book"
	// and "#/components/schemas/Book" name the same schema and the same type.
	if dec, err := url.PathUnescape(frag); err == nil {
		frag = dec
	} else {
		return ref{}, fmt.Errorf("openapi: $ref %q: %w", s, err)
	}
	loc := base
	if file != "" {
		var err error
		loc, err = joinLocation(base, file)
		if err != nil {
			return ref{}, err
		}
	}
	return ref{Loc: loc, Pointer: frag}, nil
}

func joinLocation(base, rel string) (string, error) {
	if isURL(rel) {
		return rel, nil
	}
	// Inside a document served over HTTP every reference is a URI reference,
	// "/schemas/book.yaml" included: it names a path on the same origin.
	if isURL(base) {
		b, err := url.Parse(base)
		if err != nil {
			return "", err
		}
		r, err := url.Parse(rel)
		if err != nil {
			return "", err
		}
		return b.ResolveReference(r).String(), nil
	}
	if filepath.IsAbs(rel) {
		return rel, nil
	}
	return filepath.Join(filepath.Dir(base), filepath.FromSlash(rel)), nil
}

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// lookup follows a JSON pointer inside a document.
func (l *loader) lookup(r ref) (any, error) {
	d, err := l.doc(r.Loc)
	if err != nil {
		return nil, err
	}
	if r.Pointer == "" { // "/" is not the root: it selects the member named ""
		return d, nil
	}
	cur := d
	for _, tok := range strings.Split(strings.TrimPrefix(r.Pointer, "/"), "/") {
		tok = strings.ReplaceAll(strings.ReplaceAll(tok, "~1", "/"), "~0", "~")
		switch c := cur.(type) {
		case map[string]any:
			v, ok := c[tok]
			if !ok {
				return nil, fmt.Errorf("openapi: %s: no %q", r, tok)
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(tok)
			if err != nil || i < 0 || i >= len(c) {
				return nil, fmt.Errorf("openapi: %s: bad index %q", r, tok)
			}
			cur = c[i]
		default:
			return nil, fmt.Errorf("openapi: %s: cannot descend into %T", r, cur)
		}
	}
	return cur, nil
}

// deref follows a chain of $ref objects (not schemas, which keep identity) to
// the target object, returning it and the location it lives in.
func (l *loader) deref(loc string, v any) (map[string]any, string, error) {
	for depth := 0; depth < 64; depth++ {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, loc, nil
		}
		s, ok := m["$ref"].(string)
		if !ok {
			return m, loc, nil
		}
		r, err := resolveRef(loc, s)
		if err != nil {
			return nil, loc, err
		}
		t, err := l.lookup(r)
		if err != nil {
			return nil, loc, err
		}
		v, loc = t, l.base(r.Loc)
	}
	return nil, loc, fmt.Errorf("openapi: $ref chain too deep at %s", loc)
}
