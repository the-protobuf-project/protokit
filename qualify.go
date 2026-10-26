package protokit

// qualify.go holds the post-build IR passes that enforce Prisma's global
// namespaces: model and enum names must be unique per database regardless of
// @@schema. They run after every file is merged (see buildDatabases) and before
// relations are resolved, so FK rewiring sees the final names.

import (
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/the-protobuf-project/protokit/naming"
	"github.com/the-protobuf-project/protokit/schema"
)

// qualifyModels enforces the Prisma rule that model names occupy one global
// namespace per database (independent of @@schema). Normalizing embedded value
// types pulls same-named messages (a per-package "Media", "Location", …) into
// one database; here the colliding ones gain a schema-domain prefix
// ("calendar" + "Media" → "CalendarMedia") so every model name is unique. Runs
// before resolveRelations, which then reflects the new names onto every FK.
//
// Every participant in a collision is qualified — no occurrence keeps the bare
// name. This makes names stable as the schema grows: adding another same-named
// model later cannot silently rename (and force a destructive migration on) the
// ones already generated, which positional "first stays bare" qualification did.
func qualifyModels(db *schema.Database, diags *diagnostics) {
	byName := map[string][]*schema.Table{}
	for _, s := range db.Schemas {
		for _, t := range s.Tables {
			byName[t.ModelName] = append(byName[t.ModelName], t)
		}
	}
	used := map[string]bool{}
	for name, group := range byName {
		if len(group) < 2 {
			used[name] = true
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		group := byName[name]
		if len(group) < 2 {
			continue
		}
		sort.Slice(group, func(i, j int) bool {
			if group[i].PgSchema != group[j].PgSchema {
				return group[i].PgSchema < group[j].PgSchema
			}
			return group[i].ProtoMessage < group[j].ProtoMessage
		})
		for _, t := range group {
			base := naming.PascalGo(naming.SchemaDomain(t.PgSchema)) + t.ModelName
			q := base
			for n := 2; used[q]; n++ {
				q = base + strconv.Itoa(n)
			}
			used[q] = true
			diags.warnf("collision", "model %q is defined in multiple schemas; qualified to %q "+
				"(Prisma model names are global)", t.ModelName, q)
			t.ModelName = q
		}
	}
}

// qualifyLocalModels gives distinct names to the distinct proto messages of one
// schema that share a simple name. Schema-namespaced targets render LocalName
// bare (one Go package, one SQL schema), so a nested "GuaranteePayment.Address"
// beside a top-level "Address" would otherwise declare one model and one table
// twice. A nested message takes the names of the messages enclosing it
// ("GuaranteePayment.Address" → "GuaranteePaymentAddress", protoc-gen-go's Go
// name without the underscore); a top-level one keeps its simple name. Two that
// still collide — top-level messages of packages merged into one schema — gain
// their package's domain ("shop.cart.v1" → "CartAddress"), then a number as a
// last resort. It mirrors qualifyLocalEnums.
//
// A table whose SQL name was derived from its message name is renamed with it;
// a declared table name is kept. Runs before qualifyModels, which then sees the
// schema-local names as the models' own.
func qualifyLocalModels(db *schema.Database, diags *diagnostics) {
	for _, s := range db.Schemas {
		byName := map[string][]*schema.Table{}
		var order []string
		for _, t := range s.Tables {
			if t.Source == nil {
				continue
			}
			if _, ok := byName[t.LocalName]; !ok {
				order = append(order, t.LocalName)
			}
			byName[t.LocalName] = append(byName[t.LocalName], t)
		}
		used := map[string]bool{}
		for _, t := range s.Tables {
			if len(byName[t.LocalName]) < 2 {
				used[t.LocalName] = true
			}
		}
		for _, name := range order {
			group := byName[name]
			if len(group) < 2 {
				continue
			}
			sort.Slice(group, func(i, j int) bool { return group[i].ProtoMessage < group[j].ProtoMessage })
			names := make([]string, len(group))
			count := map[string]int{}
			for i, t := range group {
				names[i] = scopedMessageName(t.Source)
				count[names[i]]++
			}
			for i, t := range group {
				if count[names[i]] > 1 {
					pkg := strings.ReplaceAll(string(t.Source.ParentFile().Package()), ".", "_")
					names[i] = naming.PascalGo(naming.SchemaDomain(pkg)) + names[i]
				}
			}
			for i, t := range group {
				q := names[i]
				for n := 2; used[q]; n++ {
					q = names[i] + strconv.Itoa(n)
				}
				used[q] = true
				if q == t.LocalName {
					continue
				}
				diags.warnf("collision", "model %q of %s shares its name with another message of schema %q; qualified to %q",
					t.LocalName, t.ProtoMessage, s.Name, q)
				if t.Name == naming.SnakePlural(t.LocalName) {
					t.Name = naming.SnakePlural(q)
				}
				t.LocalName, t.ModelName = q, q
				renameOneofCases(s, t)
			}
		}
	}
}

// dedupeTableNames gives distinct SQL names to the tables of one schema that
// share one. Distinct messages can pluralize alike ("Tax" and "Taxes" are both
// "taxes"), and two models mapped to one table would read and write each
// other's rows. Each participant takes its model's singular snake name
// ("tax", "taxes"), then a number; a schema whose table names are already
// distinct is untouched. Runs after the stutter pass, on the final names.
func dedupeTableNames(db *schema.Database, diags *diagnostics) {
	for _, s := range db.Schemas {
		byName := map[string][]*schema.Table{}
		var order []string
		for _, t := range s.Tables {
			if _, ok := byName[t.Name]; !ok {
				order = append(order, t.Name)
			}
			byName[t.Name] = append(byName[t.Name], t)
		}
		used := map[string]bool{}
		for name, group := range byName {
			if len(group) < 2 {
				used[name] = true
			}
		}
		for _, name := range order {
			group := byName[name]
			if len(group) < 2 {
				continue
			}
			sort.Slice(group, func(i, j int) bool { return group[i].ProtoMessage < group[j].ProtoMessage })
			for _, t := range group {
				base := naming.SnakeCase(t.LocalName)
				q := base
				for n := 2; used[q]; n++ {
					q = base + "_" + strconv.Itoa(n)
				}
				used[q] = true
				if q == t.Name {
					continue
				}
				diags.warnf("collision", "table %q of %s shares its name with another table of schema %q; renamed %q",
					t.Name, t.ProtoMessage, s.Name, q)
				t.Name = q
			}
		}
	}
}

// fitTableNames fits every table and column name of db to Postgres's
// identifier limit (naming.FitIdentifier). Postgres cuts a longer name at 63
// bytes without a word, so two long names sharing their first 63 bytes would
// become one table, or one column; a hash of the whole keeps them apart. Runs
// on the final names, before relations resolve, so every foreign key and index
// names the fitted ones. A name that fits is untouched.
func fitTableNames(db *schema.Database) {
	for _, s := range db.Schemas {
		for _, t := range s.Tables {
			t.Name = naming.FitIdentifier(t.Name)
			renamed := map[string]string{}
			for _, c := range t.Columns {
				if f := naming.FitIdentifier(c.Name); f != c.Name {
					renamed[c.Name] = f
					c.Name = f
				}
			}
			if len(renamed) == 0 {
				continue
			}
			for _, idx := range t.Indexes {
				for i, c := range idx.Columns {
					if f, ok := renamed[c]; ok {
						idx.Columns[i] = f
					}
				}
			}
		}
	}
}

// fitEnumNames fits every enum's SQL type name of db to Postgres's identifier
// limit, as fitTableNames does for tables: two long type names cut to the same
// 63 bytes would be one type. Runs after dedupeEnums, on the final names.
func fitEnumNames(db *schema.Database) {
	for _, s := range db.Schemas {
		for _, e := range s.Enums {
			e.SQLName = naming.FitIdentifier(e.SQLName)
			e.LocalSQLName = naming.FitIdentifier(e.LocalSQLName)
		}
	}
}

// renameOneofCases renames the discriminator enums of t's oneofs (oneofEnum's
// "<Message><Oneof>Case") after t's new model name, so two same-named messages
// qualified apart don't keep one case enum name between them.
func renameOneofCases(s *schema.Schema, t *schema.Table) {
	oneofs := t.Source.Oneofs()
	for i := 0; i < oneofs.Len(); i++ {
		o := oneofs.Get(i)
		if o.IsSynthetic() {
			continue
		}
		protoName := string(o.FullName()) + ".case"
		for _, e := range s.Enums {
			if e.ProtoName != protoName {
				continue
			}
			q := t.LocalName + naming.PascalGo(string(o.Name())) + "Case"
			e.Name, e.LocalName = q, q
			e.SQLName, e.LocalSQLName = naming.SnakeCase(q), naming.SnakeCase(q)
		}
	}
}

// scopedMessageName is m's simple name prefixed with the names of the messages
// enclosing it, outermost first: "Outer.Inner.Address" → "OuterInnerAddress".
func scopedMessageName(m protoreflect.MessageDescriptor) string {
	name := string(m.Name())
	for p := m.Parent(); p != nil; p = p.Parent() {
		pm, ok := p.(protoreflect.MessageDescriptor)
		if !ok {
			break
		}
		name = string(pm.Name()) + name
	}
	return name
}

// dedupeEnums enforces the Prisma rule that enum type names occupy one global
// namespace per database (independent of @@schema). It (1) collapses the same
// proto enum built separately under multiple schemas onto a single canonical
// definition, repointing every column to it, and (2) qualifies the names of
// distinct enums that happen to share a simple name with a schema-derived
// prefix ("State" in calendar_app and alarm_app → "State" / "AlarmAppState").
func dedupeEnums(db *schema.Database, diags *diagnostics) {
	// Pass 1: choose one canonical *Enum per proto full name (first seen wins).
	canonical := map[string]*schema.Enum{}
	for _, s := range db.Schemas {
		for _, e := range s.Enums {
			if _, ok := canonical[e.ProtoName]; !ok {
				canonical[e.ProtoName] = e
			}
		}
	}
	// Repoint every enum column to its canonical definition, then keep only the
	// canonical enum in its home schema and drop the duplicates elsewhere.
	for _, s := range db.Schemas {
		for _, t := range s.Tables {
			for _, c := range t.Columns {
				if c.Enum != nil {
					c.Enum = canonical[c.Enum.ProtoName]
				}
			}
		}
		kept := s.Enums[:0]
		for _, e := range s.Enums {
			if canonical[e.ProtoName] == e {
				kept = append(kept, e)
			}
		}
		s.Enums = kept
	}

	// Pass 2: qualify simple-name collisions among distinct enums with a
	// schema-domain prefix ("calendar" + "State" → "CalendarState"). Sorted for
	// determinism; the first keeps the bare name, the rest gain a unique prefix.
	//
	// Prisma's namespace is shared by models and enums, so an enum named like a
	// model anywhere in the database collides even when it is the only enum of
	// its name ("TripPurpose" in one schema beside a model "TripPurpose" in
	// another): it is qualified the same way, and no qualified name may take a
	// model's.
	models := map[string]bool{}
	for _, s := range db.Schemas {
		for _, t := range s.Tables {
			models[t.ModelName] = true
		}
	}
	byName := map[string][]*schema.Enum{}
	for _, e := range canonical {
		byName[e.Name] = append(byName[e.Name], e)
	}
	used := map[string]bool{}
	for name := range models {
		used[name] = true
	}
	for name, group := range byName {
		if len(group) < 2 && !models[name] {
			used[name] = true
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		group := byName[name]
		if len(group) < 2 && !models[name] {
			continue
		}
		sort.Slice(group, func(i, j int) bool {
			if group[i].PgSchema != group[j].PgSchema {
				return group[i].PgSchema < group[j].PgSchema
			}
			return group[i].ProtoName < group[j].ProtoName
		})
		for _, e := range group {
			base := naming.PascalGo(naming.SchemaDomain(e.PgSchema)) + e.Name
			q := base
			for n := 2; used[q]; n++ {
				q = base + strconv.Itoa(n)
			}
			used[q] = true
			diags.warnf("collision", "enum %q is defined in multiple schemas; qualified to %q "+
				"(Prisma enum names are global)", name, q)
			e.Name = q
			e.SQLName = naming.SnakeCase(q)
		}
	}
}

// validateIndexes reports any index that names a column absent from its table,
// so a typo in the generator's table.indexes option is caught at generation time instead of
// surfacing as invalid DDL when the schema is applied.
func validateIndexes(db *schema.Database, diags *diagnostics) {
	for _, s := range db.Schemas {
		for _, t := range s.Tables {
			cols := make(map[string]bool, len(t.Columns))
			for _, c := range t.Columns {
				cols[c.Name] = true
			}
			for _, idx := range t.Indexes {
				label := idx.Name
				if label == "" {
					label = "(" + strings.Join(idx.Columns, ",") + ")"
				}
				for _, c := range idx.Columns {
					if !cols[c] {
						diags.warnf("index", "table %q index %s names unknown column %q",
							t.Name, label, c)
					}
				}
			}
		}
	}
}
