package protokit_test

// document_test.go builds testdata/document under a layout that names the
// vocab.v1 messages documents, and checks that a field of such a type is one
// JSON column -- single or repeated -- and that no table is made for it or for
// anything nested in it, while a message of the resource's own package is still
// normalized.

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/golden"
	"github.com/the-protobuf-project/protokit/schema"
)

// vocabDocuments stores every vocab.v1 message as a document.
type vocabDocuments struct{}

func (vocabDocuments) ResolveDatasource(string) (string, string, bool, bool) {
	return "", "", false, false
}
func (vocabDocuments) DedupeSchemaTable() bool { return false }
func (vocabDocuments) Document(fullName string) bool {
	return strings.HasPrefix(fullName, "vocab.v1.")
}

func TestDocumentFields(t *testing.T) {
	p, err := protogen.Options{}.New(golden.BuildRequest(t, "testdata/document"))
	if err != nil {
		t.Fatalf("protogen: %v", err)
	}
	ir, err := protokit.Build(p, protokit.Options{}, []schema.FacetReader{oneSchemaReader{}}, vocabDocuments{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	tables := map[string]*schema.Table{}
	for _, db := range ir.Databases {
		for _, s := range db.Schemas {
			for _, tb := range s.Tables {
				tables[tb.ProtoMessage] = tb
			}
		}
	}
	for _, msg := range []string{"vocab.v1.Stay", "vocab.v1.Guest", "vocab.v1.Address"} {
		if _, ok := tables[msg]; ok {
			t.Errorf("%s: made a table; a document type must not be", msg)
		}
	}
	if _, ok := tables["stay.v1.Note"]; !ok {
		t.Error("stay.v1.Note: no table; a message of the resource's own package is still normalized")
	}
	b := tables["stay.v1.Booking"]
	if b == nil {
		t.Fatal("no Booking table")
	}
	cols := map[string]*schema.Column{}
	for _, c := range b.Columns {
		cols[c.Name] = c
	}
	for name, list := range map[string]bool{"stay": false, "guests": true} {
		c := cols[name]
		if c == nil {
			t.Errorf("Booking.%s: no column", name)
			continue
		}
		if !c.Document || c.Type != schema.TypeJSON || c.List != list {
			t.Errorf("Booking.%s: document %v type %v list %v, want a JSON document, list %v", name, c.Document, c.Type, c.List, list)
		}
	}
	if c := cols["state"]; c == nil || c.Document {
		t.Errorf("Booking.state: %+v, want an ordinary column", c)
	}
	if _, ok := cols["stay_id"]; ok {
		t.Error("Booking.stay_id: a document field must not leave a foreign key")
	}
}
