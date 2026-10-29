// Package perfbudget holds the performance budgets (budgets.txt) and the
// check the perf guard tests use (STAB-5). The guards measure a number
// (startup, key latency, memory, …) and fail when it is over its budget.
//
// They run only with TERMOCODE_PERF=1 and without -short, so the normal
// -race test run does not time things:
//
//	TERMOCODE_PERF=1 go test -count=1 -run Perf ./internal/testkit/
//	TERMOCODE_PERF=1 go -C e2e test -tags e2e -count=1 -run Perf ./...
//
// With TERMOCODE_PERF_OUT=<file>, each check adds a row
// "name<TAB>got<TAB>budget<TAB>unit<TAB>ok|OVER" to the file
// (.github/workflows/perf.yml turns it into the job summary).
//
// The e2e module (e2e/perf_test.go) reads the same budgets.txt.
package perfbudget

import (
	_ "embed"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

//go:embed budgets.txt
var raw string

// Budget is one line of budgets.txt.
type Budget struct {
	Name  string
	Limit float64
	Unit  string
}

// Parse reads budgets.txt: "name limit unit", "#" starts a comment.
func Parse(s string) (map[string]Budget, error) {
	out := map[string]Budget{}
	for i, line := range strings.Split(s, "\n") {
		if j := strings.IndexByte(line, '#'); j >= 0 {
			line = line[:j]
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) != 3 {
			return nil, fmt.Errorf("budgets.txt:%d: want \"name limit unit\", got %q", i+1, line)
		}
		lim, err := strconv.ParseFloat(f[1], 64)
		if err != nil {
			return nil, fmt.Errorf("budgets.txt:%d: bad limit %q", i+1, f[1])
		}
		if _, dup := out[f[0]]; dup {
			return nil, fmt.Errorf("budgets.txt:%d: %s listed twice", i+1, f[0])
		}
		out[f[0]] = Budget{Name: f[0], Limit: lim, Unit: f[2]}
	}
	return out, nil
}

func enabled(short bool) bool { return !short && os.Getenv("TERMOCODE_PERF") == "1" }

// Require skips the test unless perf guards are on (TERMOCODE_PERF=1,
// no -short).
func Require(t testing.TB) {
	t.Helper()
	if !enabled(testing.Short()) {
		t.Skip("perf guard: set TERMOCODE_PERF=1 (and no -short) to run")
	}
}

// T is the part of testing.TB that Check uses.
type T interface {
	Helper()
	Logf(format string, args ...any)
	Errorf(format string, args ...any)
}

// Check compares got (in the budget's unit) with the budget name.
func Check(t T, name string, got float64) {
	t.Helper()
	bs, err := Parse(raw)
	if err != nil {
		t.Errorf("%v", err)
		return
	}
	check(t, bs, name, got)
}

func check(t T, bs map[string]Budget, name string, got float64) {
	t.Helper()
	b, ok := bs[name]
	if !ok {
		t.Errorf("perf: no budget %q in internal/perfbudget/budgets.txt", name)
		return
	}
	status := "ok"
	if got > b.Limit {
		status = "OVER"
	}
	g := strconv.FormatFloat(got, 'f', -1, 64)
	l := strconv.FormatFloat(b.Limit, 'f', -1, 64)
	t.Logf("perf: %s = %s %s (budget %s %s)", name, g, b.Unit, l, b.Unit)
	if p := os.Getenv("TERMOCODE_PERF_OUT"); p != "" {
		if f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(f, "%s\t%s\t%s\t%s\t%s\n", name, g, l, b.Unit, status)
			_ = f.Close()
		}
	}
	if status != "ok" {
		t.Errorf("perf: %s = %s %s is over its budget %s %s (internal/perfbudget/budgets.txt)", name, g, b.Unit, l, b.Unit)
	}
}
