package service

// kind.go classifies proto field types at proto granularity, which is what a
// JSON codec needs and what a database-oriented type system loses.

// Kind is a proto field's type, kept at proto granularity rather than collapsed
// to a neutral one.
//
// protokit's schema.FieldType exists for a database backend and folds
// distinctions a gateway needs: the four 64-bit widths all become "int64" there,
// but protojson encodes every one of them as a JSON string while the 32-bit
// widths stay numbers, and a codec that gets that wrong loses precision
// silently.
type Kind uint8

const (
	KindDouble Kind = iota
	KindFloat
	KindInt32
	KindInt64
	KindUint32
	KindUint64
	KindSint32
	KindSint64
	KindFixed32
	KindFixed64
	KindSfixed32
	KindSfixed64
	KindBool
	KindString
	KindBytes
	KindEnum
	KindMessage
	KindMap
)

// JSONString reports whether protojson encodes this kind as a JSON string.
// Every 64-bit integer width does, which is the rule most often got wrong.
func (k Kind) JSONString() bool {
	switch k {
	case KindInt64, KindUint64, KindSint64, KindFixed64, KindSfixed64, KindBytes, KindString:
		return true
	}
	return false
}

// Numeric reports whether the kind is a number in proto terms, regardless of
// how protojson spells it.
func (k Kind) Numeric() bool {
	switch k {
	case KindDouble, KindFloat, KindInt32, KindInt64, KindUint32, KindUint64,
		KindSint32, KindSint64, KindFixed32, KindFixed64, KindSfixed32, KindSfixed64:
		return true
	}
	return false
}

// Bindable reports whether a value of this kind can be produced by parsing a
// single string, which is what a path capture or a query parameter supplies.
// Messages and maps cannot, except for the well-known types handled separately.
func (k Kind) Bindable() bool {
	return k != KindMap && k != KindMessage
}

// String returns the proto spelling of the kind, so a diagnostic naming a
// field's type reads as the author wrote it rather than as an enum number.
func (k Kind) String() string {
	switch k {
	case KindDouble:
		return "double"
	case KindFloat:
		return "float"
	case KindInt32:
		return "int32"
	case KindInt64:
		return "int64"
	case KindUint32:
		return "uint32"
	case KindUint64:
		return "uint64"
	case KindSint32:
		return "sint32"
	case KindSint64:
		return "sint64"
	case KindFixed32:
		return "fixed32"
	case KindFixed64:
		return "fixed64"
	case KindSfixed32:
		return "sfixed32"
	case KindSfixed64:
		return "sfixed64"
	case KindBool:
		return "bool"
	case KindString:
		return "string"
	case KindBytes:
		return "bytes"
	case KindEnum:
		return "enum"
	case KindMessage:
		return "message"
	case KindMap:
		return "map"
	}
	return "unknown"
}
