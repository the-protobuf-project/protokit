package protokit_test

// nestedmodel_test.go builds testdata/nestedmodel — a resource whose nested
// messages share simple names with a top-level message and with each other, in
// one schema — and checks that every distinct message becomes a model and a
// table of its own.

import (
	"testing"

	"google.golang.org/protobuf/compiler/protogen"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/golden"
	"github.com/the-protobuf-project/protokit/schema"
)

func TestNestedMessagesSharingASimpleName(t *testing.T) {
	p, err := protogen.Options{}.New(golden.BuildRequest(t, "testdata/nestedmodel"))
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
	want := map[string]struct{ model, table string }{
		"hotel.v1.Hotel":                         {"Hotel", "hotels"},
		"hotel.v1.Address":                       {"Address", "address"},
		"hotel.v1.Hotel.Address":                 {"HotelAddress", "hotel_address"},
		"hotel.v1.Tax":                           {"Tax", "tax"},
		"hotel.v1.Taxes":                         {"Taxes", "taxes"},
		"hotel.v1.Hotel.Rule":                    {"Rule", "rules"},
		"hotel.v1.Hotel.Images":                  {"Images", "images"},
		"hotel.v1.Hotel.Videos":                  {"Videos", "videos"},
		"hotel.v1.Hotel.Images.Item":             {"HotelImagesItem", "hotel_images_items"},
		"hotel.v1.Hotel.Videos.Item":             {"HotelVideosItem", "hotel_videos_items"},
		"hotel.v1.Hotel.Images.Item.Description": {"HotelImagesItemDescription", "hotel_images_item_descriptions"},
		"hotel.v1.Hotel.Videos.Item.Description": {"HotelVideosItemDescription", "hotel_videos_item_descriptions"},
	}
	for _, e := range ir.Databases[0].Schemas[0].Enums {
		if e.ProtoName == "hotel.v1.Hotel.Rule.Rule" && (e.LocalName != "HotelRuleRule" || e.LocalSQLName != "hotel_rule_rule") {
			t.Errorf("enum %s: %q/%q, want HotelRuleRule and hotel_rule_rule", e.ProtoName, e.LocalName, e.LocalSQLName)
		}
	}
	got := map[string]*schema.Table{}
	for _, tb := range ir.Databases[0].Schemas[0].Tables {
		got[tb.ProtoMessage] = tb
	}
	for msg, w := range want {
		tb, ok := got[msg]
		if !ok {
			t.Errorf("%s: no table", msg)
			continue
		}
		if tb.LocalName != w.model || tb.ModelName != w.model || tb.Name != w.table {
			t.Errorf("%s: model %q/%q table %q, want %q and %q", msg, tb.LocalName, tb.ModelName, tb.Name, w.model, w.table)
		}
	}
}
