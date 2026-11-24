package openapi

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/the-protobuf-project/protokit/apispec"
	"github.com/the-protobuf-project/protokit/naming"
)

// Options configures loading.
type Options struct {
	// Fetch reads referenced documents; nil uses DefaultFetcher (files + http).
	Fetch Fetcher
}

// Load reads the document at location (a file path or http(s) URL) and every
// document it references, and normalizes the whole into an apispec.Spec.
func Load(ctx context.Context, location string, opts Options) (*apispec.Spec, error) {
	if !isURL(location) {
		abs, err := filepath.Abs(location)
		if err != nil {
			return nil, err
		}
		location = abs
	}
	ld := newLoader(ctx, opts.Fetch)
	return convert(ld, location)
}

// Parse normalizes an in-memory document. name is its location, used to
// resolve relative $refs (a path or URL); relative refs are read through opts.Fetch.
func Parse(ctx context.Context, name string, data []byte, opts Options) (*apispec.Spec, error) {
	if !isURL(name) {
		// Same absolute key Load uses, so relative refs reach the Fetcher as
		// absolute locations and refs back to this document hit the cache.
		abs, err := filepath.Abs(name)
		if err != nil {
			return nil, err
		}
		name = abs
	}
	ld := newLoader(ctx, opts.Fetch)
	d, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("openapi: parse %s: %w", name, err)
	}
	ld.docs[name] = d
	return convert(ld, name)
}

// converter carries conversion state.
type converter struct {
	ld        *loader
	root      string
	rootDoc   map[string]any
	swagger2  bool
	spec      *apispec.Spec
	refNames  map[string]string    // ref.String() -> type name
	reserved  map[string]bool      // names claimed by a ref still being built
	pending   map[string]bool      // reserved names lift() may hand out once
	aliasInfo map[string]fieldInfo // scalar alias name -> its slot info
	opIDs     map[string]bool
	composing map[string]bool // allOf members being expanded (cycle guard)
	idScope   []string        // OpenAPI 3.1 $id bases of the schemas being converted, innermost last
	callbacks map[string]bool // callback path items being converted (cycle guard)
}

func (c *converter) groupList() []string { return nil } // assigned by assignGroups

func convert(ld *loader, root string) (*apispec.Spec, error) {
	d, err := ld.doc(root)
	if err != nil {
		return nil, err
	}
	m := obj(d)
	if m == nil {
		return nil, fmt.Errorf("openapi: %s: not an object", root)
	}
	root = ld.base(root)
	c := &converter{
		ld: ld, root: root, rootDoc: m,
		spec:     apispec.New(apispec.SourceOpenAPI),
		refNames: map[string]string{}, reserved: map[string]bool{}, pending: map[string]bool{},
		aliasInfo: map[string]fieldInfo{}, opIDs: map[string]bool{}, composing: map[string]bool{}, callbacks: map[string]bool{},
	}
	switch {
	case str(m, "swagger") != "":
		c.swagger2 = true
		c.spec.SourceVersion = str(m, "swagger")
	case str(m, "openapi") != "":
		c.spec.SourceVersion = str(m, "openapi")
	default:
		return nil, fmt.Errorf("openapi: %s: neither \"openapi\" nor \"swagger\" is set", root)
	}
	info := obj(m["info"])
	c.spec.Title = str(info, "title")
	c.spec.Description = str(info, "description")
	c.spec.Version = str(info, "version")
	c.spec.Extensions = extensions(m)

	if err := c.servers(); err != nil {
		return nil, err
	}
	c.tags()
	if err := c.securitySchemes(); err != nil {
		return nil, err
	}
	if sec, ok := m["security"]; ok {
		c.spec.DefaultSecurity = securityRequirements(sec)
	}
	if err := c.components(); err != nil {
		return nil, err
	}
	if err := c.paths(obj(m["paths"]), apispec.OpHTTP); err != nil {
		return nil, err
	}
	if err := c.paths(obj(m["webhooks"]), apispec.OpWebhook); err != nil {
		return nil, err
	}
	for _, op := range c.spec.Operations {
		if op.Pagination == nil {
			op.Pagination = apispec.DetectPagination(c.spec, op)
		}
	}
	apispec.AssignGroups(c.spec)
	return c.spec, nil
}

// servers records where the API is served. Swagger 2.0 defaults host and
// scheme to the ones the document was retrieved from; OpenAPI 3.x defaults
// an absent or empty server list to "/", and resolves relative server URLs
// against the document's own URL. Nothing is invented for a local document.
func (c *converter) servers() error {
	var docURL *url.URL
	if isURL(c.root) {
		u, err := url.Parse(c.root)
		if err != nil {
			return err
		}
		docURL = u
	}
	if c.swagger2 {
		host := str(c.rootDoc, "host")
		if host == "" && docURL != nil {
			host = docURL.Host
		}
		if host == "" {
			if bp := str(c.rootDoc, "basePath"); bp != "" {
				c.spec.Servers = append(c.spec.Servers, apispec.Server{URL: bp})
			}
			return nil
		}
		schemes := strs(c.rootDoc["schemes"])
		if len(schemes) == 0 {
			schemes = []string{"https"}
			if docURL != nil {
				schemes = []string{docURL.Scheme}
			}
		}
		for _, s := range schemes {
			c.spec.Servers = append(c.spec.Servers, apispec.Server{URL: s + "://" + host + str(c.rootDoc, "basePath")})
		}
		return nil
	}
	for _, sv := range list(c.rootDoc["servers"]) {
		sm := obj(sv)
		u := str(sm, "url")
		vars := obj(sm["variables"])
		for _, k := range keys(vars) {
			if k == orderKey {
				continue
			}
			u = strings.ReplaceAll(u, "{"+k+"}", str(obj(vars[k]), "default"))
		}
		if docURL != nil && !isURL(u) {
			if r, err := url.Parse(u); err == nil {
				u = docURL.ResolveReference(r).String()
			}
		}
		c.spec.Servers = append(c.spec.Servers, apispec.Server{URL: u, Description: str(sm, "description")})
	}
	if len(c.spec.Servers) == 0 {
		u := "/"
		if docURL != nil {
			u = docURL.ResolveReference(&url.URL{Path: "/"}).String()
		}
		c.spec.Servers = append(c.spec.Servers, apispec.Server{URL: u})
	}
	return nil
}

// securityRequirements reads a security requirement list. An explicit empty
// list stays empty (not nil): it means "no security".
func securityRequirements(sec any) []map[string][]string {
	out := []map[string][]string{}
	for _, alt := range list(sec) {
		req := map[string][]string{}
		for k, v := range obj(alt) {
			if k != orderKey {
				req[k] = strs(v)
			}
		}
		out = append(out, req)
	}
	return out
}

func (c *converter) tags() {
	parent := map[string]string{}
	for _, g := range list(c.rootDoc["x-tagGroups"]) {
		gm := obj(g)
		for _, t := range strs(gm["tags"]) {
			parent[t] = str(gm, "name")
		}
	}
	for _, t := range list(c.rootDoc["tags"]) {
		tm := obj(t)
		name := str(tm, "name")
		if name == "" {
			continue
		}
		p := firstNonEmpty(parent[name], str(tm, "parent")) // OpenAPI 3.2 nested tags
		c.spec.AddGroup(&apispec.Group{Name: name, Description: str(tm, "description"), Parent: p})
	}
}

func (c *converter) securitySchemes() error {
	var defs map[string]any
	if c.swagger2 {
		defs = obj(c.rootDoc["securityDefinitions"])
	} else {
		defs = obj(obj(c.rootDoc["components"])["securitySchemes"])
	}
	for _, name := range keys(defs) {
		if name == orderKey {
			continue
		}
		sm, _, err := c.ld.deref(c.root, defs[name])
		if err != nil {
			return fmt.Errorf("openapi: security scheme %q: %w", name, err)
		}
		if sm == nil {
			return fmt.Errorf("openapi: security scheme %q is not an object", name)
		}
		s := &apispec.SecurityScheme{
			Name: name, Type: str(sm, "type"), Description: str(sm, "description"),
			In: str(sm, "in"), ParamName: str(sm, "name"), Scheme: str(sm, "scheme"),
			BearerFormat: str(sm, "bearerFormat"), OpenIDURL: str(sm, "openIdConnectUrl"),
			Flows: map[string]*apispec.OAuthFlow{},
		}
		if c.swagger2 && s.Type == "basic" {
			s.Type, s.Scheme = "http", "basic"
		}
		if c.swagger2 && s.Type == "oauth2" {
			s.Flows[str(sm, "flow")] = &apispec.OAuthFlow{
				AuthorizationURL: str(sm, "authorizationUrl"), TokenURL: str(sm, "tokenUrl"),
				Scopes: stringMap(obj(sm["scopes"])),
			}
		}
		flows := obj(sm["flows"])
		for _, fk := range keys(flows) {
			if fk == orderKey {
				continue
			}
			fm := obj(flows[fk])
			s.Flows[fk] = &apispec.OAuthFlow{
				AuthorizationURL: str(fm, "authorizationUrl"), TokenURL: str(fm, "tokenUrl"),
				RefreshURL: str(fm, "refreshUrl"), Scopes: stringMap(obj(fm["scopes"])),
			}
		}
		c.spec.Security[name] = s
	}
	return nil
}

func stringMap(m map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if k == orderKey {
			continue
		}
		s, _ := v.(string)
		out[k] = s
	}
	return out
}

// components registers every named schema so names are stable regardless of
// the order operations reach them.
func (c *converter) components() error {
	base := "/components/schemas"
	defs := obj(obj(c.rootDoc["components"])["schemas"])
	if c.swagger2 {
		base, defs = "/definitions", obj(c.rootDoc["definitions"])
	}
	for _, k := range keys(defs) {
		if k == orderKey {
			continue
		}
		if _, err := c.namedRef(ref{Loc: c.root, Pointer: base + "/" + escapePointer(k)}); err != nil {
			return err
		}
	}
	return nil
}

func escapePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

var nonIdent = regexp.MustCompile(`[^A-Za-z0-9]+`)

// typeName sanitizes a source schema name into a PascalCase identifier.
func typeName(s string) string {
	s = nonIdent.ReplaceAllString(s, "_")
	n := naming.PascalGo(strings.Trim(s, "_"))
	if n == "" {
		return "Schema"
	}
	if n[0] >= '0' && n[0] <= '9' {
		n = "T" + n
	}
	return n
}

// shortName names a schema. Dotted, namespaced names (Kubernetes'
// io.k8s.api.core.v1.Pod, .NET's Company.Models.User) use their last segment
// when free, widening one segment at a time to stay unique.
func (c *converter) shortName(raw string) string {
	taken := func(n string) bool { return c.spec.Types[n] != nil || c.reserved[n] }
	segs := strings.Split(raw, ".")
	for k := 1; k < len(segs); k++ {
		if n := typeName(strings.Join(segs[len(segs)-k:], "_")); n != "" && !taken(n) {
			return n
		}
	}
	base := typeName(raw)
	name := base
	for i := 2; taken(name); i++ {
		name = fmt.Sprintf("%s%d", base, i)
	}
	return name
}

// namedRef returns the type name for the schema at r, converting it on first use.
func (c *converter) namedRef(r ref) (string, error) {
	key := r.String()
	if n, ok := c.refNames[key]; ok {
		return n, nil
	}
	name := c.shortName(refName(r))
	c.refNames[key] = name
	c.reserved[name] = true
	c.pending[name] = true

	// A failed registration must not leave a name behind that a later
	// reference would reuse as if the type existed.
	fail := func(err error) (string, error) {
		delete(c.refNames, key)
		delete(c.reserved, name)
		delete(c.pending, name)
		delete(c.spec.Types, name)
		return "", err
	}
	target, err := c.ld.lookup(r)
	if err != nil {
		return fail(err)
	}
	// The referenced schema starts its own $id scope.
	saved := c.idScope
	c.idScope = nil
	tref, info, err := c.schemaType(c.ld.base(r.Loc), target, name)
	c.idScope = saved
	if err != nil {
		return fail(fmt.Errorf("%s: %w", name, err))
	}
	delete(c.pending, name)
	if tref.Named != name {
		// The schema is not itself an object/enum/union: a named alias.
		t := &apispec.Type{Name: name, Kind: apispec.KindScalar, Description: info.Description, Deprecated: info.Deprecated, Extensions: info.Extensions}
		r := tref
		if tref.Map != nil {
			t.Kind = apispec.KindMap
		}
		t.Scalar = &r
		c.spec.Types[name] = t
		c.aliasInfo[name] = info
	} else if t := c.spec.Types[name]; t != nil {
		t.Synthetic = false
	}
	return name, nil
}
