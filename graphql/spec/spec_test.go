package spec

import (
	"testing"

	"github.com/the-protobuf-project/protokit/apispec"
	"github.com/the-protobuf-project/protokit/graphql/dialect"
	"github.com/the-protobuf-project/protokit/graphql/introspect"
)

const sdl = `
scalar timestamptz
scalar Money @specifiedBy(url: "https://example.com/money")
interface Node { id: ID! }
"A person."
type User implements Node @key(fields: "id") {
  id: ID!
  email: String! @unique
  name: String
  posts(first: Int = 10, after: String): PostConnection!
  created_at: timestamptz!
  balance: Money
  old: String @deprecated(reason: "use name")
}
type Post implements Node { id: ID!, title: String!, author: User!, tags: [String!] }
type PostEdge { node: Post!, cursor: String! }
type PageInfo { hasNextPage: Boolean!, endCursor: String }
type PostConnection { edges: [PostEdge!]!, pageInfo: PageInfo! }
union SearchResult = User | Post
enum Role { ADMIN MEMBER GUEST @deprecated }
input CreateUserInput { email: String!, role: Role = MEMBER }
type users_aggregate { count: Int }
type Query {
  user(id: ID!): User
  users(limit: Int, offset: Int): [User!]!
  search(q: String!): [SearchResult!]!
  node(id: ID!): Node
}
type Mutation { createUser(input: CreateUserInput!): User! }
`

func TestBuild(t *testing.T) {
	in, err := introspect.ParseSDL("t.graphql", sdl)
	if err != nil {
		t.Fatal(err)
	}
	s := Build(in, dialect.Default())
	if Build(in, nil).Types["users_aggregate"].Generated {
		t.Errorf("nil dialect must not mark Generated")
	}
	u := s.Types["User"]
	if u == nil || u.Kind != apispec.KindObject || u.Description != "A person." {
		t.Fatalf("User: %+v", u)
	}
	if len(u.Implements) != 1 || u.Implements[0] != "Node" || len(u.Directives) != 1 || u.Directives[0].Args["fields"] != "\"id\"" {
		t.Errorf("User meta: %+v %+v", u.Implements, u.Directives)
	}
	if f := u.Field("email"); !f.Required || len(f.Directives) != 1 || f.Directives[0].Name != "unique" {
		t.Errorf("email: %+v", f)
	}
	if f := u.Field("created_at"); f.Type.Scalar != apispec.ScalarTimestamp || f.Type.Format != "timestamptz" {
		t.Errorf("created_at: %+v", f.Type)
	}
	if f := u.Field("old"); !f.Deprecated || f.DeprecationReason != "use name" {
		t.Errorf("old: %+v", f)
	}
	if f := u.Field("posts"); len(f.Args) != 2 || f.Args[0].Default != "10" {
		t.Errorf("posts args: %+v", f.Args)
	}
	if m := s.Types["Money"]; m.Extensions["specifiedByURL"] != "https://example.com/money" {
		t.Errorf("Money: %+v", m)
	}
	if sr := s.Types["SearchResult"]; sr.Kind != apispec.KindUnion || len(sr.Variants) != 2 {
		t.Errorf("SearchResult: %+v", sr)
	}
	if n := s.Types["Node"]; n.Kind != apispec.KindUnion || len(n.Variants) != 2 {
		t.Errorf("Node implementations: %+v", n.Variants)
	}
	if r := s.Types["Role"]; len(r.Values) != 3 || !r.Values[2].Deprecated {
		t.Errorf("Role: %+v", r.Values)
	}
	if in := s.Types["CreateUserInput"]; in.Kind != apispec.KindInput || in.Field("role").Default != "MEMBER" {
		t.Errorf("input: %+v", in)
	}
	if !s.Types["users_aggregate"].Generated {
		t.Errorf("aggregate should be Generated")
	}
	var users *apispec.Operation
	for _, op := range s.Operations {
		if op.ID == "users" {
			users = op
		}
	}
	if users == nil || users.Pagination == nil || users.Pagination.Style != "offset" || users.Pagination.ItemType != "User" {
		t.Errorf("users pagination: %+v", users)
	}
	if len(users.Groups) != 1 || users.Groups[0] != "User" {
		t.Errorf("users groups: %v", users.Groups)
	}
}

func TestRootsArgsAndInterfaces(t *testing.T) {
	in, err := introspect.ParseSDL("roots.graphql", `
interface Node { id: ID! }
interface Named implements Node { id: ID!, name: String }
type Item implements Node & Named { id: ID!, name: String }
type Payload { query: Query, item: Item }
type Query { self: Query, items(first: Int! = 10, tag: String @deprecated): [Item!]! }
type Mutation { touch: Payload }
`)
	if err != nil {
		t.Fatal(err)
	}
	s := Build(in, nil)
	q := s.Types["Query"]
	if q == nil || !q.Generated {
		t.Fatalf("Query must resolve (and be marked Generated): %+v", q)
	}
	for _, v := range s.Types["Node"].Variants {
		if v == "Named" {
			t.Error("an interface is not a runtime possible type")
		}
	}
	var first *apispec.Param
	for _, op := range s.Operations {
		if op.ID == "items" {
			first = op.Params[0]
		}
	}
	if first == nil || first.Required || first.Nullable {
		t.Fatalf("first: %+v (defaulted non-null: optional, not nullable)", first)
	}
	if refs := s.Types["Item"].References(); len(refs) < 2 {
		t.Errorf("Item references %v: interfaces missing", refs)
	}
}
