package openapi

import (
	"fmt"
	"strings"

	"github.com/the-protobuf-project/protokit/apispec"
	"github.com/the-protobuf-project/protokit/naming"
)

var methods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace", "query"}

// paths converts a paths (or webhooks) object into operations.
func (c *converter) paths(paths map[string]any, kind apispec.OpKind) error {
	for _, p := range keys(paths) {
		if p == orderKey || strings.HasPrefix(p, "x-") {
			continue
		}
		item, loc, err := c.ld.deref(c.root, paths[p])
		if err != nil {
			return fmt.Errorf("openapi: path %s: %w", p, err)
		}
		if err := c.pathItem(loc, p, item, kind, ""); err != nil {
			return err
		}
	}
	return nil
}

func (c *converter) pathItem(loc, p string, item map[string]any, kind apispec.OpKind, idPrefix string) error {
	if item == nil {
		return nil
	}
	shared := list(item["parameters"])
	for _, method := range methods {
		om := obj(item[method])
		if om == nil {
			continue
		}
		op, err := c.operation(loc, p, method, om, shared, kind, idPrefix)
		if err != nil {
			return fmt.Errorf("openapi: %s %s: %w", strings.ToUpper(method), p, err)
		}
		c.spec.Operations = append(c.spec.Operations, op)

		// Callbacks: server-initiated requests, modeled as webhook operations.
		cbs := obj(om["callbacks"])
		for _, cbName := range keys(cbs) {
			if cbName == orderKey {
				continue
			}
			cb, cloc, err := c.ld.deref(loc, cbs[cbName])
			if err != nil {
				return err
			}
			for _, expr := range keys(cb) {
				if expr == orderKey {
					continue
				}
				ci, iloc, err := c.ld.deref(cloc, cb[expr])
				if err != nil {
					return err
				}
				// A callback whose path item leads back to one still being
				// converted would expand forever.
				key := fmt.Sprintf("%s|%p", iloc, ci)
				if c.callbacks[key] {
					return fmt.Errorf("openapi: callback %s %s: cycle back to a path item being converted", cbName, expr)
				}
				c.callbacks[key] = true
				err = c.pathItem(iloc, expr, ci, apispec.OpWebhook, op.ID+naming.PascalGo(cbName))
				delete(c.callbacks, key)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *converter) operation(loc, p, method string, om map[string]any, shared []any, kind apispec.OpKind, idPrefix string) (*apispec.Operation, error) {
	op := &apispec.Operation{
		ID:          str(om, "operationId"),
		Kind:        kind,
		Summary:     str(om, "summary"),
		Description: str(om, "description"),
		Groups:      strs(om["tags"]),
		Deprecated:  boolean(om, "deprecated"),
		HTTP:        &apispec.HTTPBinding{Method: strings.ToUpper(method), Path: p},
		Extensions:  extensions(om),
	}
	if op.ID == "" {
		op.ID = idPrefix + synthOperationID(method, p)
	} else if idPrefix != "" {
		op.ID = idPrefix + naming.PascalGo(op.ID)
	}
	op.ID = c.uniqueOpID(op.ID)
	for _, g := range op.Groups {
		c.spec.AddGroup(&apispec.Group{Name: g})
	}
	if sec, ok := om["security"]; ok {
		op.Security = securityRequirements(sec)
	}
	for _, alt := range c.spec.EffectiveSecurity(op) {
		for name := range alt {
			if c.spec.Security[name] == nil {
				return nil, fmt.Errorf("security requirement names undeclared scheme %q", name)
			}
		}
	}
	hint := naming.PascalGo(op.ID)

	// Parameters: operation-level overrides path-level by (name, in).
	type pk struct{ name, in string }
	var order []pk
	byKey := map[pk]map[string]any{}
	locOf := map[pk]string{}
	for _, src := range [][]any{shared, list(om["parameters"])} {
		for _, pv := range src {
			pm, ploc, err := c.ld.deref(loc, pv)
			if err != nil {
				return nil, err
			}
			if pm == nil {
				continue
			}
			k := pk{str(pm, "name"), str(pm, "in")}
			if _, seen := byKey[k]; !seen {
				order = append(order, k)
			}
			byKey[k] = pm
			locOf[k] = ploc
		}
	}
	var form []map[string]any
	var formLocs []string
	for _, k := range order {
		pm := byKey[k]
		switch k.in {
		case "body": // 2.0
			t, fi, err := c.schemaType(locOf[k], pm["schema"], hint+"Request")
			if err != nil {
				return nil, err
			}
			op.Body = &apispec.Body{
				Description: firstNonEmpty(str(pm, "description"), fi.Description),
				Required:    boolean(pm, "required"), Type: t,
				ContentTypes: c.consumes(om),
			}
			continue
		case "formData": // 2.0
			form = append(form, pm)
			formLocs = append(formLocs, locOf[k])
			continue
		}
		param, err := c.param(locOf[k], pm, hint)
		if err != nil {
			return nil, err
		}
		op.Params = append(op.Params, param)
	}
	if len(form) > 0 {
		name := c.lift(hint + "Form")
		t := &apispec.Type{Name: name, Kind: apispec.KindObject, Synthetic: true}
		c.spec.Types[name] = t
		for i, pm := range form {
			fp, err := c.param(formLocs[i], pm, name)
			if err != nil {
				return nil, err
			}
			t.Fields = append(t.Fields, &apispec.Field{Name: fp.Name, Description: fp.Description, Type: fp.Type, Required: fp.Required, Default: fp.Default, Constraints: fp.Constraints})
		}
		required := false
		for _, f := range t.Fields {
			required = required || f.Required
		}
		op.Body = &apispec.Body{Required: required, Type: apispec.TypeRef{Named: name}, ContentTypes: c.consumes(om)}
	}

	// 3.x requestBody.
	if rb, rloc, err := c.ld.deref(loc, om["requestBody"]); err != nil {
		return nil, err
	} else if rb != nil {
		mt, sch := pickMedia(obj(rb["content"]))
		body := &apispec.Body{Description: str(rb, "description"), Required: boolean(rb, "required"), ContentTypes: mediaTypes(obj(rb["content"]))}
		if mt != nil {
			t, fi, err := c.schemaType(rloc, sch, hint+"Request")
			if err != nil {
				return nil, err
			}
			body.Type = t
			if body.Description == "" {
				body.Description = fi.Description
			}
		} else {
			body.Type = apispec.TypeRef{Scalar: apispec.ScalarBytes}
		}
		op.Body = body
	}

	// Responses.
	resps := obj(om["responses"])
	for _, status := range keys(resps) {
		if status == orderKey || strings.HasPrefix(status, "x-") {
			continue
		}
		rm, rloc, err := c.ld.deref(loc, resps[status])
		if err != nil {
			return nil, err
		}
		if rm == nil {
			continue
		}
		r := &apispec.Response{Status: strings.ToUpper(status), Description: str(rm, "description")}
		var sch any
		if c.swagger2 {
			sch = rm["schema"]
			r.ContentTypes = c.produces(om)
		} else {
			_, sch = pickMedia(obj(rm["content"]))
			r.ContentTypes = mediaTypes(obj(rm["content"]))
		}
		if sch != nil {
			t, _, err := c.schemaType(rloc, sch, hint+responseSuffix(status))
			if err != nil {
				return nil, err
			}
			r.Type = &t
		}
		hdrs := obj(rm["headers"])
		for _, hn := range keys(hdrs) {
			if hn == orderKey {
				continue
			}
			hm, hloc, err := c.ld.deref(rloc, hdrs[hn])
			if err != nil {
				return nil, fmt.Errorf("response %s header %s: %w", status, hn, err)
			}
			if hm == nil {
				continue
			}
			// Copy: the header object may be shared through a $ref.
			hc := make(map[string]any, len(hm)+2)
			for k, v := range hm {
				hc[k] = v
			}
			hc["name"], hc["in"] = hn, "header"
			hp, err := c.param(hloc, hc, hint+naming.PascalGo(hn))
			if err != nil {
				return nil, fmt.Errorf("response %s header %s: %w", status, hn, err)
			}
			r.Headers = append(r.Headers, hp)
		}
		op.Responses = append(op.Responses, r)
	}
	if r := successResponse(op.Responses); r != nil {
		op.Result = r.Type
	}
	return op, nil
}

func responseSuffix(status string) string {
	if strings.HasPrefix(status, "2") {
		return "Response"
	}
	if status == "default" {
		return "DefaultResponse"
	}
	return "Response" + strings.ToUpper(status)
}

// successResponse picks the primary 2xx response that carries a payload: 200,
// 201, 202, 203, 206, 2XX, then any other 2xx. A 204 or a payload-less 200
// does not hide a later typed one.
func successResponse(rs []*apispec.Response) *apispec.Response {
	for _, want := range []string{"200", "201", "202", "203", "206", "2XX"} {
		for _, r := range rs {
			if r.Status == want && r.Type != nil {
				return r
			}
		}
	}
	for _, r := range rs {
		if strings.HasPrefix(r.Status, "2") && r.Type != nil {
			return r
		}
	}
	return nil
}

// param converts one parameter (3.x schema/content form or 2.0 inline form).
func (c *converter) param(loc string, pm map[string]any, hint string) (*apispec.Param, error) {
	p := &apispec.Param{
		Name: str(pm, "name"), In: apispec.ParamIn(str(pm, "in")),
		Description: str(pm, "description"), Required: boolean(pm, "required"),
		Deprecated: boolean(pm, "deprecated"), Style: str(pm, "style"),
		Example: clean(pm["example"]),
	}
	if p.In == "formData" {
		p.In = apispec.InQuery
	}
	if e, ok := pm["explode"].(bool); ok {
		p.Explode = &e
	}
	if c.swagger2 && str(pm, "type") == "array" {
		p.Style, p.Explode = collectionFormat(str(pm, "collectionFormat"), p.In)
	}
	sch := pm["schema"]
	if sch == nil {
		if _, s := pickMedia(obj(pm["content"])); s != nil {
			sch = s
		}
	}
	if sch == nil {
		sch = pm // 2.0: type/format/items/enum live on the parameter itself
	}
	if p.In == apispec.InPath {
		p.Required = true
	}
	t, fi, err := c.schemaType(loc, sch, hint+naming.PascalGo(p.Name))
	if err != nil {
		return nil, err
	}
	if str(pm, "type") == "file" { // 2.0 upload
		t = apispec.TypeRef{Scalar: apispec.ScalarBytes}
	}
	p.Type, p.Default, p.Constraints = t, fi.Default, fi.Constraints
	if p.Description == "" {
		p.Description = fi.Description
	}
	if p.Example == nil {
		p.Example = fi.Example
	}
	return p, nil
}

// collectionFormat maps a Swagger 2.0 array serialization onto the 3.x
// style/explode pair that sends the same bytes. tsv has no 3.x style and is
// kept verbatim.
func collectionFormat(f string, in apispec.ParamIn) (string, *bool) {
	no, yes := false, true
	switch f {
	case "", "csv":
		if in == apispec.InHeader || in == apispec.InPath {
			return "simple", &no
		}
		return "form", &no
	case "multi":
		return "form", &yes
	case "ssv":
		return "spaceDelimited", &no
	case "pipes":
		return "pipeDelimited", &no
	}
	return f, &no
}

// pickMedia chooses the most structured media type: JSON first, then any
// +json, then form encodings, then the first declared. Entries that carry a
// schema are preferred, so an empty application/json does not hide a typed
// vendor media type.
func pickMedia(content map[string]any) (map[string]any, any) {
	for _, needSchema := range []bool{true, false} {
		if mt, sch, ok := pickMediaWhere(content, needSchema); ok {
			return mt, sch
		}
	}
	return nil, nil
}

func pickMediaWhere(content map[string]any, needSchema bool) (map[string]any, any, bool) {
	var ks []string
	for _, k := range keys(content) {
		if k == orderKey {
			continue
		}
		if needSchema && obj(content[k])["schema"] == nil {
			continue
		}
		ks = append(ks, k)
	}
	pick := ""
	for _, pref := range []func(string) bool{
		func(k string) bool { return k == "application/json" },
		func(k string) bool { return strings.HasSuffix(k, "+json") || strings.Contains(k, "json") },
		func(k string) bool { return k == "application/x-www-form-urlencoded" || k == "multipart/form-data" },
		func(k string) bool { return k != orderKey },
	} {
		for _, k := range ks {
			if pref(k) {
				pick = k
				break
			}
		}
		if pick != "" {
			break
		}
	}
	if pick == "" {
		return nil, nil, false
	}
	mt := obj(content[pick])
	if mt == nil {
		return nil, nil, false
	}
	return mt, mt["schema"], true
}

func mediaTypes(content map[string]any) []string {
	var out []string
	for _, k := range keys(content) {
		if k != orderKey {
			out = append(out, k)
		}
	}
	return out
}

// consumes and produces: an operation's own list wins even when empty; the
// document's applies only when the operation omits the key.
func (c *converter) consumes(om map[string]any) []string {
	if v, ok := om["consumes"]; ok {
		return strs(v)
	}
	return strs(c.rootDoc["consumes"])
}

func (c *converter) produces(om map[string]any) []string {
	if v, ok := om["produces"]; ok {
		return strs(v)
	}
	return strs(c.rootDoc["produces"])
}

func (c *converter) uniqueOpID(id string) string {
	out := id
	for i := 2; c.opIDs[out]; i++ {
		out = fmt.Sprintf("%s%d", id, i)
	}
	c.opIDs[out] = true
	return out
}

// synthOperationID builds an id from method and path: GET /users/{id}/posts →
// getUsersIdPosts.
func synthOperationID(method, p string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(method))
	for _, seg := range strings.Split(p, "/") {
		seg = strings.Trim(seg, "{}")
		if seg == "" {
			continue
		}
		b.WriteString(naming.PascalGo(nonIdent.ReplaceAllString(seg, "_")))
	}
	return b.String()
}
