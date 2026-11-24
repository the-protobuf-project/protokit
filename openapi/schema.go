package openapi

import (
	"fmt"
	"path"
	"strings"

	"github.com/the-protobuf-project/protokit/apispec"
	"github.com/the-protobuf-project/protokit/naming"
)

// fieldInfo is what a schema says about the slot it sits in (as opposed to
// the type it describes): nullability, access, docs, constraints.
type fieldInfo struct {
	Description string
	Nullable    bool
	ReadOnly    bool
	WriteOnly   bool
	Deprecated  bool
	Default     any
	Example     any
	Constraints apispec.Constraints
	Extensions  map[string]any
}

// schemaType converts a schema (possibly a $ref) found at loc into a TypeRef.
// hint names the type if the schema is an inline object/enum/union that must
// be lifted. group is the tag the schema was reached through, if any.
func (c *converter) schemaType(loc string, v any, hint string) (apispec.TypeRef, fieldInfo, error) {
	m := obj(v)
	if m == nil {
		// "true" / {} / missing: any JSON value.
		return apispec.TypeRef{Scalar: apispec.ScalarJSON}, fieldInfo{}, nil
	}
	info := c.slotInfo(m)

	if s, ok := m["$ref"].(string); ok {
		r, err := resolveRef(loc, s)
		if err != nil {
			return apispec.TypeRef{}, info, err
		}
		name, err := c.namedRef(r)
		if err != nil {
			return apispec.TypeRef{}, info, err
		}
		t := c.spec.Types[name]
		// A named alias of a scalar inlines to the scalar it stands for, keeping
		// the alias's constraints and format on the slot.
		if t != nil && t.Kind == apispec.KindScalar && t.Scalar != nil && t.Scalar.Named == "" && t.Scalar.List == nil && t.Scalar.Map == nil {
			ai := c.aliasInfo[name]
			merged := mergeInfo(ai, info)
			return *t.Scalar, merged, nil
		}
		return apispec.TypeRef{Named: name}, info, nil
	}

	typ, nullable := schemaTypeName(m)
	if nullable {
		info.Nullable = true
	}

	// Composition first: allOf / oneOf / anyOf.
	if all := list(m["allOf"]); len(all) > 0 {
		return c.allOf(loc, m, all, hint, info)
	}
	for _, k := range []string{"oneOf", "anyOf"} {
		if alts := list(m[k]); len(alts) > 0 {
			return c.union(loc, m, alts, hint, info)
		}
	}

	if enum := list(m["enum"]); len(enum) > 0 {
		vals := enumValues(m, enum)
		if typ == "string" || typ == "" || typ == "integer" || typ == "number" {
			// A single-valued enum is a const, not a type.
			if len(vals) == 1 && typ != "" {
				info.Constraints.Const = vals[0].Value
				return apispec.TypeRef{Scalar: apispec.ScalarFromFormat(typ, str(m, "format")), Format: str(m, "format")}, info, nil
			}
			if hasNullValue(enum) {
				info.Nullable = true
			}
			name := c.lift(hint)
			c.spec.Types[name] = &apispec.Type{
				Name: name, Kind: apispec.KindEnum, Description: str(m, "description"),
				Values: vals, Synthetic: true, Extensions: extensions(m), Groups: c.groupList(),
			}
			return apispec.TypeRef{Named: name}, info, nil
		}
	}

	switch typ {
	case "array":
		items := m["items"]
		if items == nil {
			items = m["prefixItems"] // 3.1 tuple: take the first slot type
			if l := list(items); len(l) > 0 {
				items = l[0]
			}
		}
		elem, ei, err := c.schemaType(loc, items, hint+"Item")
		if err != nil {
			return apispec.TypeRef{}, info, err
		}
		return apispec.TypeRef{List: &elem, ElemNullable: ei.Nullable}, info, nil
	case "object", "":
		props := obj(m["properties"])
		ap, hasAP := m["additionalProperties"]
		if len(props) == 0 && typ == "object" || len(props) == 0 && hasAP {
			if hasAP {
				if b, isBool := ap.(bool); !isBool || b {
					val := apispec.TypeRef{Scalar: apispec.ScalarJSON}
					var vi fieldInfo
					if !isBool {
						var err error
						val, vi, err = c.schemaType(loc, ap, hint+"Value")
						if err != nil {
							return apispec.TypeRef{}, info, err
						}
					}
					return apispec.TypeRef{Map: &val, ElemNullable: vi.Nullable}, info, nil
				}
			}
			if typ == "object" {
				return apispec.TypeRef{Scalar: apispec.ScalarJSON}, info, nil // free-form object
			}
		}
		if len(props) > 0 {
			name := c.lift(hint)
			t := &apispec.Type{Name: name, Kind: apispec.KindObject, Synthetic: true, Groups: c.groupList()}
			c.spec.Types[name] = t
			if err := c.fillObject(loc, t, m); err != nil {
				return apispec.TypeRef{}, info, err
			}
			return apispec.TypeRef{Named: name}, info, nil
		}
		if typ == "" {
			return apispec.TypeRef{Scalar: apispec.ScalarJSON}, info, nil
		}
	}

	sc := apispec.ScalarFromFormat(typ, str(m, "format"))
	if sc == apispec.ScalarNone {
		sc = apispec.ScalarJSON
	}
	if typ == "string" && (str(m, "contentEncoding") == "base64" || str(m, "contentMediaType") == "application/octet-stream") {
		sc = apispec.ScalarBytes
	}
	return apispec.TypeRef{Scalar: sc, Format: str(m, "format")}, info, nil
}

// slotInfo reads the slot-level keywords of a schema.
func (c *converter) slotInfo(m map[string]any) fieldInfo {
	fi := fieldInfo{
		Description: firstNonEmpty(str(m, "description"), proseTitle(str(m, "title"))),
		Nullable:    boolean(m, "nullable") || boolean(m, "x-nullable"),
		ReadOnly:    boolean(m, "readOnly"),
		WriteOnly:   boolean(m, "writeOnly"),
		Deprecated:  boolean(m, "deprecated"),
		Default:     clean(m["default"]),
		Example:     clean(m["example"]),
		Extensions:  extensions(m),
	}
	if ex := list(m["examples"]); fi.Example == nil && len(ex) > 0 {
		fi.Example = clean(ex[0])
	}
	k := &fi.Constraints
	k.MinLength, k.MaxLength = integer(m, "minLength"), integer(m, "maxLength")
	k.Pattern = str(m, "pattern")
	k.Minimum, k.Maximum = num(m, "minimum"), num(m, "maximum")
	// 3.0: exclusiveMinimum is a bool; 3.1: it is the bound itself.
	if b, ok := m["exclusiveMinimum"].(bool); ok {
		k.ExclusiveMinimum = b
	} else if n := num(m, "exclusiveMinimum"); n != nil {
		k.Minimum, k.ExclusiveMinimum = n, true
	}
	if b, ok := m["exclusiveMaximum"].(bool); ok {
		k.ExclusiveMaximum = b
	} else if n := num(m, "exclusiveMaximum"); n != nil {
		k.Maximum, k.ExclusiveMaximum = n, true
	}
	k.MultipleOf = num(m, "multipleOf")
	k.MinItems, k.MaxItems = integer(m, "minItems"), integer(m, "maxItems")
	k.UniqueItems = boolean(m, "uniqueItems")
	k.MinProperties, k.MaxProperties = integer(m, "minProperties"), integer(m, "maxProperties")
	if v, ok := m["const"]; ok {
		k.Const = clean(v)
	}
	return fi
}

// schemaTypeName returns the schema's primary type and whether "null" is allowed.
func schemaTypeName(m map[string]any) (string, bool) {
	switch t := m["type"].(type) {
	case string:
		if t == "null" {
			return "", true
		}
		return t, false
	case []any:
		var typ string
		var null bool
		for _, e := range t {
			s, _ := e.(string)
			if s == "null" {
				null = true
			} else if typ == "" {
				typ = s
			}
		}
		return typ, null
	}
	// No type keyword: infer from shape.
	switch {
	case m["properties"] != nil || m["additionalProperties"] != nil:
		return "object", false
	case m["items"] != nil:
		return "array", false
	}
	return "", false
}

func enumValues(m map[string]any, enum []any) []*apispec.EnumValue {
	names := strs(firstOf(m, "x-enum-varnames", "x-enumNames"))
	descs := strs(firstOf(m, "x-enum-descriptions", "x-enumDescriptions"))
	var out []*apispec.EnumValue
	for i, e := range enum {
		if e == nil {
			continue
		}
		ev := &apispec.EnumValue{Value: e}
		if i < len(names) {
			ev.Name = names[i]
		}
		if i < len(descs) {
			ev.Description = descs[i]
		}
		out = append(out, ev)
	}
	// oneOf-of-const style descriptions are handled in union().
	return out
}

func hasNullValue(enum []any) bool {
	for _, e := range enum {
		if e == nil {
			return true
		}
	}
	return false
}

func firstOf(m map[string]any, ks ...string) any {
	for _, k := range ks {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

// fillObject copies properties into t.
func (c *converter) fillObject(loc string, t *apispec.Type, m map[string]any) error {
	if t.Description == "" {
		t.Description = firstNonEmpty(str(m, "description"), proseTitle(str(m, "title")))
	}
	t.Deprecated = t.Deprecated || boolean(m, "deprecated")
	if t.Example == nil {
		t.Example = clean(m["example"])
	}
	if ex := extensions(m); ex != nil {
		if t.Extensions == nil {
			t.Extensions = map[string]any{}
		}
		for k, v := range ex {
			t.Extensions[k] = v
		}
	}
	required := map[string]bool{}
	for _, r := range strs(m["required"]) {
		required[r] = true
	}
	props := obj(m["properties"])
	for _, pn := range keys(props) {
		if pn == orderKey {
			continue
		}
		if t.Field(pn) != nil {
			continue // allOf: the first definition wins
		}
		ref, fi, err := c.schemaType(loc, props[pn], t.Name+naming.PascalGo(pn))
		if err != nil {
			return fmt.Errorf("%s.%s: %w", t.Name, pn, err)
		}
		t.Fields = append(t.Fields, &apispec.Field{
			Name: pn, Description: fi.Description, Type: ref,
			Required: required[pn], Nullable: fi.Nullable,
			ReadOnly: fi.ReadOnly, WriteOnly: fi.WriteOnly, Deprecated: fi.Deprecated,
			Default: fi.Default, Example: fi.Example, Constraints: fi.Constraints,
			Extensions: fi.Extensions,
		})
	}
	// "required" may name properties declared by an allOf sibling.
	for _, f := range t.Fields {
		if required[f.Name] {
			f.Required = true
		}
	}
	if d := obj(m["discriminator"]); d != nil {
		t.Discriminator = c.discriminator(loc, d)
	}
	return nil
}

// allOf merges every member into one object, recording $ref members as parents.
// allOf: [$ref] with only slot keywords alongside is just that reference.
func (c *converter) allOf(loc string, m map[string]any, all []any, hint string, info fieldInfo) (apispec.TypeRef, fieldInfo, error) {
	if len(all) == 1 && obj(m["properties"]) == nil {
		ref, ri, err := c.schemaType(loc, all[0], hint)
		return ref, mergeInfo(ri, info), err
	}
	name := c.lift(hint)
	t := &apispec.Type{Name: name, Kind: apispec.KindObject, Synthetic: true, Groups: c.groupList()}
	c.spec.Types[name] = t
	if err := c.mergeAllOf(loc, t, m, all); err != nil {
		return apispec.TypeRef{}, info, err
	}
	return apispec.TypeRef{Named: name}, info, nil
}

func (c *converter) mergeAllOf(loc string, t *apispec.Type, m map[string]any, all []any) error {
	if err := c.fillObject(loc, t, m); err != nil {
		return err
	}
	for i, part := range all {
		pm := obj(part)
		ploc := loc
		if s, ok := pm["$ref"].(string); ok {
			r, err := resolveRef(loc, s)
			if err != nil {
				return err
			}
			pname, err := c.namedRef(r)
			if err != nil {
				return err
			}
			t.Implements = append(t.Implements, pname)
			target, tloc, err := c.ld.deref(loc, part)
			if err != nil {
				return err
			}
			pm, ploc = target, tloc
		}
		if pm == nil {
			continue
		}
		if sub := list(pm["allOf"]); len(sub) > 0 {
			if err := c.mergeAllOf(ploc, t, pm, sub); err != nil {
				return err
			}
			continue
		}
		if obj(pm["properties"]) == nil && pm["oneOf"] == nil && pm["anyOf"] == nil {
			// a constraint-only member (required: [...]) still marks fields required
			req := map[string]bool{}
			for _, r := range strs(pm["required"]) {
				req[r] = true
			}
			for _, f := range t.Fields {
				if req[f.Name] {
					f.Required = true
				}
			}
			continue
		}
		if pm["oneOf"] != nil || pm["anyOf"] != nil {
			ref, _, err := c.schemaType(ploc, pm, fmt.Sprintf("%sVariant%d", t.Name, i+1))
			if err != nil {
				return err
			}
			t.Fields = append(t.Fields, &apispec.Field{Name: "variant", Type: ref})
			continue
		}
		if err := c.fillObject(ploc, t, pm); err != nil {
			return err
		}
	}
	return nil
}

// union handles oneOf/anyOf. [X, null] collapses to nullable X; enums written
// as oneOf-of-const become enums with per-value descriptions.
func (c *converter) union(loc string, m map[string]any, alts []any, hint string, info fieldInfo) (apispec.TypeRef, fieldInfo, error) {
	var nonNull []any
	for _, a := range alts {
		am := obj(a)
		if t, _ := schemaTypeName(am); t == "" && am != nil && am["$ref"] == nil && (am["type"] == "null" || isNullOnly(am)) {
			info.Nullable = true
			continue
		}
		nonNull = append(nonNull, a)
	}
	if len(nonNull) == 1 {
		ref, ri, err := c.schemaType(loc, nonNull[0], hint)
		ri.Nullable = ri.Nullable || info.Nullable
		return ref, mergeInfo(ri, info), err
	}
	// oneOf: [{const: a, description}, …] is an enum.
	if vals, ok := constEnum(nonNull); ok {
		name := c.lift(hint)
		c.spec.Types[name] = &apispec.Type{Name: name, Kind: apispec.KindEnum, Description: str(m, "description"), Values: vals, Synthetic: true, Groups: c.groupList()}
		return apispec.TypeRef{Named: name}, info, nil
	}
	// All scalar alternatives of the same family (string | integer): widen.
	if sc, ok := scalarWiden(nonNull); ok {
		return apispec.TypeRef{Scalar: sc}, info, nil
	}
	name := c.lift(hint)
	t := &apispec.Type{Name: name, Kind: apispec.KindUnion, Description: str(m, "description"), Synthetic: true, Groups: c.groupList()}
	c.spec.Types[name] = t
	for i, a := range nonNull {
		ref, _, err := c.schemaType(loc, a, fmt.Sprintf("%sOption%d", name, i+1))
		if err != nil {
			return apispec.TypeRef{}, info, err
		}
		if ref.Named == "" {
			// Scalar alternative: wrap so every variant is a named type.
			alias := c.lift(fmt.Sprintf("%sOption%d", name, i+1))
			r := ref
			c.spec.Types[alias] = &apispec.Type{Name: alias, Kind: apispec.KindScalar, Scalar: &r, Synthetic: true}
			ref = apispec.TypeRef{Named: alias}
		}
		t.Variants = append(t.Variants, ref.Named)
	}
	if d := obj(m["discriminator"]); d != nil {
		t.Discriminator = c.discriminator(loc, d)
	}
	return apispec.TypeRef{Named: name}, info, nil
}

func isNullOnly(m map[string]any) bool {
	if v, ok := m["enum"]; ok {
		l := list(v)
		return len(l) == 1 && l[0] == nil
	}
	return false
}

func constEnum(alts []any) ([]*apispec.EnumValue, bool) {
	var out []*apispec.EnumValue
	for _, a := range alts {
		m := obj(a)
		v, ok := m["const"]
		if !ok {
			if e := list(m["enum"]); len(e) == 1 {
				v, ok = e[0], true
			}
		}
		if !ok || m["$ref"] != nil {
			return nil, false
		}
		out = append(out, &apispec.EnumValue{Value: v, Description: firstNonEmpty(str(m, "description"), str(m, "title"))})
	}
	return out, len(out) > 0
}

func scalarWiden(alts []any) (apispec.Scalar, bool) {
	var kinds []string
	for _, a := range alts {
		m := obj(a)
		if m == nil || m["$ref"] != nil || m["properties"] != nil || m["enum"] != nil {
			return "", false
		}
		t, _ := schemaTypeName(m)
		switch t {
		case "string", "integer", "number", "boolean":
			kinds = append(kinds, t)
		default:
			return "", false
		}
	}
	allNum := true
	for _, k := range kinds {
		if k != "integer" && k != "number" {
			allNum = false
		}
	}
	if allNum {
		return apispec.ScalarDouble, true
	}
	// Mixed string/number/bool: the value travels as JSON.
	return apispec.ScalarJSON, true
}

func (c *converter) discriminator(loc string, d map[string]any) *apispec.Discriminator {
	out := &apispec.Discriminator{Property: str(d, "propertyName"), Mapping: map[string]string{}}
	mp := obj(d["mapping"])
	for _, k := range keys(mp) {
		if k == orderKey {
			continue
		}
		target, _ := mp[k].(string)
		if !strings.Contains(target, "#") && !strings.Contains(target, "/") {
			target = "#/components/schemas/" + target // bare schema name form
		}
		r, err := resolveRef(loc, target)
		if err != nil {
			continue
		}
		if n, err := c.namedRef(r); err == nil {
			out.Mapping[k] = n
		}
	}
	return out
}

func mergeInfo(base, over fieldInfo) fieldInfo {
	out := base
	if over.Description != "" {
		out.Description = over.Description
	}
	out.Nullable = base.Nullable || over.Nullable
	out.ReadOnly = base.ReadOnly || over.ReadOnly
	out.WriteOnly = base.WriteOnly || over.WriteOnly
	out.Deprecated = base.Deprecated || over.Deprecated
	if over.Default != nil {
		out.Default = over.Default
	}
	if over.Example != nil {
		out.Example = over.Example
	}
	if !over.Constraints.IsZero() {
		out.Constraints = over.Constraints
	}
	return out
}

// proseTitle keeps a schema title only when it reads as text: generators put
// machine identifiers there (Stripe's "pause_settings_bill_for_params").
func proseTitle(t string) string {
	if strings.ContainsAny(t, " ") {
		return t
	}
	return ""
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// lift returns a unique type name for an inline schema.
func (c *converter) lift(hint string) string {
	base := naming.PascalGo(hint)
	if base == "" {
		base = "Anonymous"
	}
	if c.pending[base] && c.spec.Types[base] == nil {
		delete(c.pending, base)
		return base
	}
	name := base
	for i := 2; c.spec.Types[name] != nil || c.reserved[name]; i++ {
		name = fmt.Sprintf("%s%d", base, i)
	}
	return name
}

// refName derives a type name from a reference: the last pointer token, or
// the file's base name for a whole-file reference.
func refName(r ref) string {
	if r.Pointer != "" {
		toks := strings.Split(strings.TrimSuffix(r.Pointer, "/"), "/")
		last := toks[len(toks)-1]
		last = strings.ReplaceAll(strings.ReplaceAll(last, "~1", "/"), "~0", "~")
		return last
	}
	b := path.Base(strings.ReplaceAll(r.Loc, "\\", "/"))
	return strings.TrimSuffix(b, path.Ext(b))
}
