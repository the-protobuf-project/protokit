package service

// classify.go decides which AIP standard method an RPC is, which is what makes
// a middleware policy expressible in terms of what a method *means* rather than
// what it is called.

import (
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// classifyMethod returns the AIP pattern for a method.
//
// The name is the primary signal, because AIP-131 through AIP-136 define the
// standard methods by name. It is then checked against the shape the pattern
// implies, so a method called GetBook that takes a body is not silently
// treated as an AIP-131 Get: a mismatch means the author meant something else,
// and guessing wrong would put it in the wrong middleware bucket.
func classifyMethod(md protoreflect.MethodDescriptor, rules []*httpBinding) MethodPattern {
	name := string(md.Name())

	// Batch prefixes are checked first: BatchGetBooks starts with "Batch", and
	// testing "Get" first would classify it as a plain Get.
	//
	// These return before the binding checks below, deliberately. AIP-231
	// through AIP-235 *define* the batch methods with custom verbs —
	// `GET /v1/{parent}/books:batchGet` — so treating a verb as disqualifying
	// would misclassify every conformant batch method as custom.
	switch {
	case strings.HasPrefix(name, "BatchGet"):
		return PatternBatchGet
	case strings.HasPrefix(name, "BatchCreate"):
		return PatternBatchCreate
	case strings.HasPrefix(name, "BatchUpdate"):
		return PatternBatchUpdate
	case strings.HasPrefix(name, "BatchDelete"):
		return PatternBatchDelete
	case strings.HasPrefix(name, "Undelete"):
		return PatternUndelete
	}

	pattern, ok := standardPrefix(name)
	if !ok {
		return PatternCustom
	}
	// Every binding must agree with the pattern, not just the first. A method
	// named GetBook with an additional POST binding is not a Get, and treating
	// it as one would mark it non-mutating and exempt it from any policy
	// written against Selector::Mutating.
	for _, rule := range rules {
		if rule == nil {
			continue
		}
		// A custom verb means AIP-136 regardless of the name: ArchiveBook and
		// GetBook:archive are both custom methods.
		if rule.verb != "" {
			return PatternCustom
		}
		if !methodMatchesPattern(pattern, rule.httpMethod) {
			return PatternCustom
		}
	}
	return pattern
}

// standardPrefix maps a name prefix to its pattern.
func standardPrefix(name string) (MethodPattern, bool) {
	switch {
	case strings.HasPrefix(name, "Get"):
		return PatternGet, true
	case strings.HasPrefix(name, "List"):
		return PatternList, true
	case strings.HasPrefix(name, "Create"):
		return PatternCreate, true
	case strings.HasPrefix(name, "Update"):
		return PatternUpdate, true
	case strings.HasPrefix(name, "Delete"):
		return PatternDelete, true
	}
	return PatternCustom, false
}

// methodMatchesPattern reports whether an HTTP method is the one a standard
// pattern is defined to use.
//
// AIP-134 allows either PUT or PATCH for an Update; everything else has one
// spelling.
func methodMatchesPattern(pattern MethodPattern, httpMethod string) bool {
	switch pattern {
	case PatternGet, PatternList:
		return httpMethod == "GET"
	case PatternCreate:
		return httpMethod == "POST"
	case PatternUpdate:
		return httpMethod == "PATCH" || httpMethod == "PUT"
	case PatternDelete:
		return httpMethod == "DELETE"
	}
	return true
}

// mutating reports whether a method changes state.
//
// The pattern decides it when the pattern is a write. Otherwise the bindings
// do: a method classified as a read but bound to a POST is mutating, because
// the binding is what the request actually performs. Trusting the pattern
// alone would exempt such a method from every policy written against
// Selector::Mutating.
//
// A custom method with no binding is assumed mutating, which is the
// conservative reading of an annotation that says nothing.
func mutating(pattern MethodPattern, bindings []*Binding) bool {
	// A standard write pattern is mutating whatever its bindings say.
	// PatternCustom is excluded because MethodPattern.mutating reports true for
	// it as a default, and for a custom method the bindings are the better
	// evidence.
	if pattern != PatternCustom && pattern.mutating() {
		return true
	}
	if len(bindings) == 0 {
		// Nothing to inspect. A custom method is assumed mutating, the
		// conservative reading of an annotation that says nothing; a standard
		// read pattern with no HTTP binding is still a read.
		return pattern == PatternCustom
	}
	for _, b := range bindings {
		if b.HTTPMethod != "GET" && b.HTTPMethod != "HEAD" {
			return true
		}
	}
	return false
}
