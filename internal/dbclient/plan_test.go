package dbclient

import (
	"strings"
	"testing"
)

func planRes(col string, rows ...string) Result {
	r := Result{Columns: []Column{{Name: col}}}
	for _, t := range rows {
		r.Rows = append(r.Rows, []Cell{{Text: t}})
	}
	return r
}

func planText(p Plan) string {
	var b strings.Builder
	for _, l := range p.Lines {
		b.WriteString(strings.Repeat("  ", l.Depth) + l.Text)
		if l.Costly {
			b.WriteString(" <costly>")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestExplainForms(t *testing.T) {
	cases := []struct {
		driver  string
		analyze bool
		maria   bool
		want    []string
	}{
		{"postgres", false, false, []string{"EXPLAIN (FORMAT JSON) SELECT 1"}},
		{"postgres", true, false, []string{"EXPLAIN (ANALYZE, FORMAT JSON) SELECT 1"}},
		{"mysql", false, false, []string{"EXPLAIN FORMAT=JSON SELECT 1", "EXPLAIN SELECT 1"}},
		{"mysql", true, false, []string{"EXPLAIN ANALYZE SELECT 1"}},
		{"mysql", true, true, []string{"ANALYZE FORMAT=JSON SELECT 1", "ANALYZE SELECT 1"}},
		{"sqlite", false, false, []string{"EXPLAIN QUERY PLAN SELECT 1"}},
		{"sqlite", true, false, []string{"EXPLAIN QUERY PLAN SELECT 1"}},
	}
	for _, c := range cases {
		got := ExplainForms(c.driver, "SELECT 1;", c.analyze, c.maria)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s analyze=%v maria=%v: %q, want %q", c.driver, c.analyze, c.maria, got, c.want)
		}
	}
	if ExplainForms("redis", "GET a", false, false) != nil {
		t.Error("redis has no plan")
	}
}

func TestParsePlanPostgresJSONMarksTheCostlyNode(t *testing.T) {
	const js = `[{"Plan":{"Node Type":"Hash Join","Join Type":"Inner","Startup Cost":10.5,"Total Cost":1000,"Plan Rows":50,
	 "Hash Cond":"(o.user_id = u.id)","Plans":[
	  {"Node Type":"Seq Scan","Relation Name":"orders","Alias":"o","Startup Cost":0,"Total Cost":900,"Plan Rows":50000,"Filter":"(total > 10)"},
	  {"Node Type":"Hash","Startup Cost":5,"Total Cost":8,"Plan Rows":100,"Plans":[
	    {"Node Type":"Index Scan","Relation Name":"users","Index Name":"users_pkey","Startup Cost":0.1,"Total Cost":7,"Plan Rows":100}]}]},
	 "Planning Time":0.25,"Execution Time":12.5}]`
	p := ParsePlan("postgres", planRes("QUERY PLAN", js), false)
	got := planText(p)
	for _, want := range []string{
		"Hash Join  cost=10.5..1000 rows=50\n",
		"  Hash Cond: (o.user_id = u.id)\n",
		"  Seq Scan on orders o  cost=0..900 rows=50000 <costly>\n",
		"    Filter: (total > 10)\n",
		"    Index Scan on users using users_pkey  cost=0.1..7 rows=100\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("plan lacks %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "<costly>") != 1 {
		t.Errorf("want exactly one costly node:\n%s", got)
	}
	if p.Nodes() != 4 {
		t.Errorf("nodes = %d", p.Nodes())
	}
	if strings.Join(p.Summary, "|") != "Planning time: 0.25 ms|Execution time: 12.5 ms" {
		t.Errorf("summary %q", p.Summary)
	}
}

func TestParsePlanPostgresAnalyzeUsesTime(t *testing.T) {
	const js = `[{"Plan":{"Node Type":"Nested Loop","Total Cost":100,"Plan Rows":1,"Actual Total Time":50,"Actual Rows":3,"Actual Loops":1,"Plans":[
	  {"Node Type":"Seq Scan","Relation Name":"a","Total Cost":90,"Plan Rows":1,"Actual Total Time":1,"Actual Rows":1,"Actual Loops":1},
	  {"Node Type":"Index Scan","Relation Name":"b","Total Cost":1,"Plan Rows":1,"Actual Total Time":2,"Actual Rows":1,"Actual Loops":20}]}}]`
	got := planText(ParsePlan("postgres", planRes("QUERY PLAN", js), true))
	// by cost the Seq Scan wins; by time (2 ms x 20 loops of 50) the Index Scan does
	if !strings.Contains(got, "Index Scan on b  cost=0..1 rows=1 | actual 2 ms rows=1 loops=20 <costly>") {
		t.Errorf("time must decide, not cost:\n%s", got)
	}
	if strings.Count(got, "<costly>") != 1 {
		t.Errorf("only the index scan is costly:\n%s", got)
	}
}

func TestParsePlanSingleNodeIsNeverCostly(t *testing.T) {
	const js = `[{"Plan":{"Node Type":"Result","Total Cost":0.01,"Plan Rows":1}}]`
	if strings.Contains(planText(ParsePlan("postgres", planRes("QUERY PLAN", js), false)), "costly") {
		t.Error("a lone node has nothing to compare with")
	}
}

func TestParsePlanMySQLJSON(t *testing.T) {
	const js = `{"query_block":{"select_id":1,"cost_info":{"query_cost":"120.50"},"nested_loop":[
	 {"table":{"table_name":"orders","access_type":"ALL","rows_examined_per_scan":9000,"cost_info":{"read_cost":"90.00","eval_cost":"10.00","prefix_cost":"100.00"},"attached_condition":"(orders.total > 10)"}},
	 {"table":{"table_name":"users","access_type":"eq_ref","key":"PRIMARY","rows_examined_per_scan":1,"cost_info":{"read_cost":"0.25","eval_cost":"0.10"}}}]}}`
	got := planText(ParsePlan("mysql", planRes("EXPLAIN", js), false))
	for _, want := range []string{"query_block  cost=120.5\n", "  orders  ALL rows=9000 <costly>\n", "    Condition: (orders.total > 10)\n", "  users  eq_ref using PRIMARY rows=1\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("plan lacks %q:\n%s", want, got)
		}
	}
}

func TestParsePlanMySQLTableAndTree(t *testing.T) {
	r := Result{Columns: []Column{{Name: "id"}, {Name: "select_type"}, {Name: "table"}, {Name: "type"}, {Name: "key"}, {Name: "rows"}, {Name: "Extra"}}}
	r.Rows = [][]Cell{
		{{Text: "1"}, {Text: "SIMPLE"}, {Text: "orders"}, {Text: "ALL"}, {Null: true}, {Text: "9000"}, {Text: "Using where"}},
		{{Text: "1"}, {Text: "SIMPLE"}, {Text: "users"}, {Text: "eq_ref"}, {Text: "PRIMARY"}, {Text: "1"}, {Null: true}},
	}
	got := planText(ParsePlan("mysql", r, false))
	if !strings.Contains(got, "orders  ALL rows=9000 <costly>\n  Using where\n") || !strings.Contains(got, "users  eq_ref using PRIMARY rows=1\n") {
		t.Errorf("tabular plan:\n%s", got)
	}
	tree := planRes("EXPLAIN", "-> Nested loop inner join  (cost=1 rows=2) (actual time=0.1..0.2 rows=2 loops=1)\n    -> Table scan on a  (cost=0.5 rows=2) (actual time=0.05..0.1 rows=2 loops=1)\n    -> Single-row index lookup on b using PRIMARY\n")
	got = planText(ParsePlan("mysql", tree, true))
	if !strings.HasPrefix(got, "Nested loop inner join") || !strings.Contains(got, "\n  Table scan on a") || !strings.Contains(got, "\n  Single-row index lookup on b") {
		t.Errorf("tree plan:\n%s", got)
	}
}

func TestParsePlanSQLite(t *testing.T) {
	text := planRes("plan", "QUERY PLAN", "|--SCAN orders", "`--SEARCH users USING INTEGER PRIMARY KEY (rowid=?)", "   `--USE TEMP B-TREE FOR ORDER BY")
	p := ParsePlan("sqlite", text, false)
	got := planText(p)
	want := "SCAN orders <costly>\nSEARCH users USING INTEGER PRIMARY KEY (rowid=?)\n  USE TEMP B-TREE FOR ORDER BY\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	rows := Result{Columns: []Column{{Name: "id"}, {Name: "parent"}, {Name: "notused"}, {Name: "detail"}}, Rows: [][]Cell{
		{{Text: "2"}, {Text: "0"}, {Text: "0"}, {Text: "SEARCH t USING INDEX i (a=?)"}},
		{{Text: "5"}, {Text: "2"}, {Text: "0"}, {Text: "USE TEMP B-TREE FOR ORDER BY"}},
	}}
	if got := planText(ParsePlan("sqlite", rows, false)); got != "SEARCH t USING INDEX i (a=?)\n  USE TEMP B-TREE FOR ORDER BY\n" {
		t.Errorf("id/parent rows:\n%s", got)
	}
}

func TestParsePlanPostgresTextFallback(t *testing.T) {
	p := ParsePlan("postgres", planRes("QUERY PLAN", "Hash Join  (cost=1..2 rows=1 width=4)", "  Hash Cond: (a = b)", "  ->  Seq Scan on a  (cost=0..1 rows=1 width=4)", "  ->  Hash  (cost=1..1 rows=1 width=4)", "        ->  Seq Scan on b  (cost=0..1 rows=1 width=4)"), false)
	got := planText(p)
	if got != "Hash Join  (cost=1..2 rows=1 width=4)\n  Hash Cond: (a = b)\n  Seq Scan on a  (cost=0..1 rows=1 width=4)\n  Hash  (cost=1..1 rows=1 width=4)\n    Seq Scan on b  (cost=0..1 rows=1 width=4)\n" {
		t.Errorf("text plan:\n%s", got)
	}
}
