package protokit

// column.go maps a single proto field to a *schema.Column and builds the IR
// enum a field references. Type inference proper lives in the types package;
// here we apply the neutral column structure a StructureReader supplied (name,
// skip), field_behavior, and the AIP enum defaulting rules. Generator rendering
// overrides (a store.v1.column type/unique/index/default) are applied later by
// that generator's enrichment pass, not here.

import (
	"strconv"
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/the-protobuf-project/protokit/naming"
	"github.com/the-protobuf-project/protokit/schema"
	"github.com/the-protobuf-project/protokit/types"
)

// buildColumn maps one proto field to a *schema.Column.
// Returns nil when the backend's column option marks the field skipped.
func (ctx *buildCtx) buildColumn(s *schema.Schema, f *protogen.Field) *schema.Column {
	cs := ctx.columnOf(f.Desc)
	if cs.Skip {
		return nil
	}
	col := &schema.Column{
		Name:    colName(f, cs),
		Comment: cleanComment(f.Comments.Leading),
		Type:    types.ClassifyField(f),
		List:    f.Desc.IsList(),
		Source:  f.Desc, // provenance handle for a generator's enrichment pass
		Node:    schema.NodeIDOfField(f.Desc),
	}
	// col.Type (set above) is the neutral type each generator projects to its own
	// type system; protokit sets no backend SQLType. An enum field instead carries
	// schema.Column.Enum, and each generator renders its own native enum reference.
	if f.Enum != nil && !f.Desc.IsList() {
		col.Enum = ctx.enumOf(s, f.Enum)
	}
	for _, b := range fieldBehaviors(f) {
		switch b {
		case annotations.FieldBehavior_REQUIRED:
			col.NotNull = true
		case annotations.FieldBehavior_IDENTIFIER:
			col.PrimaryKey, col.NotNull = true, true
		}
	}
	col.Optional = !col.NotNull
	// With the UNSPECIFIED sentinel dropped, a required enum column has no implicit
	// "unset" value, so default it to the first declared variant — mirroring the
	// hand-written `status … @default(ACTIVE)` pattern. An explicit default from
	// the generator's column option (applied in Backend.Enrich) always wins.
	if col.Enum != nil && col.NotNull && col.Default == "" && len(col.Enum.Values) > 0 {
		col.Default = col.Enum.Values[0].Name
	}
	return col
}

// enumOf returns the IR enum for e within schema s, building it on first use.
// Enums are keyed by their full proto name, as oneof discriminators are, so two
// distinct enums sharing a simple name (Webhook.State and Delivery.State) stay
// two enums; qualifyLocalEnums then gives them distinct names.
func (ctx *buildCtx) enumOf(s *schema.Schema, e *protogen.Enum) *schema.Enum {
	full := string(e.Desc.FullName())
	for _, ex := range s.Enums {
		if ex.ProtoName == full {
			return ex
		}
	}
	name := string(e.Desc.Name())
	en := &schema.Enum{
		Name:         name,
		SQLName:      naming.SnakeCase(name),
		LocalName:    name,
		LocalSQLName: naming.SnakeCase(name),
		ProtoName:    full,
		PgSchema:     s.Name,
		Comment:      cleanComment(e.Comments.Leading),
		SourceFile:   sourceFileBase(e.Desc.ParentFile().Path()),
		SourceProto:  e.Desc.ParentFile().Path(),
		SourceDir:    protoDirNoVersion(e.Desc.ParentFile().Path()),
	}
	for _, v := range e.Values {
		full := string(v.Desc.Name())
		// AIP-126: the zero value is the *_UNSPECIFIED "not set" sentinel, never a
		// real state. Drop it so the generated enum mirrors a hand-written one — no
		// storable UNSPECIFIED row — and so a required column can default to the
		// first genuine value (see buildColumn).
		if v.Desc.Number() == 0 && strings.HasSuffix(strings.ToUpper(full), "UNSPECIFIED") {
			continue
		}
		// MapName is the SCREAMING_SNAKE stored value (read by every target). Name
		// is the rendered identifier and must start with a letter (a Prisma enum
		// value rule): when stripping the enum prefix leaves a digit-leading value
		// (ASPECT_RATIO_3_4 → "3_4"), fall back to the full proto value name, which
		// is letter-leading. MapName keeps the short form for the DB ("3_4").
		mapName := naming.ScreamingSnake(naming.EnumValueName(name, full))
		valName := mapName
		if !startsWithLetter(valName) {
			valName = naming.ScreamingSnake(full)
		}
		if !startsWithLetter(valName) {
			valName = "V" + valName // rare: a proto value that itself isn't letter-leading
		}
		en.Values = append(en.Values, &schema.EnumValue{
			Name:    valName,
			MapName: mapName,
			Comment: cleanComment(v.Comments.Leading),
		})
	}
	s.Enums = append(s.Enums, en)
	ctx.enumSrc[en] = e
	ctx.qualifyLocalEnums(s, name)
	return en
}

// qualifyLocalEnums gives distinct names to the distinct proto enums of schema s
// that share the simple name. Schema-namespaced targets render LocalName bare
// (one Go package, one SQL schema), so two "State" enums would otherwise declare
// one type twice. A nested enum takes the names of the messages enclosing it
// ("Webhook.State" → "WebhookState", protoc-gen-go's Go name without the
// underscore); a top-level one keeps its simple name. Two that still collide —
// top-level enums of packages merged into one schema — gain their package's
// domain ("shop.cart.v1" → "CartState"), then a number as a last resort.
//
// Every participant is renamed, not only the later one, so an enum's name
// doesn't hang on which field the build met first. A schema with one enum of
// the name is untouched. Enum values keep their prefix stripped by the simple
// name ("STATE_ACTIVE" → "ACTIVE"), which is the proto's own prefix.
func (ctx *buildCtx) qualifyLocalEnums(s *schema.Schema, name string) {
	var group []*schema.Enum
	for _, ex := range s.Enums {
		if src := ctx.enumSrc[ex]; src != nil && string(src.Desc.Name()) == name {
			group = append(group, ex)
		}
	}
	if len(group) < 2 {
		return
	}
	names := make([]string, len(group))
	count := map[string]int{}
	for i, ex := range group {
		names[i] = scopedEnumName(ctx.enumSrc[ex].Desc)
		count[names[i]]++
	}
	for i, ex := range group {
		if count[names[i]] > 1 {
			pkg := strings.ReplaceAll(string(ctx.enumSrc[ex].Desc.ParentFile().Package()), ".", "_")
			names[i] = naming.PascalGo(naming.SchemaDomain(pkg)) + names[i]
		}
	}
	used := map[string]bool{}
	for i, ex := range group {
		q := names[i]
		for n := 2; used[q]; n++ {
			q = names[i] + strconv.Itoa(n)
		}
		used[q] = true
		ex.Name, ex.LocalName = q, q
		ex.SQLName, ex.LocalSQLName = naming.SnakeCase(q), naming.SnakeCase(q)
	}
}

// scopedEnumName is e's simple name prefixed with the names of the messages
// enclosing it, outermost first: "Outer.Inner.State" → "OuterInnerState".
func scopedEnumName(e protoreflect.EnumDescriptor) string {
	name := string(e.Name())
	for p := e.Parent(); p != nil; p = p.Parent() {
		m, ok := p.(protoreflect.MessageDescriptor)
		if !ok {
			break
		}
		name = string(m.Name()) + name
	}
	return name
}

// startsWithLetter reports whether s begins with an ASCII letter — the leading
// character Prisma requires for an enum value identifier.
func startsWithLetter(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}
