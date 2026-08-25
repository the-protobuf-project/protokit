package service

// wellknown.go identifies the messages protojson encodes specially, plus the two
// google.api types that change how a request or response body is handled.

// WellKnown identifies the messages protojson gives a bespoke encoding, plus
// the two google.api types that change how a body is handled.
type WellKnown uint8

const (
	WKNone WellKnown = iota
	WKTimestamp
	WKDuration
	WKFieldMask
	WKAny
	WKStruct
	WKValue
	WKListValue
	WKNullValue
	WKEmpty
	WKDoubleValue
	WKFloatValue
	WKInt64Value
	WKUInt64Value
	WKInt32Value
	WKUInt32Value
	WKBoolValue
	WKStringValue
	WKBytesValue
	// WKHTTPBody is google.api.HttpBody: a body that bypasses the codec
	// entirely, carrying raw bytes and its own content type.
	WKHTTPBody
	// WKOperation is google.longrunning.Operation (AIP-151).
	WKOperation
)

// wellKnownByName maps full proto names to their WellKnown classification.
var wellKnownByName = map[string]WellKnown{
	"google.protobuf.Timestamp":    WKTimestamp,
	"google.protobuf.Duration":     WKDuration,
	"google.protobuf.FieldMask":    WKFieldMask,
	"google.protobuf.Any":          WKAny,
	"google.protobuf.Struct":       WKStruct,
	"google.protobuf.Value":        WKValue,
	"google.protobuf.ListValue":    WKListValue,
	"google.protobuf.NullValue":    WKNullValue,
	"google.protobuf.Empty":        WKEmpty,
	"google.protobuf.DoubleValue":  WKDoubleValue,
	"google.protobuf.FloatValue":   WKFloatValue,
	"google.protobuf.Int64Value":   WKInt64Value,
	"google.protobuf.UInt64Value":  WKUInt64Value,
	"google.protobuf.Int32Value":   WKInt32Value,
	"google.protobuf.UInt32Value":  WKUInt32Value,
	"google.protobuf.BoolValue":    WKBoolValue,
	"google.protobuf.StringValue":  WKStringValue,
	"google.protobuf.BytesValue":   WKBytesValue,
	"google.api.HttpBody":          WKHTTPBody,
	"google.longrunning.Operation": WKOperation,
}

// WellKnownOf classifies a message by full proto name.
func WellKnownOf(fullName string) WellKnown { return wellKnownByName[fullName] }

// StringLike reports whether protojson encodes the well-known type as a bare
// JSON string, which is what makes it bindable from a path or query parameter
// even though it is a message.
func (w WellKnown) StringLike() bool {
	switch w {
	case WKTimestamp, WKDuration, WKFieldMask, WKStringValue, WKBytesValue:
		return true
	}
	return false
}

// Wrapper returns the scalar kind a google.protobuf wrapper wraps, and whether
// the well-known type is a wrapper at all.
func (w WellKnown) Wrapper() (Kind, bool) {
	switch w {
	case WKDoubleValue:
		return KindDouble, true
	case WKFloatValue:
		return KindFloat, true
	case WKInt64Value:
		return KindInt64, true
	case WKUInt64Value:
		return KindUint64, true
	case WKInt32Value:
		return KindInt32, true
	case WKUInt32Value:
		return KindUint32, true
	case WKBoolValue:
		return KindBool, true
	case WKStringValue:
		return KindString, true
	case WKBytesValue:
		return KindBytes, true
	}
	return 0, false
}
