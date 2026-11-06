package expr

import (
	"fmt"
	"os"
	"slices"
	"testing"
	"time"
)

// TestPerfEval10k is the perf guard: 10 000 evals of a typical filter
// expression (parse included) take under 50 ms. One warm-up round, then
// the median of 9. Like the other perf guards it runs only with
// TERMOCODE_PERF=1, without -short and without -race (see perf.yml).
func TestPerfEval10k(t *testing.T) {
	if os.Getenv("TERMOCODE_PERF") != "1" || testing.Short() || raceOn {
		t.Skip("perf guard: set TERMOCODE_PERF=1, no -short, no -race")
	}
	const budget = 50 * time.Millisecond
	exprs := []string{
		`not item.plan.skip and item.desc matches "(?i)steps to reproduce"`,
		`item.labels has "bug" and item.prio <= 2`,
		`len(item.labels) > 0 or item.id startswith "LIN"`,
		`"fix: " + item.title`,
	}
	scope := testScope()
	round := func() time.Duration {
		start := time.Now()
		for i := range 10_000 {
			if _, err := Eval(exprs[i%len(exprs)], scope); err != nil {
				t.Fatal(err)
			}
		}
		return time.Since(start)
	}
	round() // warm-up: caches, page faults
	times := make([]time.Duration, 9)
	for i := range times {
		times[i] = round()
	}
	slices.Sort(times)
	got := times[len(times)/2]
	report("workflow_expr_eval_10k", got, budget)
	t.Logf("10k evals: median %v (budget %v), all %v", got, budget, times)
	if got > budget {
		t.Fatalf("10k evals took %v (median of 9), budget %v", got, budget)
	}
}

// report adds a row to TERMOCODE_PERF_OUT in the perfbudget format
// (name, got, budget, unit, ok|OVER) for the perf job summary.
func report(name string, got, budget time.Duration) {
	out := os.Getenv("TERMOCODE_PERF_OUT")
	if out == "" {
		return
	}
	f, err := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	mark := "ok"
	if got > budget {
		mark = "OVER"
	}
	fmt.Fprintf(f, "%s\t%.1f\t%.0f\tms\t%s\n", name, float64(got.Microseconds())/1000, float64(budget.Milliseconds()), mark)
}
