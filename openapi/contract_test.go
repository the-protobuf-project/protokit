package openapi

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/the-protobuf-project/protokit/apispec"
)

// memFetcher serves documents from memory, keyed by base name, and records
// every location it was asked for.
type memFetcher struct {
	docs  map[string]string
	asked []string
}

func (m *memFetcher) fetch(_ context.Context, loc string) ([]byte, error) {
	m.asked = append(m.asked, loc)
	if d, ok := m.docs[filepath.Base(loc)]; ok {
		return []byte(d), nil
	}
	return nil, fmt.Errorf("no document %s", loc)
}

func parse(t *testing.T, doc string, extra map[string]string) (*apispec.Spec, error) {
	t.Helper()
	f := &memFetcher{docs: extra}
	return Parse(context.Background(), "api.yaml", []byte(doc), Options{Fetch: f.fetch})
}

func mustParse(t *testing.T, doc string) *apispec.Spec {
	t.Helper()
	s, err := parse(t, doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const head = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n"

func TestAllOfRequiredIndependentOfOrder(t *testing.T) {
	for name, members := range map[string]string{
		"required first": "- required: [id]\n        - properties: {id: {type: string}}",
		"required last":  "- properties: {id: {type: string}}\n        - required: [id]",
	} {
		t.Run(name, func(t *testing.T) {
			s := mustParse(t, head+`components:
  schemas:
    Book:
      allOf:
        `+members+`
        - properties: {title: {type: string}}
`)
			b := s.Types["Book"]
			if b == nil || b.Field("id") == nil || !b.Field("id").Required || b.Field("title").Required {
				t.Fatalf("Book: %+v", b)
			}
		})
	}
}

func TestSecurityInheritance(t *testing.T) {
	s := mustParse(t, `openapi: 3.0.3
info: {title: t, version: '1'}
security: [{key: []}]
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
paths:
  /a:
    get: {operationId: inherits, responses: {'200': {description: ok}}}
  /b:
    get: {operationId: open, security: [], responses: {'200': {description: ok}}}
`)
	if len(s.DefaultSecurity) != 1 {
		t.Fatalf("default security: %v", s.DefaultSecurity)
	}
	for _, op := range s.Operations {
		got := s.EffectiveSecurity(op)
		switch op.ID {
		case "inherits":
			if op.Security != nil || len(got) != 1 {
				t.Errorf("inherits: own %v effective %v", op.Security, got)
			}
		case "open":
			if op.Security == nil || len(got) != 0 {
				t.Errorf("open: own %v effective %v", op.Security, got)
			}
		}
	}
}

func TestUndeclaredSecurityScheme(t *testing.T) {
	_, err := parse(t, `openapi: 3.0.3
info: {title: t, version: '1'}
paths:
  /a:
    get: {security: [{missing: []}], responses: {'200': {description: ok}}}
`, nil)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseRelativeRefsReachFetcherAbsolute(t *testing.T) {
	f := &memFetcher{docs: map[string]string{"models.yaml": "Book: {type: object, properties: {id: {type: string}}}\n"}}
	s, err := Parse(context.Background(), "api.yaml", []byte(head+`components:
  schemas:
    Shelf:
      properties:
        book: {$ref: 'models.yaml#/Book'}
`), Options{Fetch: f.fetch})
	if err != nil {
		t.Fatal(err)
	}
	if s.Types["Book"] == nil {
		t.Fatal("Book not loaded")
	}
	for _, loc := range f.asked {
		if !filepath.IsAbs(loc) {
			t.Errorf("fetcher asked for relative location %q", loc)
		}
	}
}

func TestFailedRegistrationLeavesNoName(t *testing.T) {
	_, err := parse(t, head+`components:
  schemas:
    Pet:
      oneOf: [{$ref: '#/components/schemas/Cat'}, {$ref: '#/components/schemas/Dog'}]
      discriminator: {propertyName: kind, mapping: {cat: Cat, ghost: Ghost}}
    Cat: {properties: {kind: {type: string}}}
    Dog: {properties: {kind: {type: string}}}
`, nil)
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("a mapping to a missing schema must fail, got %v", err)
	}
}

func TestEncodedPointerIsOneType(t *testing.T) {
	s := mustParse(t, head+`components:
  schemas:
    Book: {properties: {id: {type: string}}}
    Shelf:
      properties:
        a: {$ref: '#/components/schemas/Book'}
        b: {$ref: '#/components%2Fschemas/%42ook'}
`)
	sh := s.Types["Shelf"]
	if sh.Field("a").Type.Named != "Book" || sh.Field("b").Type.Named != "Book" {
		t.Fatalf("a=%v b=%v", sh.Field("a").Type, sh.Field("b").Type)
	}
	if s.Types["Book2"] != nil {
		t.Fatal("an encoded reference created a second type")
	}
}

func TestYAMLMergeAndAliases(t *testing.T) {
	s := mustParse(t, head+`x-base: &base {type: string, description: base}
x-more: &more {description: more, format: uuid}
components:
  schemas:
    Ident:
      <<: [*base, *more]
      description: own
`)
	id := s.Types["Ident"]
	if id == nil || id.Description != "own" || id.Scalar == nil || id.Scalar.Format != "uuid" {
		t.Fatalf("Id: %+v", id)
	}
	if _, err := decode([]byte("a: &a\n  b: *a\n")); err == nil {
		t.Fatal("an alias cycle must be an error")
	}
}

func TestScalarAllOfAndTypeArrays(t *testing.T) {
	s := mustParse(t, head+`components:
  schemas:
    Obj:
      properties:
        code:
          allOf: [{type: string, minLength: 2}, {maxLength: 8}]
        either: {type: [integer, string]}
        num: {type: [integer, number, 'null']}
        gone: false
`)
	o := s.Types["Obj"]
	code := o.Field("code")
	if code.Type.Scalar != apispec.ScalarString || code.Constraints.MinLength == nil || code.Constraints.MaxLength == nil {
		t.Fatalf("code: %+v", code)
	}
	if o.Field("either").Type.Scalar != apispec.ScalarJSON {
		t.Errorf("either: %+v", o.Field("either").Type)
	}
	if n := o.Field("num"); n.Type.Scalar != apispec.ScalarDouble || !n.Nullable {
		t.Errorf("num: %+v", n)
	}
	if o.Field("gone") != nil {
		t.Error("a property with schema false must be dropped")
	}
}

func TestAllOfCycle(t *testing.T) {
	_, err := parse(t, head+`components:
  schemas:
    A: {allOf: [{$ref: '#/components/schemas/B'}, {properties: {a: {type: string}}}]}
    B: {allOf: [{$ref: '#/components/schemas/A'}, {properties: {b: {type: string}}}]}
`, nil)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("err = %v", err)
	}
}

func TestMediaWithSchemaPreferred(t *testing.T) {
	s := mustParse(t, `openapi: 3.0.3
info: {title: t, version: '1'}
paths:
  /b:
    get:
      operationId: getB
      responses:
        '200':
          description: ok
          content:
            application/json: {}
            application/vnd.book+json: {schema: {$ref: '#/components/schemas/Book'}}
components:
  schemas:
    Book: {properties: {id: {type: string}}}
`)
	if r := s.Operations[0].Result; r == nil || r.Named != "Book" {
		t.Fatalf("result = %+v", r)
	}
}

func TestServersDefault(t *testing.T) {
	s := mustParse(t, head)
	if len(s.Servers) != 1 || s.Servers[0].URL != "/" {
		t.Fatalf("servers = %+v", s.Servers)
	}
}

func TestSwaggerCollectionFormat(t *testing.T) {
	s := mustParse(t, `swagger: '2.0'
info: {title: t, version: '1'}
paths:
  /p:
    get:
      parameters:
        - {name: multi, in: query, type: array, items: {type: string}, collectionFormat: multi}
        - {name: csv, in: query, type: array, items: {type: string}}
      responses: {'200': {description: ok}}
`)
	for _, p := range s.Operations[0].Params {
		want := p.Name == "multi"
		if p.Style != "form" || p.Explode == nil || *p.Explode != want {
			t.Errorf("%s: style %q explode %v", p.Name, p.Style, p.Explode)
		}
	}
}

func TestFollowUps(t *testing.T) {
	s := mustParse(t, `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /n:
    get:
      operationId: getN
      responses:
        '204': {description: empty}
        '200':
          description: ok
          headers:
            X-Rate: {schema: {type: integer}}
          content: {application/json: {schema: {$ref: '#/components/schemas/N'}}}
components:
  schemas:
    N:
      properties:
        n: {type: number, minimum: 5, exclusiveMinimum: 3}
        m: {type: number, minimum: 3, exclusiveMinimum: 3}
        o: {enum: [{a: 1}, {b: 2}]}
`)
	op := s.Operations[0]
	if op.Result == nil || op.Result.Named != "N" {
		t.Fatalf("result = %+v", op.Result)
	}
	n := s.Types["N"]
	if k := n.Field("n").Constraints; *k.Minimum != 5 || k.ExclusiveMinimum {
		t.Errorf("n: min %v excl %v, want inclusive 5", *k.Minimum, k.ExclusiveMinimum)
	}
	if k := n.Field("m").Constraints; *k.Minimum != 3 || !k.ExclusiveMinimum {
		t.Errorf("m: min %v excl %v, want exclusive 3", *k.Minimum, k.ExclusiveMinimum)
	}
	if e := s.Types[n.Field("o").Type.Named]; e != nil {
		for _, v := range e.Values {
			if m, ok := v.Value.(map[string]any); ok {
				if _, leaked := m[orderKey]; leaked {
					t.Error("enum literal carries key-order metadata")
				}
			}
		}
	}
	for _, doc := range []string{
		"N: {enum: [null]}",
		"N: {oneOf: [{type: 'null'}]}",
	} {
		if _, err := parse(t, head+"components:\n  schemas:\n    "+doc+"\n", nil); err == nil {
			t.Errorf("%s: a null-only schema must be an error", doc)
		}
	}
}
