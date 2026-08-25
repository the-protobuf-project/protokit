package service

// binding_reject_test.go covers the shapes buildBinding must refuse.

import (
	"strings"
	"testing"
)

// google/api/http.proto is explicit that a path variable must not refer to a
// repeated field, and a "**" is no exception: it captures one value, the
// matched segments joined with "/", so there is nothing to distribute across
// elements.
func TestRepeatedPathVariablesAreRejected(t *testing.T) {
	msg := &Message{
		FullName: "test.Req",
		Fields: []*Field{
			{Name: "tags", JSONName: "tags", Kind: KindString, Repeated: true},
		},
	}
	b := &builder{messages: map[string]*Message{"test.Req": msg}}

	for _, template := range []string{"/v1/{tags}", "/v1/{tags=**}"} {
		raw := &httpBinding{httpMethod: "GET", template: template}
		_, err := b.buildBinding(0, raw, msg, nil, "test")
		if err == nil {
			t.Errorf("buildBinding(%q) succeeded; a repeated path variable must be rejected", template)
			continue
		}
		if !strings.Contains(err.Error(), "non-repeated") {
			t.Errorf("buildBinding(%q) = %v, want a non-repeated-field error", template, err)
		}
	}
}

// response_body was resolved against binding.responseMessage before it was
// assigned, so any rule using it failed with "the response message is not
// resolvable".
func TestResponseBodyResolvesAgainstTheOutputMessage(t *testing.T) {
	input := &Message{FullName: "test.Req", Fields: []*Field{
		{Name: "name", JSONName: "name", Kind: KindString},
	}}
	inner := &Message{FullName: "test.Inner", Fields: []*Field{
		{Name: "value", JSONName: "value", Kind: KindString},
	}}
	output := &Message{FullName: "test.Resp", Fields: []*Field{
		{Name: "payload", JSONName: "payload", Kind: KindMessage, Message: "test.Inner"},
	}}
	b := &builder{messages: map[string]*Message{
		"test.Req": input, "test.Resp": output, "test.Inner": inner,
	}}

	raw := &httpBinding{httpMethod: "GET", template: "/v1/{name}", responseBody: "payload"}
	binding, err := b.buildBinding(0, raw, input, output, "test")
	if err != nil {
		t.Fatalf("buildBinding: %v", err)
	}
	if binding.ResponseBody == nil || binding.ResponseBody.JSON != "payload" {
		t.Errorf("ResponseBody = %v, want the payload field", binding.ResponseBody)
	}
}

// google/api/http.proto describes body and response_body as "the name of the
// field" — singular, not a field path.
func TestDottedBodyPathsAreRejected(t *testing.T) {
	inner := &Message{FullName: "test.Inner", Fields: []*Field{
		{Name: "deep", JSONName: "deep", Kind: KindMessage, Message: "test.Leaf"},
	}}
	leaf := &Message{FullName: "test.Leaf"}
	input := &Message{FullName: "test.Req", Fields: []*Field{
		{Name: "name", JSONName: "name", Kind: KindString},
		{Name: "inner", JSONName: "inner", Kind: KindMessage, Message: "test.Inner"},
	}}
	b := &builder{messages: map[string]*Message{
		"test.Req": input, "test.Inner": inner, "test.Leaf": leaf,
	}}

	raw := &httpBinding{httpMethod: "POST", template: "/v1/{name}", body: "inner.deep"}
	if _, err := b.buildBinding(0, raw, input, input, "test"); err == nil {
		t.Error("a dotted body path was accepted; it must name a top-level field")
	} else if !strings.Contains(err.Error(), "top-level") {
		t.Errorf("got %v, want a top-level-field error", err)
	}

	// A top-level message field is still fine.
	ok := &httpBinding{httpMethod: "POST", template: "/v1/{name}", body: "inner"}
	if _, err := b.buildBinding(0, ok, input, input, "test"); err != nil {
		t.Errorf("a top-level body field was rejected: %v", err)
	}
}
