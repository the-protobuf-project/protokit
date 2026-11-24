package apispec

import "strings"

var (
	sizeParams  = []string{"page_size", "pagesize", "limit", "per_page", "perpage", "first", "last", "size", "count", "max_results", "maxresults", "top", "$top", "page_limit", "max"}
	tokenParams = []string{"page_token", "pagetoken", "cursor", "after", "before", "starting_after", "ending_before", "continuation_token", "continuationtoken", "next_token", "nexttoken", "marker", "offset", "skip", "$skip", "start", "page", "page_number", "pagenumber"}
	itemFields  = []string{"items", "data", "results", "nodes", "edges", "records", "entries", "values", "content", "elements", "list"}
	nextFields  = []string{"next_page_token", "nextpagetoken", "next_cursor", "nextcursor", "next_token", "nexttoken", "cursor", "next", "has_more", "hasmore", "page_info", "pageinfo", "links", "_links"}
)

func norm(s string) string { return strings.ToLower(strings.ReplaceAll(s, "-", "_")) }

func matchParam(ps []*Param, names []string) string {
	for _, want := range names {
		for _, p := range ps {
			n := norm(p.Name)
			if n == want || strings.ReplaceAll(n, "_", "") == strings.ReplaceAll(want, "_", "") {
				return p.Name
			}
		}
	}
	return ""
}

// DetectPagination recognizes the paging contract of a read operation:
// AIP-158 page tokens, cursors, offset/limit, page numbers and Relay
// connections. It returns nil for operations that are not list-shaped.
func DetectPagination(s *Spec, op *Operation) *Pagination {
	if op.Result == nil {
		return nil
	}
	if op.HTTP != nil && op.Kind == OpHTTP && op.HTTP.Method != "GET" && op.HTTP.Method != "QUERY" {
		return nil
	}
	if op.Kind == OpMutation || op.Kind == OpSubscription || op.Kind == OpWebhook {
		return nil
	}
	p := &Pagination{
		SizeParam:  matchParam(op.Params, sizeParams),
		TokenParam: matchParam(op.Params, tokenParams),
	}
	res := *op.Result
	switch {
	case res.List != nil:
		p.ItemType = res.Leaf().Named
	case res.Named != "":
		t := s.Types[res.Named]
		if t == nil || t.Kind != KindObject {
			return nil
		}
		if relay(s, t, p) {
			p.Style = "relay"
			return p
		}
		for _, want := range itemFields {
			for _, f := range t.Fields {
				if norm(f.Name) == want && f.Type.List != nil {
					p.ItemsField, p.ItemType = f.Name, f.Type.Leaf().Named
					break
				}
			}
			if p.ItemsField != "" {
				break
			}
		}
		if p.ItemsField == "" {
			// A single list-of-objects field: the plural collection itself (users: [User]).
			var lists []*Field
			for _, f := range t.Fields {
				if f.Type.List != nil && f.Type.Leaf().Named != "" {
					lists = append(lists, f)
				}
			}
			if len(lists) == 1 {
				p.ItemsField, p.ItemType = lists[0].Name, lists[0].Type.Leaf().Named
			}
		}
		for _, want := range nextFields {
			if f := fieldNorm(t, want); f != nil {
				p.NextField = f.Name
				break
			}
		}
		if p.ItemsField == "" {
			return nil
		}
	default:
		return nil
	}
	if p.SizeParam == "" && p.TokenParam == "" && p.NextField == "" && res.List == nil {
		return nil
	}
	switch norm(p.TokenParam) {
	case "offset", "skip", "$skip", "start":
		p.Style = "offset"
	case "page", "page_number", "pagenumber":
		p.Style = "page"
	case "":
		if p.NextField != "" {
			p.Style = "cursor"
		} else {
			p.Style = "none"
		}
	default:
		p.Style = "cursor"
	}
	return p
}

func fieldNorm(t *Type, want string) *Field {
	for _, f := range t.Fields {
		if norm(f.Name) == want || strings.ReplaceAll(norm(f.Name), "_", "") == strings.ReplaceAll(want, "_", "") {
			return f
		}
	}
	return nil
}

// relay recognizes a Relay connection: edges [{node, cursor}] + pageInfo.
func relay(s *Spec, t *Type, p *Pagination) bool {
	edges := fieldNorm(t, "edges")
	info := fieldNorm(t, "pageinfo")
	if edges == nil || info == nil || edges.Type.List == nil {
		return false
	}
	et := s.Types[edges.Type.Leaf().Named]
	if et == nil {
		return false
	}
	node := et.Field("node")
	if node == nil {
		return false
	}
	p.ItemsField, p.NextField, p.ItemType = edges.Name, info.Name, node.Type.Leaf().Named
	return true
}
