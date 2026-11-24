package openapi

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/the-protobuf-project/protokit/apispec"
)

func load(t *testing.T, p string) *apispec.Spec {
	t.Helper()
	s, err := Load(context.Background(), p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func op(t *testing.T, s *apispec.Spec, id string) *apispec.Operation {
	t.Helper()
	for _, o := range s.Operations {
		if o.ID == id {
			return o
		}
	}
	t.Fatalf("no operation %q", id)
	return nil
}

func typ(t *testing.T, s *apispec.Spec, n string) *apispec.Type {
	t.Helper()
	ty := s.Types[n]
	if ty == nil {
		t.Fatalf("no type %q (have %v)", n, s.TypeNames())
	}
	return ty
}

func field(t *testing.T, ty *apispec.Type, n string) *apispec.Field {
	t.Helper()
	f := ty.Field(n)
	if f == nil {
		t.Fatalf("%s: no field %q", ty.Name, n)
	}
	return f
}

func TestOpenAPI30(t *testing.T) {
	s := load(t, "testdata/v30/api.yaml")
	if s.SourceVersion != "3.0.3" || s.Title != "Library API" {
		t.Fatalf("info: %+v", s)
	}
	if s.Servers[0].URL != "https://eu.library.example.com/v1" {
		t.Errorf("server var not substituted: %q", s.Servers[0].URL)
	}
	if g := s.Group("books"); g == nil || g.Parent != "Catalog" || g.Description != "Book catalog." {
		t.Errorf("books group: %+v", g)
	}

	book := typ(t, s, "Book")
	if book.Synthetic || book.Kind != apispec.KindObject {
		t.Errorf("Book: %+v", book)
	}
	if f := field(t, book, "id"); !f.ReadOnly || f.Type.Scalar != apispec.ScalarUUID {
		t.Errorf("id: %+v", f)
	}
	if f := field(t, book, "title"); !f.Required || *f.Constraints.MaxLength != 200 {
		t.Errorf("title: %+v", f)
	}
	if f := field(t, book, "isbn"); !f.Nullable || f.Constraints.Pattern == "" {
		t.Errorf("isbn: %+v", f)
	}
	st := typ(t, s, field(t, book, "status").Type.Named)
	if st.Kind != apispec.KindEnum || len(st.Values) != 3 || st.Values[0].Description != "Not yet visible." {
		t.Errorf("status enum: %+v", st)
	}
	if f := field(t, book, "metadata"); f.Type.Map == nil || f.Type.Map.Scalar != apispec.ScalarString {
		t.Errorf("metadata map: %+v", f.Type)
	}
	if f := field(t, book, "tags"); f.Type.List == nil || !f.Constraints.UniqueItems {
		t.Errorf("tags: %+v", f)
	}
	media := typ(t, s, "Media")
	if media.Kind != apispec.KindUnion || strings.Join(media.Variants, ",") != "Image,Video" {
		t.Errorf("Media: %+v", media)
	}
	if media.Discriminator == nil || media.Discriminator.Mapping["video"] != "Video" {
		t.Errorf("discriminator: %+v", media.Discriminator)
	}
	video := typ(t, s, "Video")
	if len(video.Implements) != 1 || video.Implements[0] != "MediaBase" {
		t.Errorf("Video implements: %v", video.Implements)
	}
	if f := field(t, video, "kind"); !f.Required {
		t.Errorf("Video.kind should be required via MediaBase")
	}
	if f := field(t, video, "duration"); f.Type.Scalar != apispec.ScalarDuration || !f.Required {
		t.Errorf("duration: %+v", f)
	}
	author := typ(t, s, "Author") // from models.yaml
	if f := field(t, author, "books"); f.Type.List == nil || f.Type.List.Named != "Book" {
		t.Errorf("cross-file ref: %+v", f.Type)
	}
	if f := field(t, author, "mentor"); f.Type.Named != "Author" {
		t.Errorf("self ref: %+v", f.Type)
	}

	list := op(t, s, "listBooks")
	if list.Pagination == nil || list.Pagination.ItemsField != "books" || list.Pagination.SizeParam != "page_size" || list.Pagination.Style != "cursor" {
		t.Errorf("pagination: %+v", list.Pagination)
	}
	if p := list.Params[0]; p.Name != "page_size" || p.Default != int64(20) || *p.Constraints.Maximum != 100 {
		t.Errorf("$ref param: %+v", p)
	}
	get := op(t, s, "getBook")
	if len(get.Params) != 1 || get.Params[0].Type.Scalar != apispec.ScalarUUID || !get.Params[0].Required {
		t.Errorf("path-level param: %+v", get.Params)
	}
	if get.Responses[0].Headers[0].Name != "ETag" {
		t.Errorf("headers: %+v", get.Responses[0].Headers)
	}
	create := op(t, s, "createBook")
	if create.Body == nil || create.Body.Type.Named != "Book" || !create.Body.Required {
		t.Errorf("body: %+v", create.Body)
	}
	if len(create.Responses) != 2 || create.Responses[1].Status != "DEFAULT" || create.Responses[1].Type.Named != "Error" {
		t.Errorf("responses: %+v", create.Responses)
	}
	if a := s.Operations[len(s.Operations)-1]; !strings.HasPrefix(a.ID, "getAuthorsAuthor") {
		t.Errorf("synthesized id: %q", a.ID)
	}
	if g := book.Groups; len(g) != 2 || g[0] != "books" {
		t.Errorf("Book groups: %v", g)
	}
}

func TestSwagger20(t *testing.T) {
	s := load(t, "testdata/v20/petstore.json")
	if s.Servers[0].URL != "https://petstore.example.com/v2" {
		t.Errorf("server: %+v", s.Servers)
	}
	add := op(t, s, "addPet")
	if add.Body == nil || add.Body.Type.Named != "Pet" || add.Security[0]["petstore_auth"][0] != "write:pets" {
		t.Errorf("addPet: %+v", add)
	}
	up := op(t, s, "uploadFile")
	form := typ(t, s, up.Body.Type.Named)
	if f := field(t, form, "file"); f.Type.Scalar != apispec.ScalarBytes {
		t.Errorf("file: %+v", f.Type)
	}
	find := op(t, s, "findPetsByStatus")
	if p := find.Params[0]; p.Type.List == nil || s.Types[p.Type.List.Named].Kind != apispec.KindEnum {
		t.Errorf("2.0 array param: %+v", p.Type)
	}
	if f := field(t, typ(t, s, "Pet"), "x-owner"); !f.Nullable {
		t.Errorf("x-nullable")
	}
	if sc := s.Security["basic"]; sc.Type != "http" || sc.Scheme != "basic" {
		t.Errorf("basic: %+v", sc)
	}
	if sc := s.Security["petstore_auth"]; sc.Flows["implicit"].Scopes["read:pets"] != "read" {
		t.Errorf("oauth: %+v", sc)
	}
}

func TestOpenAPI31(t *testing.T) {
	s := load(t, "testdata/v31/api.yaml")
	ev := typ(t, s, "Event")
	lvl := typ(t, s, field(t, ev, "level").Type.Named)
	if lvl.Kind != apispec.KindEnum || lvl.Values[1].Description != "High." {
		t.Errorf("const enum: %+v", lvl)
	}
	if f := field(t, ev, "payload"); !f.Nullable || f.Type.Named != "Payload" {
		t.Errorf("anyOf null: %+v", f)
	}
	if f := field(t, ev, "kind"); f.Constraints.Const != "event" {
		t.Errorf("const: %+v", f.Constraints)
	}
	if f := field(t, ev, "labels"); f.Type.Map == nil {
		t.Errorf("labels: %+v", f.Type)
	}
	if f := field(t, typ(t, s, "Payload"), "body"); f.Type.Scalar != apispec.ScalarBytes {
		t.Errorf("contentEncoding: %+v", f.Type)
	}
	l := op(t, s, "listEvents")
	if p := l.Params[0]; !p.Constraints.ExclusiveMinimum || *p.Constraints.Minimum != 0 {
		t.Errorf("exclusiveMinimum: %+v", p.Constraints)
	}
	if l.Pagination == nil || l.Pagination.ItemsField != "data" || l.Pagination.NextField != "next_cursor" || l.Pagination.ItemType != "Event" {
		t.Errorf("pagination: %+v", l.Pagination)
	}
	var wh *apispec.Operation
	for _, o := range s.Operations {
		if o.Kind == apispec.OpWebhook {
			wh = o
		}
	}
	if wh == nil || wh.HTTP.Path != "eventCreated" || wh.Body.Type.Named != "Event" {
		t.Errorf("webhook: %+v", wh)
	}
}

// TestSmoke loads OPENAPI_SMOKE (comma-separated paths/URLs) and reports stats.
func TestSmoke(t *testing.T) {
	env := os.Getenv("OPENAPI_SMOKE")
	if env == "" {
		t.Skip("OPENAPI_SMOKE not set")
	}
	for _, p := range strings.Split(env, ",") {
		s := load(t, p)
		kinds := map[apispec.Kind]int{}
		synth := 0
		for _, ty := range s.Types {
			kinds[ty.Kind]++
			if ty.Synthetic {
				synth++
			}
		}
		paged := 0
		for _, o := range s.Operations {
			if o.Pagination != nil {
				paged++
			}
		}
		var ks []string
		for k, n := range kinds {
			ks = append(ks, fmt.Sprintf("%s=%d", k, n))
		}
		sort.Strings(ks)
		t.Logf("%s: v%s ops=%d paged=%d types=%d (%s) synthetic=%d groups=%d", p, s.SourceVersion, len(s.Operations), paged, len(s.Types), strings.Join(ks, " "), synth, len(s.Groups))
	}
}
