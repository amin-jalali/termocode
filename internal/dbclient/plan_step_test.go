package dbclient

import (
	"reflect"
	"strings"
	"testing"
)

// Each plan node knows the table it reads and keeps every field the
// server sent for it: the step details panel of the Plan view (DBX-39).
func TestParsePlanPostgresStepTableAndFields(t *testing.T) {
	const js = `[{"Plan":{"Node Type":"Hash Join","Total Cost":10,"Plan Rows":5,"Hash Cond":"(o.uid = u.id)","Parallel Aware":false,
	 "Plans":[
	  {"Node Type":"Seq Scan","Relation Name":"orders","Schema":"sales","Alias":"o","Total Cost":8,"Plan Rows":1000,"Output":["o.id","o.uid"]},
	  {"Node Type":"Index Scan","Relation Name":"users","Alias":"u","Index Name":"users_pkey","Total Cost":1,"Plan Rows":1}
	 ]}}]`
	p := ParsePlan("postgres", planRes("QUERY PLAN", js), false)
	var nodes []PlanLine
	for _, l := range p.Lines {
		if !l.Detail {
			nodes = append(nodes, l)
		}
	}
	if len(nodes) != 3 {
		t.Fatalf("nodes = %d:\n%s", len(nodes), planText(p))
	}
	if nodes[0].Table != "" || nodes[1].Table != "sales.orders" || nodes[2].Table != "users" {
		t.Errorf("tables = %q %q %q", nodes[0].Table, nodes[1].Table, nodes[2].Table)
	}
	want := []string{"Alias: o", "Node Type: Seq Scan", "Output: o.id, o.uid", "Plan Rows: 1000", "Relation Name: orders", "Schema: sales", "Total Cost: 8"}
	if !reflect.DeepEqual(nodes[1].Fields, want) {
		t.Errorf("fields = %q\nwant %q", nodes[1].Fields, want)
	}
	for _, f := range nodes[0].Fields {
		if strings.HasPrefix(f, "Plans") {
			t.Errorf("the children are a field: %q", f)
		}
	}
	if !strings.Contains(strings.Join(nodes[0].Fields, "\n"), "Parallel Aware: false") {
		t.Errorf("a false flag is lost: %q", nodes[0].Fields)
	}
}

func TestParsePlanMySQLStepTableAndFields(t *testing.T) {
	const js = `{"query_block":{"select_id":1,"cost_info":{"query_cost":"12.5"},
	 "table":{"table_name":"orders","access_type":"ALL","rows_examined_per_scan":100,"cost_info":{"read_cost":"10.0","eval_cost":"2.5"},"used_columns":["id","uid"]}}}`
	p := ParsePlan("mysql", planRes("EXPLAIN", js), false)
	var tbl PlanLine
	for _, l := range p.Lines {
		if l.Table != "" {
			tbl = l
		}
	}
	if tbl.Table != "orders" {
		t.Fatalf("no table node:\n%s", planText(p))
	}
	got := strings.Join(tbl.Fields, "\n")
	for _, want := range []string{"access_type: ALL", "cost_info.read_cost: 10.0", "cost_info.eval_cost: 2.5", "used_columns: id, uid", "rows_examined_per_scan: 100"} {
		if !strings.Contains(got, want) {
			t.Errorf("fields lack %q:\n%s", want, got)
		}
	}

	r := Result{Columns: []Column{{Name: "select_type"}, {Name: "table"}, {Name: "type"}, {Name: "rows"}}}
	r.Rows = [][]Cell{
		{{Text: "PRIMARY"}, {Text: "t"}, {Text: "ALL"}, {Text: "10"}},
		{{Text: "DERIVED"}, {Text: "<derived2>"}, {Text: "ALL"}, {Text: "3"}},
	}
	p = ParsePlan("mysql", r, false)
	if p.Lines[0].Table != "t" || p.Lines[1].Table != "" {
		t.Errorf("table form tables = %q %q", p.Lines[0].Table, p.Lines[1].Table)
	}
	if !reflect.DeepEqual(p.Lines[0].Fields, []string{"select_type: PRIMARY", "table: t", "type: ALL", "rows: 10"}) {
		t.Errorf("table form fields = %q", p.Lines[0].Fields)
	}
}

func TestParsePlanTextStepTables(t *testing.T) {
	p := ParsePlan("sqlite", planRes("plan", "QUERY PLAN", "|--SCAN orders", "`--SEARCH TABLE users USING INTEGER PRIMARY KEY (rowid=?)", "   `--USE TEMP B-TREE FOR ORDER BY"), false)
	if got := []string{p.Lines[0].Table, p.Lines[1].Table, p.Lines[2].Table}; !reflect.DeepEqual(got, []string{"orders", "users", ""}) {
		t.Errorf("sqlite tables = %q", got)
	}
	p = ParsePlan("postgres", planRes("QUERY PLAN", "Hash Join  (cost=1..2 rows=1 width=4)", "  Hash Cond: (a = b)", "  ->  Index Scan using a_pkey on public.a x  (cost=0..1 rows=1 width=4)"), false)
	if got := []string{p.Lines[0].Table, p.Lines[1].Table, p.Lines[2].Table}; !reflect.DeepEqual(got, []string{"", "", "public.a"}) {
		t.Errorf("text tables = %q", got)
	}
}
