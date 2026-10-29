package dbclient

import (
	"strings"
	"testing"
)

// PostgreSQL JSON plans: join types, sub-plans, list and flag details are
// shown; a node that ran zero loops counts as one.
func TestParsePlanPostgresJSONDetails(t *testing.T) {
	const js = `[{"Plan":{"Node Type":"Hash Join","Join Type":"Left","Startup Cost":1,"Total Cost":10,"Plan Rows":5,
	 "Actual Total Time":2.5,"Actual Loops":0,"Actual Rows":5,"Rows Removed by Filter":120,"Hash Cond":"(a.id = b.id)",
	 "Plans":[
	  {"Node Type":"Sort","Subplan Name":"SubPlan 1","Sort Key":["a.x","a.y DESC"],"Total Cost":0,"Plan Rows":1},
	  {"Node Type":"Seq Scan","Relation Name":"b","Alias":"bb","Total Cost":9,"Plan Rows":1000}
	 ]}}]`
	got := planText(ParsePlan("postgres", planRes("QUERY PLAN", js), true))
	for _, want := range []string{"Left Hash Join", "Rows Removed by Filter: 120", "Hash Cond: (a.id = b.id)", "SubPlan 1: Sort", "Sort Key: a.x, a.y DESC", "Seq Scan on b bb"} {
		if !strings.Contains(got, want) {
			t.Errorf("plan lacks %q:\n%s", want, got)
		}
	}
	// JSON without a "Plan" is shown as the server's text.
	if got := planText(ParsePlan("postgres", planRes("QUERY PLAN", `[{"Other":1}]`), false)); !strings.Contains(got, `[{"Other":1}]`) {
		t.Errorf("fallback:\n%s", got)
	}
}

// MariaDB JSON plans (ANALYZE times, "rows", nested blocks, subqueries)
// read like MySQL ones; text that is not a plan is shown as it is.
func TestParsePlanMariaDBJSON(t *testing.T) {
	const js = `{"query_block":{"select_id":1,"r_total_time_ms":3.2,
	 "ordering_operation":{"using_filesort":true,"table":{"table_name":"t","access_type":"ALL","rows":500,"r_total_time_ms":2.9,"used_columns":["a"]}},
	 "subqueries":[{"query_block":{"select_id":2,"nested_loop":[{"block-nl-join":{"table":{"table_name":"u","access_type":"ref","rows":3}}}]}}]}}`
	got := planText(ParsePlan("mariadb", planRes("EXPLAIN", js), true))
	for _, want := range []string{"t  ALL rows=500 | actual 2.9 ms", "u  ref rows=3"} {
		if !strings.Contains(got, want) {
			t.Errorf("plan lacks %q:\n%s", want, got)
		}
	}
	for _, text := range []string{"{not json", `{"other":{}}`} {
		if got := planText(ParsePlan("mysql", planRes("EXPLAIN", text), false)); !strings.Contains(got, text) {
			t.Errorf("%q fallback:\n%s", text, got)
		}
	}
}

// MariaDB ANALYZE in table form adds the actual rows; a row without a
// table (a UNION result) is named by its select type.
func TestParsePlanMySQLTableActualRows(t *testing.T) {
	r := Result{Columns: []Column{{Name: "select_type"}, {Name: "table"}, {Name: "type"}, {Name: "rows"}, {Name: "r_rows"}}}
	r.Rows = [][]Cell{
		{{Text: "PRIMARY"}, {Text: "t"}, {Text: "ALL"}, {Text: "10"}, {Text: "9.00"}},
		{{Text: "UNION RESULT"}, {Null: true}, {Text: "ALL"}, {Null: true}, {Null: true}},
	}
	got := planText(ParsePlan("mysql", r, true))
	if !strings.Contains(got, "t  ALL rows=10 | actual rows=9.00") || !strings.Contains(got, "UNION RESULT  ALL") {
		t.Errorf("plan:\n%s", got)
	}
}
