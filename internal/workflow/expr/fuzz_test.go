package expr

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// maxErrLen caps every error text, so a long input is never echoed back.
const maxErrLen = 512

// FuzzEval: any expression against any JSON scope never panics (the
// evaluator runs without the recover guard here), stays fast, and its
// errors stay short. Templates get the same input.
func FuzzEval(f *testing.F) {
	seeds := []string{
		`not item.plan.skip and item.description matches "(?i)steps to reproduce"`,
		`item.labels has "bug" or "x" in item.title`,
		`len(item.labels) + 1 >= 2`,
		`json(steps.a.items[0]) == "{}"`,
		`days_since(run.last_ok_at) > 7`,
		`has(item.x) and seen(item.key)`,
		`[1, "a", null, [true]] + [] == item`,
		`lower(upper(trim(item.t))) startswith 'a' endswith 'b'`,
		`(((((((1)))))))`,
		`item["k"][0].v - 1`,
		`fix: {{item.title}} {{"}}"}}`,
		`{{`,
		strings.Repeat("a", 2000),
	}
	scopes := []string{
		`{"item":{"plan":{"skip":false},"description":"Steps to reproduce","labels":["bug"],"title":"x"}}`,
		`{"item":null,"steps":{"a":{"items":[{}]}},"run":{"last_ok_at":"2026-01-01"}}`,
		`{"item":[1,2,{"k":[{"v":3}]}],"vars":"s"}`,
		`{}`,
		`not json`,
	}
	for i, s := range seeds {
		f.Add(s, scopes[i%len(scopes)])
	}
	env := &Env{
		Now:  func() time.Time { return time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC) },
		Seen: func(k string) (bool, error) { return len(k)%2 == 0, nil },
	}
	f.Fuzz(func(t *testing.T, src, scopeJSON string) {
		var raw any
		if json.Unmarshal([]byte(scopeJSON), &raw) != nil {
			raw = scopeJSON
		}
		scope, ok := raw.(map[string]any)
		if !ok {
			scope = map[string]any{"item": raw, "vars": raw, "steps": raw, "run": raw, "items": raw, "workflow": raw}
		}
		start := time.Now()
		_, err := evalRaw(src, scope, env) // no recover: a panic fails the fuzz
		checkErr(t, "Eval", err)
		_, err = Render(src, scope, env)
		checkErr(t, "Render", err)
		_, err = RenderValue(src, scope, env)
		checkErr(t, "RenderValue", err)
		_ = CheckTemplate(src)
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("too slow: %v for %d bytes", d, len(src))
		}
	})
}

func checkErr(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil && len(err.Error()) > maxErrLen {
		t.Fatalf("%s error is %d chars (limit %d): %.80s…", what, len(err.Error()), maxErrLen, err)
	}
}

func TestErrorsDoNotEchoLongInput(t *testing.T) {
	long := strings.Repeat("a", 1000)
	for _, src := range []string{long, "item." + long[:900] + " " + long[:100], "1 " + long[:1000]} {
		_, err := Eval(src, nil)
		if err == nil {
			t.Fatalf("Eval(%.20q…) = no error", src)
		}
		checkErr(t, "Eval", err)
	}
}
