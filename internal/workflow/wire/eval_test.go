package wire

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/workflow/run"
)

var _ run.Evaluator = Eval{}

// Spec (WFX wiring): the expr package is the engine's Evaluator: Eval
// gives plain JSON values, Test the truth, Render text and RenderJSON
// keeps the JSON type of a lone {{…}}; seen() asks the seen hook.
func TestEvalFitsTheEngine(t *testing.T) {
	seen := map[string]bool{"LIN-1": true}
	e := Eval{Seen: func(k string) (bool, error) { return seen[k], nil }}
	scope := map[string]any{"item": map[string]any{"id": "LIN-1", "n": 3.0, "title": "Crash"}, "vars": map[string]any{"base": "main"}}

	if v, err := e.Eval("item.n + 1", scope); err != nil || v != 4.0 {
		t.Fatalf("Eval = %v, %v", v, err)
	}
	if ok, err := e.Test("seen(item.id) and item.title startswith \"Cr\"", scope); err != nil || !ok {
		t.Fatalf("Test = %v, %v", ok, err)
	}
	if ok, err := e.Test("item.missing", scope); err != nil || ok {
		t.Fatalf("missing path is false: %v, %v", ok, err)
	}
	if s, err := e.Render("fix: {{item.title}} on {{vars.base}}", scope); err != nil || s != "fix: Crash on main" {
		t.Fatalf("Render = %q, %v", s, err)
	}
	got, err := e.RenderJSON(map[string]any{"n": "{{item.n}}", "t": "x {{item.id}}"}, scope)
	m, _ := got.(map[string]any)
	if err != nil || m["n"] != 3.0 || m["t"] != "x LIN-1" {
		t.Fatalf("RenderJSON = %#v, %v", got, err)
	}
	if _, err := e.Eval("item.(", scope); err == nil {
		t.Fatal("a bad expression is an error")
	}
}
