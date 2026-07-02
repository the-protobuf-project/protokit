package schema

// FieldType is the neutral, target-agnostic type of a column, classified from the
// proto field alone. Each generator maps it to its own type system (orm → SQL/Go,
// web3 → Solidity), so the shared IR carries no db- or chain-specific type. Enum
// fields also set Column.Enum; their FieldType is TypeEnum. Cardinality is carried
// separately by Column.List, so a repeated field keeps its element FieldType.
type FieldType int

const (
	TypeUnknown   FieldType = iota
	TypeString              // string, StringValue
	TypeBool                // bool, BoolValue
	TypeInt32               // int32, sint32, sfixed32, Int32Value
	TypeUint32              // uint32, fixed32, UInt32Value
	TypeInt64               // int64, sint64, sfixed64, Int64Value
	TypeUint64              // uint64, fixed64, UInt64Value
	TypeFloat               // float, FloatValue
	TypeDouble              // double, DoubleValue
	TypeBytes               // bytes, BytesValue
	TypeEnum                // enum field (see Column.Enum)
	TypeTimestamp           // google.protobuf.Timestamp, google.type.DateTime
	TypeDuration            // google.protobuf.Duration
	TypeDate                // google.type.Date
	TypeTimeOfDay           // google.type.TimeOfDay
	TypeDecimal             // google.type.Decimal
	TypeLatLng              // google.type.LatLng
	TypeInterval            // google.type.Interval
	TypeText                // google.protobuf.FieldMask (long free text)
	TypeJSON                // freeform / non-relationalized messages (Struct, Any, …)

	// The following are never classified from a proto field — they are set by
	// protokit's synthesis passes on the columns it generates, so each generator
	// projects a synthesized surrogate key to its own type.
	TypeULID // synthesized ULID surrogate primary key
	TypeUUID // synthesized UUID surrogate primary key
)
