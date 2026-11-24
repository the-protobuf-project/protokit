package dialect

import "strings"

// TypeFilter is optionally implemented by a Dialect to recognize the helper
// types an engine generates around the domain model — boolean filter
// expressions, order-by inputs, aggregates, mutation envelopes, column
// selectors. Consumers that want the domain model (not the engine's query
// plumbing) skip them. It is a separate interface so existing Dialect
// implementations keep compiling.
type TypeFilter interface {
	// Generated reports whether the named type (kind is the introspection kind:
	// OBJECT, INPUT_OBJECT, ENUM, SCALAR) is engine plumbing.
	Generated(name, kind string) bool
}

// IsGenerated asks d's TypeFilter, when it has one.
func IsGenerated(d Dialect, name, kind string) bool {
	if d == nil {
		return false
	}
	if f, ok := d.(TypeFilter); ok {
		return f.Generated(name, kind)
	}
	return false
}

// Hasura v2 (snake_case) and v3/DDN (PascalCase) generated-type suffixes.
var hasuraGenerated = map[string][]string{
	"OBJECT": {
		"_aggregate", "_aggregate_fields", "_avg_fields", "_max_fields", "_min_fields",
		"_stddev_fields", "_stddev_pop_fields", "_stddev_samp_fields", "_sum_fields",
		"_var_pop_fields", "_var_samp_fields", "_variance_fields", "_mutation_response",
		"Aggregate", "AggregateFields", "AggExp", "MutationResponse",
		"AvgFields", "MaxFields", "MinFields", "SumFields", "StddevFields", "VarianceFields",
	},
	"INPUT_OBJECT": {
		"_bool_exp", "_comparison_exp", "_order_by", "_insert_input", "_set_input",
		"_inc_input", "_on_conflict", "_pk_columns_input", "_obj_rel_insert_input",
		"_arr_rel_insert_input", "_stream_cursor_input", "_stream_cursor_value_input",
		"_updates", "_append_input", "_prepend_input", "_delete_at_path_input",
		"_delete_elem_input", "_delete_key_input", "_aggregate_bool_exp",
		"_aggregate_order_by", "_avg_order_by", "_max_order_by", "_min_order_by",
		"_sum_order_by", "_stddev_order_by", "_variance_order_by",
		"BoolExp", "ComparisonExp", "OrderBy", "OrderByExp", "FilterInput", "AggExp",
		"UpdateColumns", "ObjectInput", "Updates", "OnConflict", "PkColumnsInput",
		"InsertInput", "SetInput", "IncInput",
	},
	"ENUM": {
		"_select_column", "_constraint", "_update_column", "_enum_name",
		"SelectColumn", "Constraint", "UpdateColumn",
	},
}

var hasuraGeneratedNames = map[string]bool{
	"order_by": true, "cursor_ordering": true, "OrderBy": true, "OrderByDirection": true,
	"Int_comparison_exp": true, "String_comparison_exp": true, "Boolean_comparison_exp": true,
}

// Generated implements TypeFilter.
func (hasura) Generated(name, kind string) bool {
	if hasuraGeneratedNames[name] || strings.HasSuffix(name, "_comparison_exp") {
		return true
	}
	// DDN mutation envelopes: InsertXResponse, UpdateXByIdResponse, DeleteXByIdResponse.
	if kind == "OBJECT" && strings.HasSuffix(name, "Response") {
		for _, v := range []string{"Insert", "Update", "Delete"} {
			if strings.HasPrefix(name, v) {
				return true
			}
		}
	}
	for _, s := range hasuraGenerated[kind] {
		if strings.HasSuffix(name, s) && len(name) > len(s) {
			return true
		}
	}
	return false
}
