package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func dbPlanStepRun() *dbPlanRun {
	return &dbPlanRun{parsed: dbclient.Plan{
		Lines: []dbclient.PlanLine{
			{Depth: 0, Text: "Hash Join  cost=0..10 rows=5", Share: 0.2, Fields: []string{"Node Type: Hash Join", "Total Cost: 10"}},
			{Depth: 1, Text: "Hash Cond: (o.uid = u.id)", Detail: true},
			{Depth: 1, Text: "Seq Scan on orders o  cost=0..8 rows=1000", Share: 0.7, Costly: true, Table: "sales.orders", Fields: []string{"Node Type: Seq Scan", "Relation Name: orders"}},
			{Depth: 2, Text: "Filter: (total > 1)", Detail: true},
			{Depth: 1, Text: "Index Scan on users u using users_pkey", Share: 0.1, Table: "users"},
		},
	}}
}

// The Plan view numbers its rows from the first step, so the cursor row
// names a step; a detail row belongs to the step above it.
func TestDBPlanViewRowsNameSteps(t *testing.T) {
	m := &Model{w: 120, h: 40}
	pl := dbPlanStepRun()
	var p dbPage
	p.add("header")
	m.databasePlanView(&p, pl, 100)
	if p.first != 2 {
		t.Fatalf("first = %d, want 2", p.first)
	}
	for row, want := range map[int]int{0: 0, 1: 0, 2: 2, 3: 2, 4: 4, 5: -1, -1: -1} {
		if got := dbPlanStepAt(pl, row); got != want {
			t.Errorf("row %d: step %d, want %d", row, got, want)
		}
	}
	pl.bars = true
	p = dbPage{}
	m.databasePlanView(&p, pl, 100)
	if p.first != 1 {
		t.Fatalf("bars first = %d", p.first)
	}
	for row, want := range map[int]int{0: 0, 1: 2, 2: 4, 3: -1} {
		if got := dbPlanStepAt(pl, row); got != want {
			t.Errorf("bars row %d: step %d, want %d", row, got, want)
		}
	}
}

// Enter on a step: its share, table, parent, child count, details and the
// server's own fields.
func TestDBPlanStepCard(t *testing.T) {
	pl := dbPlanStepRun()
	title, body := dbPlanStepCard(pl, 2)
	if !strings.Contains(title, "Seq Scan on orders o") {
		t.Errorf("title = %q", title)
	}
	got := strings.Join(body, "\n")
	for _, want := range []string{"70.0%", "costly", "Table: sales.orders", "Inside: Hash Join", "Filter: (total > 1)", "Relation Name: orders"} {
		if !strings.Contains(got, want) {
			t.Errorf("card lacks %q:\n%s", want, got)
		}
	}
	_, body = dbPlanStepCard(pl, 0)
	got = strings.Join(body, "\n")
	if !strings.Contains(got, "Steps under it: 2") || strings.Contains(got, "Inside:") || strings.Contains(got, "Filter:") {
		t.Errorf("root card:\n%s", got)
	}
	if !strings.Contains(got, "Hash Cond: (o.uid = u.id)") {
		t.Errorf("root card lacks its own detail:\n%s", got)
	}
}

// i on the Plan view: the quick doc of the step's table from the loaded
// Databases tree; on a step without a table, the doc of every table the
// plan reads.
func TestDBPlanTableDoc(t *testing.T) {
	pl := dbPlanStepRun()
	if got := dbPlanTables(pl); strings.Join(got, ",") != "sales.orders,users" {
		t.Fatalf("tables = %q", got)
	}
	s := dbQuickDocTestState()
	title, body := dbPlanTableDoc(s, "pg", []string{"sales.orders"})
	got := strings.Join(body, "\n")
	if !strings.Contains(title, "sales.orders") || !strings.Contains(got, "id  bigint") || !strings.Contains(got, "Foreign key") {
		t.Errorf("doc %q:\n%s", title, got)
	}
	title, body = dbPlanTableDoc(s, "pg", dbPlanTables(pl))
	got = strings.Join(body, "\n")
	if !strings.Contains(title, "2 tables") || !strings.Contains(got, "id  bigint") || !strings.Contains(got, "users") || !strings.Contains(got, "Not in the Databases tree") {
		t.Errorf("all tables %q:\n%s", title, got)
	}
}
