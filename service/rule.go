package service

// RuleKind identifies one validation check.
type RuleKind uint8

const (
	// RuleRequired is AIP-203 REQUIRED: the field must be present, and for a
	// scalar must not be at its default.
	RuleRequired RuleKind = iota

	// RuleOutputOnly is AIP-203 OUTPUT_ONLY appearing in a request at all.
	RuleOutputOnly

	// RuleImmutable is AIP-203 IMMUTABLE present in an Update request without
	// appearing in the update mask.
	RuleImmutable

	// RuleIdentifier is AIP-203 IDENTIFIER supplied in a Create body, where the
	// server assigns it.
	RuleIdentifier

	// RuleResourceName is an AIP-122/123 pattern check on a resource name.
	RuleResourceName

	// RuleFormat is a google.api.field_info format: UUID4, IPV4, IPV6,
	// IPV4_OR_IPV6.
	RuleFormat

	// RuleConstraint is a protovalidate constraint the generator folded to a
	// constant check it can emit directly.
	RuleConstraint

	// RuleCEL is a protovalidate constraint that needs a CEL evaluator. A build
	// producing one without the runtime feature enabled fails, rather than
	// silently dropping the check.
	RuleCEL
)

// Rule is one validation check to run before the RPC is dialled.
//
// Every rule names the field it guards by protojson path, because that is the
// name the client sent, the name the FieldViolation reports, and the name the
// OpenAPI document uses. A violation the caller cannot map back to what they
// typed is not much better than no message at all.
type Rule struct {
	Kind RuleKind
	Path *FieldPath

	// Reason is the FieldViolation reason token, e.g. "REQUIRED",
	// "RESOURCE_NAME_MALFORMED", "VALUE_LENGTH".
	Reason string

	// Message is the human-readable description, rendered into the violation.
	Message string

	// Patterns is the acceptable resource-name patterns, for RuleResourceName.
	Patterns []*Pattern

	// Format is the field_info format token, for RuleFormat.
	Format string

	// Constraint carries a folded protovalidate bound, for RuleConstraint.
	Constraint *Constraint

	// Expr is the CEL source, for RuleCEL. ID is protovalidate's constraint id.
	Expr string
	ID   string
}

// Constraint is a protovalidate bound the generator lowered to direct code.
// Only the constant subset appears here; anything needing the CEL environment
// stays a RuleCEL.
type Constraint struct {
	// MinLen and MaxLen bound a string's length in Unicode code points, or a
	// repeated field's element count.
	MinLen, MaxLen *uint64

	// Gt, Gte, Lt, Lte bound a numeric value. They are kept as float64 so one
	// field covers every numeric width; a generator narrows to the field's own
	// type when emitting.
	Gt, Gte, Lt, Lte *float64

	// Regex is an RE2 pattern the value must match.
	Regex string

	// In and NotIn enumerate allowed and forbidden values, rendered as strings
	// so enums and scalars share one representation.
	In, NotIn []string
}
