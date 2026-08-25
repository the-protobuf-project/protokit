package service

// Message is a proto message reachable from a bound method.
type Message struct {
	FullName string // "library.v1.Book"
	Name     string // "Book"
	Package  string // "library.v1"
	Doc      string

	// WellKnown is set when the message is one protojson or google.api treats
	// specially.
	WellKnown WellKnown

	Fields []*Field

	// Resource is the AIP-123 descriptor when the message declares one, which
	// is what makes it addressable by resource name.
	Resource *Resource

	// Oneofs groups field names by the oneof declaring them, excluding the
	// synthetic oneofs proto3 optional generates.
	Oneofs map[string][]string
}

// Field returns the field with the given proto name, or nil.
func (m *Message) Field(name string) *Field {
	for _, f := range m.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// Field is one field of a message.
type Field struct {
	Name     string // proto name: "display_name"
	JSONName string // protojson name: "displayName"
	Number   int32
	Doc      string

	Kind Kind

	// Message is the full proto name when Kind is KindMessage, and WellKnown
	// classifies it. Both are empty and WKNone otherwise.
	Message   string
	WellKnown WellKnown

	// Enum is the full proto name when Kind is KindEnum.
	Enum string

	Repeated bool

	// Optional is proto3 explicit presence. It is not the same as "not
	// required": AIP-203 REQUIRED is a separate, orthogonal annotation.
	Optional bool

	// MapKey and MapValue describe a map field's entry type when Kind is
	// KindMap.
	MapKey, MapValue *Field

	// Behavior is google.api.field_behavior (AIP-203).
	Behavior []Behavior

	// Format is google.api.field_info's format, e.g. "UUID4", or "".
	Format string

	// RefType is google.api.resource_reference's target type (AIP-124), e.g.
	// "library.example.com/Shelf", or "".
	RefType string

	// RefChild is true when the reference is a child_type rather than a type.
	RefChild bool
}

// Behavior is one google.api.field_behavior value (AIP-203).
type Behavior uint8

const (
	BehaviorUnspecified Behavior = iota
	BehaviorOptional
	BehaviorRequired
	BehaviorOutputOnly
	BehaviorInputOnly
	BehaviorImmutable
	BehaviorUnorderedList
	BehaviorNonEmptyDefault
	BehaviorIdentifier
)

// Has reports whether the field carries the given behavior.
func (f *Field) Has(b Behavior) bool {
	for _, got := range f.Behavior {
		if got == b {
			return true
		}
	}
	return false
}

// Bindable reports whether the field can be filled from a single string, which
// is what a path capture or query parameter provides. Message-typed fields
// qualify only when protojson spells them as a bare string.
func (f *Field) Bindable() bool {
	switch f.Kind {
	case KindMap:
		return false
	case KindMessage:
		if _, ok := f.WellKnown.Wrapper(); ok {
			return true
		}
		return f.WellKnown.StringLike()
	}
	return true
}

// Enum is a proto enum reachable from a bound method.
type Enum struct {
	FullName string
	Name     string
	Package  string
	Doc      string
	Values   []EnumValue
}

// EnumValue is one enum member. protojson encodes the name, not the number, so
// Name is what appears in JSON and in an OpenAPI enum list.
type EnumValue struct {
	Name   string
	Number int32
	Doc    string
}
