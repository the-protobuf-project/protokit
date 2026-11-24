package apispec

import "strings"

// Scalar is a semantic leaf type. Frontends map their own scalar vocabulary
// (OpenAPI type+format, GraphQL built-in and well-known custom scalars) onto
// this set; anything unrecognized stays ScalarString/ScalarJSON with the
// original spelling kept in TypeRef.Format.
type Scalar string

const (
	ScalarNone      Scalar = ""
	ScalarString    Scalar = "string"
	ScalarBool      Scalar = "bool"
	ScalarInt32     Scalar = "int32"
	ScalarInt64     Scalar = "int64"
	ScalarUint32    Scalar = "uint32"
	ScalarUint64    Scalar = "uint64"
	ScalarFloat     Scalar = "float"
	ScalarDouble    Scalar = "double"
	ScalarDecimal   Scalar = "decimal"   // arbitrary-precision number as text
	ScalarBytes     Scalar = "bytes"     // base64 / binary
	ScalarTimestamp Scalar = "timestamp" // date-time
	ScalarDate      Scalar = "date"
	ScalarTime      Scalar = "time" // time of day
	ScalarDuration  Scalar = "duration"
	ScalarUUID      Scalar = "uuid"
	ScalarID        Scalar = "id" // opaque identifier (GraphQL ID)
	ScalarEmail     Scalar = "email"
	ScalarURI       Scalar = "uri"
	ScalarHostname  Scalar = "hostname"
	ScalarIPv4      Scalar = "ipv4"
	ScalarIPv6      Scalar = "ipv6"
	ScalarJSON      Scalar = "json" // arbitrary JSON value
	ScalarMoney     Scalar = "money"
	ScalarPhone     Scalar = "phone"
	ScalarCurrency  Scalar = "currency" // ISO 4217 code
	ScalarCountry   Scalar = "country"  // ISO 3166 code
	ScalarLanguage  Scalar = "language" // BCP-47
	ScalarTimeZone  Scalar = "timezone" // IANA zone
)

// ScalarFromFormat maps an OpenAPI type/format pair onto a Scalar.
func ScalarFromFormat(typ, format string) Scalar {
	f := strings.ToLower(format)
	switch typ {
	case "boolean":
		return ScalarBool
	case "integer":
		switch f {
		case "int64", "long":
			return ScalarInt64
		case "uint32":
			return ScalarUint32
		case "uint64":
			return ScalarUint64
		case "int32", "int", "":
			return ScalarInt32
		}
		return ScalarInt64
	case "number":
		switch f {
		case "float":
			return ScalarFloat
		case "decimal", "big-decimal":
			return ScalarDecimal
		case "int32":
			return ScalarInt32
		case "int64":
			return ScalarInt64
		}
		return ScalarDouble
	case "string", "":
		if s := scalarFromStringFormat(f); s != ScalarNone {
			return s
		}
		if typ == "" {
			return ScalarNone
		}
		return ScalarString
	}
	return ScalarNone
}

func scalarFromStringFormat(f string) Scalar {
	switch f {
	case "date-time", "datetime", "timestamp", "timestamptz", "rfc3339":
		return ScalarTimestamp
	case "date", "full-date":
		return ScalarDate
	case "time", "partial-time", "timetz":
		return ScalarTime
	case "duration":
		return ScalarDuration
	case "uuid", "guid":
		return ScalarUUID
	case "email", "idn-email":
		return ScalarEmail
	case "uri", "url", "iri", "uri-reference", "iri-reference":
		return ScalarURI
	case "hostname", "idn-hostname":
		return ScalarHostname
	case "ipv4":
		return ScalarIPv4
	case "ipv6":
		return ScalarIPv6
	case "byte", "binary", "base64":
		return ScalarBytes
	case "int64", "long":
		return ScalarInt64 // string-encoded int64 (common in Google APIs)
	case "decimal", "big-decimal":
		return ScalarDecimal
	case "phone", "tel":
		return ScalarPhone
	}
	return ScalarNone
}

// ScalarFromName maps a GraphQL scalar name (built-in or a well-known custom
// scalar such as Hasura's timestamptz or graphql-scalars' DateTime) onto a
// Scalar. Unknown names return ScalarNone.
func ScalarFromName(name string) Scalar {
	switch name {
	case "String":
		return ScalarString
	case "Boolean":
		return ScalarBool
	case "Int":
		return ScalarInt32
	case "Float":
		return ScalarDouble
	case "ID":
		return ScalarID
	}
	n := strings.ToLower(strings.ReplaceAll(name, "_", ""))
	switch n {
	case "bigint", "int8", "long", "int64", "safeint":
		return ScalarInt64
	case "int4", "int2", "smallint", "integer", "int32":
		return ScalarInt32
	case "float8", "float4", "real", "doubleprecision":
		return ScalarDouble
	case "numeric", "decimal", "bigdecimal", "bigfloat":
		return ScalarDecimal
	case "timestamptz", "timestamp", "datetime", "instant", "datetimeiso", "localdatetime", "datetimeutc":
		return ScalarTimestamp
	case "date", "localdate":
		return ScalarDate
	case "time", "timetz", "localtime":
		return ScalarTime
	case "interval", "duration":
		return ScalarDuration
	case "uuid", "guid":
		return ScalarUUID
	case "json", "jsonb", "jsonobject", "jsonstring", "object", "any", "map":
		return ScalarJSON
	case "bytea", "bytes", "base64", "binary", "upload", "file":
		return ScalarBytes
	case "emailaddress", "email":
		return ScalarEmail
	case "url", "uri":
		return ScalarURI
	case "money", "currencyamount":
		return ScalarMoney
	case "currency", "currencycode":
		return ScalarCurrency
	case "countrycode":
		return ScalarCountry
	case "locale", "language", "languagecode":
		return ScalarLanguage
	case "timezone", "timezonename":
		return ScalarTimeZone
	case "phonenumber", "phone":
		return ScalarPhone
	case "ipv4":
		return ScalarIPv4
	case "ipv6", "ip":
		return ScalarIPv6
	case "hostname":
		return ScalarHostname
	case "citext", "text", "bpchar", "varchar", "char", "name":
		return ScalarString
	case "bool":
		return ScalarBool
	}
	return ScalarNone
}
