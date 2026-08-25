package service

// pattern.go classifies a method against the AIP standard methods, and models
// the per-binding metadata that classification feeds: response sets, routing
// headers, long-running operations, and diagnostics.

// MethodPattern classifies a method against the AIP standard methods.
type MethodPattern uint8

const (
	// PatternCustom is AIP-136: anything that is not a standard method. It is
	// the zero value because it is the honest default for an unannotated or
	// unrecognised method.
	PatternCustom      MethodPattern = iota
	PatternGet                       // AIP-131
	PatternList                      // AIP-132
	PatternCreate                    // AIP-133
	PatternUpdate                    // AIP-134
	PatternDelete                    // AIP-135
	PatternUndelete                  // AIP-164
	PatternBatchGet                  // AIP-231
	PatternBatchCreate               // AIP-233
	PatternBatchUpdate               // AIP-234
	PatternBatchDelete               // AIP-235
)

func (p MethodPattern) String() string {
	switch p {
	case PatternGet:
		return "Get"
	case PatternList:
		return "List"
	case PatternCreate:
		return "Create"
	case PatternUpdate:
		return "Update"
	case PatternDelete:
		return "Delete"
	case PatternUndelete:
		return "Undelete"
	case PatternBatchGet:
		return "BatchGet"
	case PatternBatchCreate:
		return "BatchCreate"
	case PatternBatchUpdate:
		return "BatchUpdate"
	case PatternBatchDelete:
		return "BatchDelete"
	}
	return "Custom"
}

// mutating reports whether the pattern changes state. A custom method is
// assumed mutating unless its binding is a GET, which the builder decides.
func (p MethodPattern) mutating() bool {
	switch p {
	case PatternGet, PatternList, PatternBatchGet:
		return false
	}
	return true
}

// StatusCase is one HTTP outcome a binding can produce, and why.
type StatusCase struct {
	HTTP   int    // 200, 201, 400, …
	Code   string // the google.rpc.Code name, e.g. "INVALID_ARGUMENT"
	Reason string // why this binding can produce it, for the OpenAPI description
}

// RoutingParam is one google.api.routing rule: a request field, and the
// template that extracts the part of it to send as a routing header.
type RoutingParam struct {
	Field    *FieldPath
	Template string // the path_template, empty meaning the whole value
	Key      string // the resulting x-goog-request-params key
}

// LROInfo is google.longrunning.operation_info on a method returning
// google.longrunning.Operation (AIP-151).
type LROInfo struct {
	ResponseType string // full proto name of the eventual result
	MetadataType string // full proto name of the progress metadata
}

// Diagnostic is a recoverable problem found while building the IR.
type Diagnostic struct {
	// Rule names the check, so a plugin's strictness setting can address it:
	// "aip", "route", "cel", "binding".
	Rule string

	// Subject is what the problem is about — a method's full name, a template.
	Subject string

	Message string
}
