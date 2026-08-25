package service

import (
	"strings"
	"testing"
)

func TestPathParamsResolveNestedFieldPaths(t *testing.T) {
	ir := buildMusic(t)

	// `{track.name=artists/*/tracks/*}` binds a field two levels down, and both
	// spellings have to come out right: proto for the generated setter, JSON
	// for what the client sends and what a FieldViolation reports.
	binding := findMethod(t, ir, "UpdateTrack").Bindings[0]
	if len(binding.PathParams) != 1 {
		t.Fatalf("PathParams = %d, want 1", len(binding.PathParams))
	}

	path := binding.PathParams[0].Path
	if got := strings.Join(path.Proto, "."); got != "track.name" {
		t.Errorf("proto path = %q, want %q", got, "track.name")
	}
	if path.JSON != "track.name" {
		t.Errorf("json path = %q, want %q", path.JSON, "track.name")
	}
	if path.Leaf().Kind != KindString {
		t.Errorf("leaf kind = %v, want string", path.Leaf().Kind)
	}
}

func TestBodySpecs(t *testing.T) {
	ir := buildMusic(t)

	// body: "track" names a field.
	create := findMethod(t, ir, "CreateTrack").Bindings[0]
	if create.Body == nil || create.Body.Wildcard {
		t.Fatalf("CreateTrack body = %+v, want a named field", create.Body)
	}
	if create.Body.Field.JSON != "track" {
		t.Errorf("body field = %q, want %q", create.Body.Field.JSON, "track")
	}

	// body: "*" claims the whole message.
	withdraw := findMethod(t, ir, "WithdrawTrack").Bindings[0]
	if withdraw.Body == nil || !withdraw.Body.Wildcard {
		t.Errorf("WithdrawTrack body = %+v, want wildcard", withdraw.Body)
	}

	// A GET declares none.
	get := findMethod(t, ir, "GetTrack").Bindings[0]
	if get.Body != nil {
		t.Errorf("GetTrack body = %+v, want none", get.Body)
	}
}

func TestQueryParamsExcludeWhatThePathAndBodyBind(t *testing.T) {
	ir := buildMusic(t)
	binding := findMethod(t, ir, "ListTracks").Bindings[0]

	names := queryNames(binding)
	// `parent` is bound by the path, so it must not also be bindable from the
	// query: two sources for one field means one silently loses.
	if contains(names, "parent") {
		t.Errorf("query params = %v, must not include the path-bound %q", names, "parent")
	}
	for _, want := range []string{"pageSize", "pageToken"} {
		if !contains(names, want) {
			t.Errorf("query params = %v, want %q", names, want)
		}
	}
}

func TestAWildcardBodyLeavesNoQueryParams(t *testing.T) {
	ir := buildMusic(t)
	binding := findMethod(t, ir, "WithdrawTrack").Bindings[0]

	if got := queryNames(binding); len(got) != 0 {
		t.Errorf("query params = %v, want none: body \"*\" claims the whole message", got)
	}
}

func TestOutputOnlyFieldsAreNotBindableFromTheQuery(t *testing.T) {
	ir := buildMusic(t)
	binding := findMethod(t, ir, "CreateArtist").Bindings[0]

	// The body is `artist`, so the remaining input fields are queryable — but
	// an OUTPUT_ONLY one would let a caller claim a server-assigned value.
	for _, name := range queryNames(binding) {
		if strings.HasSuffix(name, "monthlyListeners") || strings.HasSuffix(name, "etag") {
			t.Errorf("query params include the OUTPUT_ONLY field %q", name)
		}
	}
}

func TestNestedMessageFieldsAreReachableFromTheQuery(t *testing.T) {
	ir := buildMusic(t)
	binding := findMethod(t, ir, "UpdateTrack").Bindings[0]

	// `update_mask` is a FieldMask, which protojson spells as a bare string, so
	// it is bindable rather than being walked into.
	if !contains(queryNames(binding), "updateMask") {
		t.Errorf("query params = %v, want %q", queryNames(binding), "updateMask")
	}
}

func TestResponsesDescribeFailureNotJustSuccess(t *testing.T) {
	ir := buildMusic(t)

	// An OpenAPI document listing only 200 propagates the same bug into every
	// generated client.
	get := findMethod(t, ir, "GetTrack").Bindings[0]
	statuses := map[int]bool{}
	for _, c := range get.Responses {
		statuses[c.HTTP] = true
	}
	for _, want := range []int{200, 400, 401, 403, 404, 429, 500, 503} {
		if !statuses[want] {
			t.Errorf("GetTrack responses = %v, want %d", statuses, want)
		}
	}

	// AIP-133: a Create returns 201, and can collide.
	create := findMethod(t, ir, "CreateArtist").Bindings[0]
	if create.Responses[0].HTTP != 201 {
		t.Errorf("CreateArtist success = %d, want 201", create.Responses[0].HTTP)
	}

	// google.protobuf.Empty with no response_body is 204.
	del := findMethod(t, ir, "DeleteArtist").Bindings[0]
	if del.Responses[0].HTTP != 204 {
		t.Errorf("DeleteArtist success = %d, want 204", del.Responses[0].HTTP)
	}
}

func TestResourcesAreOrderedParentFirst(t *testing.T) {
	ir := buildMusic(t)

	types := ir.ResourceTypes()
	artist := indexOf(types, "music.example.com/Artist")
	track := indexOf(types, "music.example.com/Track")

	if artist < 0 || track < 0 {
		t.Fatalf("resource types = %v, want both Artist and Track", types)
	}
	// The OpenAPI target emits tags in this order, so a reader meets an artist
	// before the tracks that live under one.
	if artist > track {
		t.Errorf("resource order = %v, want Artist before Track", types)
	}
}

func TestSingularAndPluralAreCaptured(t *testing.T) {
	ir := buildMusic(t)

	track := ir.Resources["music.example.com/Track"]
	if track == nil {
		t.Fatal("no Track resource")
	}
	// These are what the OpenAPI target builds its folder tree from; without
	// them an import is a flat list of URLs.
	if track.Singular != "track" || track.Plural != "tracks" {
		t.Errorf("singular/plural = %q/%q, want track/tracks", track.Singular, track.Plural)
	}
}

// queryNames returns a binding's query parameter names.
func queryNames(binding *Binding) []string {
	out := make([]string, 0, len(binding.QueryParams))
	for _, param := range binding.QueryParams {
		out = append(out, param.Path.JSON)
	}
	return out
}

// indexOf returns the position of a string, or -1.
func indexOf(haystack []string, needle string) int {
	for i, item := range haystack {
		if item == needle {
			return i
		}
	}
	return -1
}
