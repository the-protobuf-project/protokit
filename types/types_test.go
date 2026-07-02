package types

import "testing"

// TestRelationalizable covers the message-classification rule that decides
// whether a message-typed field becomes a related table (PK + FK) or keeps a
// scalar / JSON mapping. Well-known scalar types and the freeform google.protobuf
// wrappers stay; every other message — imported value types and user-defined
// nested messages alike — is relationalized.
func TestRelationalizable(t *testing.T) {
	cases := map[string]bool{
		// Native single-column mappings stay scalar (not a table).
		"google.protobuf.Timestamp":   false,
		"google.protobuf.Duration":    false,
		"google.protobuf.Int64Value":  false,
		"google.protobuf.StringValue": false,
		"google.protobuf.FieldMask":   false,
		"google.type.Date":            false,
		"google.type.LatLng":          false,
		// Freeform / type-erased wrappers stay a single JSON column (not a table).
		"google.protobuf.Struct":    false,
		"google.protobuf.Value":     false,
		"google.protobuf.ListValue": false,
		"google.protobuf.Any":       false,
		"google.protobuf.Empty":     false,
		// Imported value types relationalize into their own table.
		"google.type.Money":         true,
		"google.type.PostalAddress": true,
		"google.type.PhoneNumber":   true,
		// User-defined nested messages relationalize too.
		"example.v1.CustomMessage": true,
	}
	for in, want := range cases {
		if got := Relationalizable(in); got != want {
			t.Errorf("Relationalizable(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseProvider(t *testing.T) {
	for _, s := range []string{"", "postgres", "postgresql"} {
		if p, err := ParseProvider(s); err != nil || p != Postgres {
			t.Errorf("ParseProvider(%q) = (%v, %v), want Postgres", s, p, err)
		}
	}
	for _, s := range []string{"mongodb", "mongo"} {
		if p, err := ParseProvider(s); err != nil || p != MongoDB {
			t.Errorf("ParseProvider(%q) = (%v, %v), want MongoDB", s, p, err)
		}
	}
	for _, s := range []string{"evm", "ethereum"} {
		if p, err := ParseProvider(s); err != nil || p != EVM {
			t.Errorf("ParseProvider(%q) = (%v, %v), want EVM", s, p, err)
		}
	}
	if _, err := ParseProvider("mysql"); err == nil {
		t.Error("ParseProvider(mysql): want error, got nil")
	}
}
