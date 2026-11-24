package openapi

import (
	"sort"
	"strconv"
)

func sortStrings(s []string) { sort.Strings(s) }

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func str(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

func boolean(m map[string]any, k string) bool {
	b, _ := m[k].(bool)
	return b
}

func num(m map[string]any, k string) *float64 {
	switch v := m[k].(type) {
	case int64:
		f := float64(v)
		return &f
	case float64:
		return &v
	}
	return nil
}

func integer(m map[string]any, k string) *int64 {
	switch v := m[k].(type) {
	case int64:
		return &v
	case float64:
		i := int64(v)
		return &i
	}
	return nil
}

func strs(v any) []string {
	var out []string
	for _, e := range list(v) {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// extensions collects x-* keys.
func extensions(m map[string]any) map[string]any {
	var out map[string]any
	for k, v := range m {
		if len(k) > 2 && k[0] == 'x' && k[1] == '-' {
			if out == nil {
				out = map[string]any{}
			}
			out[k] = clean(v)
		}
	}
	return out
}

// clean strips the internal order key from decoded maps, recursively, so values
// handed to consumers (defaults, examples, extensions) are plain JSON shapes.
func clean(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			if k == orderKey {
				continue
			}
			out[k] = clean(vv)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = clean(vv)
		}
		return out
	}
	return v
}
