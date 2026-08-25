package service

// responses.go derives the statuses a binding can actually produce.
//
// This exists for the OpenAPI target, and it is the difference between a
// document that describes an API and one that only describes its happy path.
// grpc-gateway's generator emits 200 and a `default`, so every generated client
// inherits the assumption that nothing else happens.

// responsesFor returns the statuses a binding can produce, success first.
func (b *builder) responsesFor(svc *Service, method *Method, binding *Binding) []StatusCase {
	cases := []StatusCase{b.successCase(method, binding)}

	// Anything that reads a body or binds a field can be sent a bad one.
	if binding.Body != nil || len(binding.PathParams) > 0 || len(binding.QueryParams) > 0 {
		cases = append(cases, StatusCase{
			HTTP:   400,
			Code:   "INVALID_ARGUMENT",
			Reason: "A field is missing, malformed, or not recognised.",
		})
	}

	// A service that declares scopes is one that can refuse a caller.
	if svc != nil && len(svc.Scopes) > 0 {
		cases = append(cases,
			StatusCase{HTTP: 401, Code: "UNAUTHENTICATED", Reason: "No valid credentials were supplied."},
			StatusCase{HTTP: 403, Code: "PERMISSION_DENIED", Reason: "The caller lacks the required scope."},
		)
	}

	// A binding that names a resource can fail to find it. One that names none
	// — an unparented List or Create — cannot.
	if len(binding.PathParams) > 0 {
		cases = append(cases, StatusCase{
			HTTP:   404,
			Code:   "NOT_FOUND",
			Reason: "The named resource does not exist.",
		})
	}

	switch method.Pattern {
	case PatternCreate, PatternBatchCreate:
		cases = append(cases, StatusCase{
			HTTP:   409,
			Code:   "ALREADY_EXISTS",
			Reason: "A resource with that name already exists.",
		})
	case PatternUpdate, PatternDelete, PatternBatchUpdate, PatternBatchDelete:
		cases = append(cases, StatusCase{
			HTTP:   409,
			Code:   "ABORTED",
			Reason: "A concurrent change conflicted with this one.",
		})
	}

	// Every method can be rate limited, can fail, and can be unavailable.
	cases = append(cases,
		StatusCase{HTTP: 429, Code: "RESOURCE_EXHAUSTED", Reason: "The caller is over quota."},
		StatusCase{HTTP: 500, Code: "INTERNAL", Reason: "An unexpected failure occurred."},
		StatusCase{HTTP: 503, Code: "UNAVAILABLE", Reason: "The service is temporarily unavailable."},
	)
	return cases
}

// successCase returns the success status for a binding.
//
// The three special cases are AIP-133's 201, AIP-151's 202, and the 204 an
// empty response earns; everything else is 200.
func (b *builder) successCase(method *Method, binding *Binding) StatusCase {
	switch {
	case method.Pattern == PatternCreate && binding.HTTPMethod == "POST":
		return StatusCase{HTTP: 201, Code: "OK", Reason: "The resource was created."}

	case method.Output != nil && method.Output.WellKnown == WKOperation:
		return StatusCase{HTTP: 202, Code: "OK", Reason: "The operation was accepted and is running."}

	case method.Output != nil && method.Output.WellKnown == WKEmpty && binding.ResponseBody == nil:
		return StatusCase{HTTP: 204, Code: "OK", Reason: "The request succeeded with no response body."}
	}
	return StatusCase{HTTP: 200, Code: "OK", Reason: "The request succeeded."}
}
