package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func dbPlanBarsRun() *dbPlanRun {
	return &dbPlanRun{parsed: dbclient.Plan{
		Lines: []dbclient.PlanLine{
			{Depth: 0, Text: "Hash Join", Share: 0.25},
			{Depth: 1, Text: "Seq Scan on orders", Share: 0.75, Costly: true},
			{Depth: 2, Text: "Filter: (id > 1)", Detail: true},
		},
		Raw: `[{"Plan":{"Node Type":"Hash Join"}}]`,
	}}
}

// b on the Plan view switches to the bar view: one bar per node with its
// share; the details are left out.
func TestDBPlanBarsView(t *testing.T) {
	m := &Model{w: 120, h: 40}
	pl := dbPlanBarsRun()
	var p dbPage
	m.databasePlanView(&p, pl, 100)
	if strings.Contains(strings.Join(p.lines, "\n"), "█") {
		t.Fatal("the tree view shows bars")
	}
	pl.bars = true
	p = dbPage{}
	m.databasePlanView(&p, pl, 100)
	out := strings.Join(p.lines, "\n")
	for _, want := range []string{"█", "75.0%", "Seq Scan on orders", "25.0%"} {
		if !strings.Contains(out, want) {
			t.Errorf("bar view misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Filter:") {
		t.Errorf("bar view shows a detail line:\n%s", out)
	}
}

// C on a plan copies the server's own answer (the native JSON).
func TestDBPlanCopyNativeJSON(t *testing.T) {
	pl := dbPlanBarsRun()
	if got := dbPlanNative(pl); got != `[{"Plan":{"Node Type":"Hash Join"}}]` {
		t.Fatalf("native = %q", got)
	}
}
