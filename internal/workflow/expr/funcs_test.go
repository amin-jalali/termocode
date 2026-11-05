package expr

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEvalFunctions(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	seenKeys := map[string]bool{"LIN-228": true}
	env := &Env{
		Now:  func() time.Time { return now },
		Seen: func(k string) (bool, error) { return seenKeys[k], nil },
	}
	scope := testScope()
	scope["vars"].(map[string]any)["when"] = "2026-11-03T12:00:00Z"
	scope["vars"].(map[string]any)["day"] = "2026-11-04"
	scope["vars"].(map[string]any)["pad"] = "  hi \n"
	cases := []struct {
		src  string
		want any
	}{
		{`len(item.labels)`, float64(2)},
		{`len(item.plan)`, float64(2)},
		{`len("héllo")`, float64(5)}, // characters, not bytes
		{`len(item.nope)`, float64(0)},
		{`len([])`, float64(0)},
		{`lower(item.id)`, "lin-228"},
		{`upper("abc")`, "ABC"},
		{`trim(vars.pad)`, "hi"},
		{`lower(item.nope)`, ""},
		{`has(item.none)`, true}, // present, even though its value is null
		{`has(item.nope)`, false},
		{`has(item.labels[1])`, true},
		{`has(item.labels[5])`, false},
		{`has(steps.ready.items)`, true},
		{`not has(item.plan.labels)`, true},
		{`seen(item.id)`, true},
		{`seen("LIN-1")`, false},
		{`days_since(vars.when)`, float64(2)},
		{`days_since(vars.day)`, 1.5},
		{`days_since(item.nope)`, nil},
		{`days_since(vars.when) > 1`, true},
		{`json(item.labels)`, `["bug","ui"]`},
		{`json("a")`, `"a"`},
		{`json(null)`, `null`},
		{`json(2)`, `2`},
		{`len(json(item.plan)) > 0`, true},
		{`lower(upper(trim(" x ")))`, "x"},
		{`item.labels has lower("BUG")`, true},
	}
	for _, c := range cases {
		got, err := EvalEnv(c.src, scope, env)
		if err != nil {
			t.Errorf("Eval(%q): %v", c.src, err)
			continue
		}
		if !reflect.DeepEqual(got.Any(), c.want) {
			t.Errorf("Eval(%q) = %#v, want %#v", c.src, got.Any(), c.want)
		}
	}
}

func TestEvalFunctionErrors(t *testing.T) {
	failing := &Env{Seen: func(string) (bool, error) { return false, errors.New("ledger is closed") }}
	cases := []struct {
		src     string
		env     *Env
		wantErr string
	}{
		{`len()`, nil, "len takes 1 argument"},
		{`len(1, 2)`, nil, "len takes 1 argument"},
		{`len(1)`, nil, "len needs a string, list or map"},
		{`lower(1)`, nil, "lower needs a string"},
		{`has("x")`, nil, "has( ) needs a path"},
		{`has(item.a, item.b)`, nil, "has takes 1 argument"},
		{`seen("a")`, nil, "seen() is not available here"},
		{`seen("a")`, failing, "ledger is closed"},
		{`seen([1])`, &Env{Seen: func(string) (bool, error) { return true, nil }}, "seen needs a string"},
		{`days_since("soon")`, nil, "days_since needs a date"},
		{`days_since(3)`, nil, "days_since needs a date"},
		{`len (1`, nil, "want"},
		{`exec(1)`, nil, "unknown name"},
		{`item.id(1)`, nil, "unexpected"},
	}
	for _, c := range cases {
		_, err := EvalEnv(c.src, testScope(), c.env)
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("Eval(%q) err = %v, want %q", c.src, err, c.wantErr)
		}
	}
}

func TestDaysSinceUsesRealClockByDefault(t *testing.T) {
	scope := map[string]any{"vars": map[string]any{"t": time.Now().Add(-48 * time.Hour).Format(time.RFC3339)}}
	v, err := Eval(`days_since(vars.t)`, scope)
	if err != nil {
		t.Fatal(err)
	}
	if d := v.Any().(float64); d < 1.9 || d > 2.1 {
		t.Fatalf("days_since = %v, want about 2", d)
	}
}
