package expr

import (
	"reflect"
	"strings"
	"testing"
)

// scope is a small JSON-shaped scope shared by the tests.
func testScope() map[string]any {
	return map[string]any{
		"item": map[string]any{
			"id":     "LIN-228",
			"title":  "Crash on save",
			"prio":   float64(2),
			"labels": []any{"bug", "ui"},
			"plan":   map[string]any{"skip": false, "rank": float64(1)},
			"none":   nil,
			"desc":   "Steps to reproduce: open, save",
		},
		"vars":  map[string]any{"base": "main", "n": 3},
		"steps": map[string]any{"ready": map[string]any{"items": []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}}}},
		"run":   map[string]any{"dry": true, "date": "2026-11-05"},
	}
}

func mustEval(t *testing.T, src string) any {
	t.Helper()
	v, err := Eval(src, testScope())
	if err != nil {
		t.Fatalf("Eval(%q): %v", src, err)
	}
	return v.Any()
}

func TestEvalLiteralsAndPaths(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		{`1`, float64(1)},
		{`2.5`, 2.5},
		{`-3`, float64(-3)},
		{`"a\"b"`, `a"b`},
		{`'single'`, "single"},
		{`"tab\there"`, "tab\there"},
		{`item["title"]`, "Crash on save"},
		{`true`, true},
		{`false`, false},
		{`null`, nil},
		{`[1, "x", null]`, []any{float64(1), "x", nil}},
		{`[]`, []any{}},
		{`item.id`, "LIN-228"},
		{`item.plan.rank`, float64(1)},
		{`item.labels[1]`, "ui"},
		{`steps.ready.items[0].id`, "a"},
		{`vars.n`, float64(3)}, // Go ints become JSON numbers
		{`run.dry`, true},
		{`(item.id)`, "LIN-228"},
	}
	for _, c := range cases {
		if got := mustEval(t, c.src); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Eval(%q) = %#v, want %#v", c.src, got, c.want)
		}
	}
}

func TestEvalMissingPathsAreNull(t *testing.T) {
	for _, src := range []string{
		`item.nope`,
		`item.nope.deeper.still`,
		`item.labels[9]`,
		`item.id.sub`,      // field of a string
		`item.labels.name`, // field of a list
		`item.plan[0]`,     // index of a map
		`item.none.x`,
		`steps.missing.items[0].id`,
		`items`,
		`workflow.name`,
	} {
		if got := mustEval(t, src); got != nil {
			t.Errorf("Eval(%q) = %#v, want nil", src, got)
		}
	}
	if v, err := Eval(`item.id`, nil); err != nil || v.Any() != nil {
		t.Errorf("Eval on a nil scope = %#v, %v; want nil, nil", v.Any(), err)
	}
}

func TestEvalParseErrors(t *testing.T) {
	for _, src := range []string{
		``,
		`   `,
		`item.`,
		`item[`,
		`item[-1]`,
		`item[1.5]`,
		`foo`,        // unknown root
		`env.HOME`,   // no access to the environment
		`os.exit(1)`, // no arbitrary code
		`item = 1`,   // no assignment
		`(1`,
		`[1,`,
		`"open`,
		`1 2`,
		`exec("ls")`, // not one of the functions
		`len`,
		`@`,
		`'it''s'`, // two strings side by side
		`"bad \q escape"`,
	} {
		_, err := Eval(src, testScope())
		if err == nil {
			t.Errorf("Eval(%q) = no error, want one", src)
			continue
		}
		if len(err.Error()) > 300 {
			t.Errorf("Eval(%q) error too long: %d chars", src, len(err.Error()))
		}
	}
}

func TestEvalSizeCap(t *testing.T) {
	long := "item.id" + strings.Repeat(" ", MaxExprLen)
	if _, err := Eval(long, testScope()); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("Eval(long) err = %v, want too long", err)
	}
	ok := strings.Repeat(" ", MaxExprLen-len("1")) + "1"
	if _, err := Eval(ok, testScope()); err != nil {
		t.Fatalf("Eval(exactly %d bytes) = %v", MaxExprLen, err)
	}
}

func TestEvalNestingCap(t *testing.T) {
	deep := strings.Repeat("(", 200) + "1" + strings.Repeat(")", 200)
	if _, err := Eval(deep, nil); err == nil || !strings.Contains(err.Error(), "nested") {
		t.Fatalf("Eval(deep) err = %v, want nested too deep", err)
	}
	notNots := strings.Repeat("not ", 300) + "true"
	if _, err := Eval(notNots, nil); err == nil {
		t.Fatal("300 nots: want nested too deep")
	}
}
