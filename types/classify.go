package types

// classify.go is the generic, target-agnostic part of the type system: it maps a
// proto field to a neutral schema.FieldType. The postgres/mongodb/solidity
// projections in this package consume the classification indirectly (today via
// SQLType); a generator can map schema.FieldType to its own type system directly.

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/the-protobuf-project/protokit/schema"
)

// ClassifyField returns the neutral FieldType of a proto field, mirroring the
// distinctions PostgresType makes (String vs FieldMask, the four integer widths,
// the well-known temporal/decimal/geo types). Cardinality is not encoded here —
// the caller records Column.List separately. Enum fields return TypeEnum.
func ClassifyField(f *protogen.Field) schema.FieldType {
	if f.Enum != nil {
		return schema.TypeEnum
	}
	if msg := f.Desc.Message(); msg != nil {
		if t, ok := wellKnownFieldType[string(msg.FullName())]; ok {
			return t
		}
		return schema.TypeJSON // freeform or non-relationalized message
	}
	if t, ok := scalarFieldType[f.Desc.Kind()]; ok {
		return t
	}
	return schema.TypeUnknown
}

// scalarFieldType maps proto scalar kinds to neutral field types, matching
// scalarPostgres: unsigned 32/64 widen conceptually (they map to wider SQL) but
// keep a distinct neutral type so each generator decides how to widen.
var scalarFieldType = map[protoreflect.Kind]schema.FieldType{
	protoreflect.BoolKind:     schema.TypeBool,
	protoreflect.Int32Kind:    schema.TypeInt32,
	protoreflect.Sint32Kind:   schema.TypeInt32,
	protoreflect.Sfixed32Kind: schema.TypeInt32,
	protoreflect.Uint32Kind:   schema.TypeUint32,
	protoreflect.Fixed32Kind:  schema.TypeUint32,
	protoreflect.Int64Kind:    schema.TypeInt64,
	protoreflect.Sint64Kind:   schema.TypeInt64,
	protoreflect.Sfixed64Kind: schema.TypeInt64,
	protoreflect.Uint64Kind:   schema.TypeUint64,
	protoreflect.Fixed64Kind:  schema.TypeUint64,
	protoreflect.FloatKind:    schema.TypeFloat,
	protoreflect.DoubleKind:   schema.TypeDouble,
	protoreflect.StringKind:   schema.TypeString,
	protoreflect.BytesKind:    schema.TypeBytes,
}

// wellKnownFieldType maps google.protobuf and google.type messages to neutral
// field types, matching wellKnownPostgres. Wrapper types collapse to their scalar
// FieldType; every other (non-freeform) message is relationalized upstream and
// never reaches ClassifyField as a column.
var wellKnownFieldType = map[string]schema.FieldType{
	"google.protobuf.Timestamp":   schema.TypeTimestamp,
	"google.protobuf.Duration":    schema.TypeDuration,
	"google.protobuf.DoubleValue": schema.TypeDouble,
	"google.protobuf.FloatValue":  schema.TypeFloat,
	"google.protobuf.Int32Value":  schema.TypeInt32,
	"google.protobuf.UInt32Value": schema.TypeUint32,
	"google.protobuf.Int64Value":  schema.TypeInt64,
	"google.protobuf.UInt64Value": schema.TypeUint64,
	"google.protobuf.BoolValue":   schema.TypeBool,
	"google.protobuf.StringValue": schema.TypeString,
	"google.protobuf.BytesValue":  schema.TypeBytes,
	"google.protobuf.FieldMask":   schema.TypeText,
	"google.type.Date":            schema.TypeDate,
	"google.type.TimeOfDay":       schema.TypeTimeOfDay,
	"google.type.DateTime":        schema.TypeTimestamp,
	"google.type.Decimal":         schema.TypeDecimal,
	"google.type.LatLng":          schema.TypeLatLng,
	"google.type.Interval":        schema.TypeInterval,
}

// freeformMessage is the set of google.protobuf messages whose shape is dynamic
// or type-erased: arbitrary JSON (Struct, Value, ListValue), a boxed message of
// any type (Any), or an empty marker (Empty). They have no stable column layout,
// so they stay a single JSON-ish column rather than being relationalized.
var freeformMessage = map[string]bool{
	"google.protobuf.Struct":    true,
	"google.protobuf.Value":     true,
	"google.protobuf.ListValue": true,
	"google.protobuf.Any":       true,
	"google.protobuf.Empty":     true,
}

// Relationalizable reports whether a message-typed field should be normalized
// into its own table (a primary key plus the foreign key linking it to its
// owner) rather than mapped to a single column. It is false for the well-known
// types with a native single-column mapping (Timestamp, Duration, the wrappers,
// Date, LatLng, …) and for the freeform google.protobuf wrappers (Struct, Any,
// …), both of which keep their scalar / JSON mapping. Every other message — a
// user-defined nested message and an imported value type alike (google.type.Money,
// google.type.PostalAddress, a third-party proto) — is relationalized, so its
// structure stays queryable instead of collapsing into an opaque blob.
func Relationalizable(fullName string) bool {
	if _, ok := wellKnownFieldType[fullName]; ok {
		return false
	}
	return !freeformMessage[fullName]
}
