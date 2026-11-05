package expr

import (
	"reflect"
	"strings"
	"testing"
)

func TestEvalOperators(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		// or / and / not, short-circuit, results are bools
		{`true or false`, true},
		{`false or false`, false},
		{`true and false`, false},
		{`true and true`, true},
		{`not true`, false},
		{`not not true`, true},
		{`not item.nope`, true},
		{`item.id and item.prio`, true},
		{`"" or 0`, false},
		{`false and item.labels[0] matches "("`, false}, // right side never runs
		{`true or item.labels[0] matches "("`, true},
		{`not item.plan.skip and item.desc matches "(?i)steps to reproduce"`, true},
		{`true or false and false`, true}, // and binds tighter than or
		{`not 1 == 2`, true},              // not wraps the comparison
		// == / !=, deep JSON equality
		{`1 == 1`, true},
		{`1 == 1.0`, true},
		{`vars.n == 3`, true},
		{`"a" == "a"`, true},
		{`"1" == 1`, false},
		{`null == null`, true},
		{`item.nope == null`, true},
		{`item.labels == ["bug", "ui"]`, true},
		{`item.labels == ["ui", "bug"]`, false},
		{`item.plan == item.plan`, true},
		{`1 != 2`, true},
		{`null != false`, true},
		// < <= > >=
		{`1 < 2`, true},
		{`2 <= 2`, true},
		{`3 > 2`, true},
		{`2 >= 3`, false},
		{`"a" < "b"`, true},
		{`"b" >= "a"`, true},
		{`item.nope < 3`, false}, // null never orders
		{`item.nope >= 3`, false},
		{`3 > null`, false},
		// has / in
		{`item.labels has "bug"`, true},
		{`item.labels has "x"`, false},
		{`"ui" in item.labels`, true},
		{`item.plan has "rank"`, true},
		{`"skip" in item.plan`, true},
		{`"zzz" in item.plan`, false},
		{`item.title has "save"`, true},
		{`"Crash" in item.title`, true},
		{`item.nope has "x"`, false},
		{`[1, [2]] has [2]`, true},
		{`2 in [1, 2.0]`, true},
		// matches / startswith / endswith
		{`item.id matches "^LIN-[0-9]+$"`, true},
		{`item.id matches "^x"`, false},
		{`item.nope matches "x"`, false},
		{`item.id startswith "LIN"`, true},
		{`item.id endswith "228"`, true},
		{`item.id endswith "LIN"`, false},
		{`item.nope startswith "a"`, false},
		// + and -
		{`1 + 2`, float64(3)},
		{`5 - 2 - 1`, float64(2)},
		{`"a" + "b"`, "ab"},
		{`"n=" + 3`, "n=3"},
		{`2.5 + "x"`, "2.5x"},
		{`"x" + item.nope`, "x"},
		{`[1] + [2]`, []any{float64(1), float64(2)}},
		{`item.prio + 1 > 2`, true}, // sum binds tighter than compare
	}
	for _, c := range cases {
		got, err := Eval(c.src, testScope())
		if err != nil {
			t.Errorf("Eval(%q): %v", c.src, err)
			continue
		}
		if !reflect.DeepEqual(got.Any(), c.want) {
			t.Errorf("Eval(%q) = %#v, want %#v", c.src, got.Any(), c.want)
		}
	}
}

func TestEvalTypeMixErrors(t *testing.T) {
	cases := []struct{ src, wantErr string }{
		{`1 < "a"`, "cannot compare number and string"},
		{`true > false`, "cannot compare bool and bool"},
		{`[1] < [2]`, "cannot compare list and list"},
		{`1 - "a"`, "\"-\" needs numbers"},
		{`item.nope - 1`, "\"-\" needs numbers"},
		{`1 + true`, "cannot add number and bool"},
		{`item.plan + 1`, "cannot add map and number"},
		{`1 has 1`, "\"has\" needs a list, map or string on the left"},
		{`item.plan has 1`, "a map key must be a string"},
		{`item.title has 1`, "needs a string on the right"},
		{`item.prio matches "1"`, "\"matches\" needs a string on the left"},
		{`item.id matches 1`, "the pattern must be a string"},
		{`item.id matches "("`, "bad pattern"},
		{`item.id matches "(?=x)"`, "bad pattern"}, // no lookahead: RE2 only
		{`item.id startswith 1`, "needs strings"},
		{`item.prio endswith "2"`, "needs strings"},
	}
	for _, c := range cases {
		_, err := Eval(c.src, testScope())
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("Eval(%q) err = %v, want %q", c.src, err, c.wantErr)
		}
	}
}

func TestMatchesPatternCap(t *testing.T) {
	scope := map[string]any{"vars": map[string]any{"p": strings.Repeat("a", MaxExprLen+1)}}
	_, err := Eval(`"aaa" matches vars.p`, scope)
	if err == nil || !strings.Contains(err.Error(), "pattern is too long") {
		t.Fatalf("err = %v, want pattern is too long", err)
	}
	scope["vars"] = map[string]any{"p": strings.Repeat("a", MaxExprLen)}
	if _, err := Eval(`"aaa" matches vars.p`, scope); err != nil {
		t.Fatalf("a 1 KB pattern: %v", err)
	}
}

func TestStringResultCap(t *testing.T) {
	big := strings.Repeat("x", MaxStringLen/2+1)
	scope := map[string]any{"vars": map[string]any{"s": big}}
	_, err := Eval(`vars.s + vars.s`, scope)
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("err = %v, want too long", err)
	}
}

func TestBadPatternErrorHasOnePrefix(t *testing.T) {
	_, err := Eval(`item.id matches "("`, testScope())
	if err == nil || strings.Count(err.Error(), "expr:") != 1 {
		t.Fatalf("err = %v, want one expr: prefix", err)
	}
}

// A scope built in Go (not from JSON) may hold a cycle; == and has must
// still end instead of running out of stack.
func TestEqualStopsOnACycle(t *testing.T) {
	a := map[string]any{}
	a["self"] = a
	b := map[string]any{}
	b["self"] = b
	scope := map[string]any{"item": a, "vars": b}
	for _, src := range []string{`item == vars`, `[item] has vars`, `json(item)`, `"" + json(item)`} {
		_, _ = Eval(src, scope) // must return
	}
}
