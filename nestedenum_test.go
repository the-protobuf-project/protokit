package protokit_test

// nestedenum_test.go builds testdata/nestedenum — two resources each with a
// nested enum State, and two packages each with a top-level enum Phase, all in
// one schema — and checks that every distinct enum keeps its own values under a
// name of its own.

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/golden"
	"github.com/the-protobuf-project/protokit/schema"
)

// oneSchemaReader routes every package into one database and one schema.
type oneSchemaReader struct{}

func (oneSchemaReader) Key() string                                       { return "oneschema" }
func (oneSchemaReader) ReadFile(protoreflect.FileDescriptor) (any, error) { return nil, nil }
func (oneSchemaReader) ReadMessage(protoreflect.MessageDescriptor) (any, error) {
	return nil, nil
}
func (oneSchemaReader) ReadField(protoreflect.FieldDescriptor) (any, error) { return nil, nil }

func (oneSchemaReader) ReadDatasource(protoreflect.FileDescriptor) schema.Datasource {
	return schema.Datasource{Database: "hooks", Schema: "hooks", Provider: "postgres"}
}

func (oneSchemaReader) ReadTable(protoreflect.MessageDescriptor) schema.TableStructure {
	return schema.TableStructure{}
}

func (oneSchemaReader) ReadColumn(protoreflect.FieldDescriptor) schema.ColumnStructure {
	return schema.ColumnStructure{}
}

func TestNestedEnumsSharingASimpleName(t *testing.T) {
	p, err := protogen.Options{}.New(golden.BuildRequest(t, "testdata/nestedenum"))
	if err != nil {
		t.Fatalf("protogen: %v", err)
	}
	ir, err := protokit.Build(p, protokit.Options{}, []schema.FacetReader{oneSchemaReader{}}, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(ir.Databases) != 1 || len(ir.Databases[0].Schemas) != 1 {
		t.Fatalf("want one database with one schema, got %+v", ir.Databases)
	}
	s := ir.Databases[0].Schemas[0]

	want := map[string]struct {
		local, sql, values string
	}{
		"hooks.v1.Webhook.State":  {"WebhookState", "webhook_state", "ACTIVE PAUSED"},
		"hooks.v1.Delivery.State": {"DeliveryState", "delivery_state", "PENDING SUCCEEDED FAILED"},
		"hooks.v1.Phase":          {"HooksPhase", "hooks_phase", "EARLY"},
		"relay.v1.Phase":          {"RelayPhase", "relay_phase", "LATE"},
	}
	if len(s.Enums) != len(want) {
		t.Errorf("got %d enums, want %d", len(s.Enums), len(want))
	}
	for _, e := range s.Enums {
		w, ok := want[e.ProtoName]
		if !ok {
			t.Errorf("unexpected enum %q", e.ProtoName)
			continue
		}
		var vals []string
		for _, v := range e.Values {
			vals = append(vals, v.MapName)
		}
		if e.LocalName != w.local || e.Name != w.local || e.LocalSQLName != w.sql || e.SQLName != w.sql {
			t.Errorf("%s: names %q/%q/%q/%q, want %q and %q", e.ProtoName, e.Name, e.LocalName, e.SQLName, e.LocalSQLName, w.local, w.sql)
		}
		if got := strings.Join(vals, " "); got != w.values {
			t.Errorf("%s: values %q, want %q", e.ProtoName, got, w.values)
		}
	}

	cols := map[string]string{}
	for _, tb := range s.Tables {
		for _, c := range tb.Columns {
			if c.Enum != nil {
				cols[tb.Name+"."+c.Name] = c.Enum.ProtoName
			}
		}
	}
	for col, enum := range map[string]string{
		"webhooks.state":           "hooks.v1.Webhook.State",
		"deliveries.state":         "hooks.v1.Delivery.State",
		"deliveries.webhook_state": "hooks.v1.Webhook.State",
		"deliveries.phase":         "hooks.v1.Phase",
		"relays.phase":             "relay.v1.Phase",
	} {
		if cols[col] != enum {
			t.Errorf("column %s references %q, want %q", col, cols[col], enum)
		}
	}
}
