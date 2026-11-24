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
	if isURL(location) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
		if err != nil {
			return nil, err
		}
		c := &http.Client{Timeout: 60 * time.Second}
		resp, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode/100 != 2 {
			return nil, fmt.Errorf("GET %s: %s", location, resp.Status)
		}
		return io.ReadAll(resp.Body)
	}
	return os.ReadFile(location)
}

// loader holds every document reached so far, keyed by absolute location.
type loader struct {
	ctx   context.Context
	fetch Fetcher
	docs  map[string]any
}

func newLoader(ctx context.Context, f Fetcher) *loader {
	if f == nil {
		f = DefaultFetcher
	}
	return &loader{ctx: ctx, fetch: f, docs: map[string]any{}}
}

// doc returns the parsed document at an absolute location, loading it once.
func (l *loader) doc(loc string) (any, error) {
	if d, ok := l.docs[loc]; ok {
		return d, nil
	}
	raw, err := l.fetch(l.ctx, loc)
	if err != nil {
		return nil, fmt.Errorf("openapi: read %s: %w", loc, err)
	}
	d, err := decode(raw)
	if err != nil {
		return nil, fmt.Errorf("openapi: parse %s: %w", loc, err)
	}
	l.docs[loc] = d
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
	return nodeValue(n.Content[0]), nil
}

// nodeValue converts a yaml node to plain values, keeping every mapping key a
// string (yaml.v3 would produce int keys for "200:" response codes).
func nodeValue(n *yaml.Node) any {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil
		}
		return nodeValue(n.Content[0])
	case yaml.AliasNode:
		return nodeValue(n.Alias)
	case yaml.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		keys := make([]string, 0, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			v := n.Content[i+1]
			if k.Tag == "!!merge" { // <<: *anchor
				if mm, ok := nodeValue(v).(map[string]any); ok {
					for kk, vv := range mm {
						if _, exists := m[kk]; !exists {
							m[kk] = vv
							keys = append(keys, kk)
						}
					}
				}
				continue
			}
			if _, exists := m[k.Value]; !exists {
				keys = append(keys, k.Value)
			}
			m[k.Value] = nodeValue(v)
		}
		m[orderKey] = keys
		return m
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			out = append(out, nodeValue(c))
		}
		return out
	case yaml.ScalarNode:
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
	return nil
}

// orderKey stores a mapping's source key order inside the map itself, so
// properties and paths keep the order the author wrote them in.
const orderKey = "\x00order"

// keys returns a mapping's keys in source order (sorted for JSON-decoded maps).
func keys(m map[string]any) []string {
	if ks, ok := m[orderKey].([]string); ok {
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
	if isURL(rel) || filepath.IsAbs(rel) {
		return rel, nil
	}
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
	if r.Pointer == "" || r.Pointer == "/" {
		return d, nil
	}
	cur := d
	for _, tok := range strings.Split(strings.TrimPrefix(r.Pointer, "/"), "/") {
		tok, _ = url.PathUnescape(tok)
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
		v, loc = t, r.Loc
	}
	return nil, loc, fmt.Errorf("openapi: $ref chain too deep at %s", loc)
}
