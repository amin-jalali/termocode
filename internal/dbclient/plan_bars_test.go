package dbclient

import (
	"math"
	"strings"
	"testing"
)

// DBX-39: every plan node knows its own share of the whole cost (or
// time), and the bar view draws one bar per node, widest for the node
// that takes the most.
func TestPlanSharesAndBars(t *testing.T) {
	const js = `[{"Plan":{"Node Type":"Hash Join","Total Cost":1000,"Plan Rows":50,"Plans":[
	  {"Node Type":"Seq Scan","Relation Name":"orders","Total Cost":900,"Plan Rows":50000},
	  {"Node Type":"Hash","Total Cost":8,"Plan Rows":100,"Plans":[
	    {"Node Type":"Index Scan","Relation Name":"users","Total Cost":7,"Plan Rows":100}]}]}}]`
	p := ParsePlan("postgres", planRes("QUERY PLAN", js), false)
	var shares []float64
	for _, l := range p.Lines {
		if !l.Detail {
			shares = append(shares, l.Share)
		}
	}
	want := []float64{0.092, 0.9, 0.001, 0.007}
	if len(shares) != len(want) {
		t.Fatalf("shares %v", shares)
	}
	for i := range want {
		if math.Abs(shares[i]-want[i]) > 1e-9 {
			t.Errorf("share %d = %v, want %v", i, shares[i], want[i])
		}
	}

	bars := PlanBars(p, 20)
	if len(bars) != 4 {
		t.Fatalf("bars %q", bars)
	}
	if !strings.HasPrefix(bars[1], strings.Repeat("█", 18)) || !strings.Contains(bars[1], " 90.0% ") || !strings.HasSuffix(bars[1], "Seq Scan on orders  cost=0..900 rows=50000") {
		t.Errorf("seq scan bar %q", bars[1])
	}
	for _, b := range bars {
		if w := len([]rune(b[:strings.Index(b, " ")])); w != 20 {
			t.Errorf("bar width %d in %q", w, b)
		}
	}
	if PlanBars(Plan{}, 20) != nil {
		t.Error("an empty plan has no bars")
	}
}
