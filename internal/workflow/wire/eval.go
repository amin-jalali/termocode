// Package wire fits the workflow parts together (plan WFX §4): the expr
// package as the engine's Evaluator, the source package as its Source,
// the ledger and a gate as its Writer, aistep as its Planner and Runner,
// and an atomic run file store. The app passes in what only it has (the
// MCP manager, the database sessions, the cards).
package wire

import (
	"time"

	"github.com/amin-jalali/termocode/internal/workflow/expr"
)

// Eval is the run.Evaluator over the expr package.
type Eval struct {
	// Seen backs seen(key) (the workflow's seen ledger); nil: seen() fails.
	Seen func(key string) (bool, error)
	// Now is days_since's clock (nil: time.Now).
	Now func() time.Time
}

func (e Eval) env() *expr.Env { return &expr.Env{Seen: e.Seen, Now: e.Now} }

// Eval returns the value of src as plain JSON types.
func (e Eval) Eval(src string, scope map[string]any) (any, error) {
	v, err := expr.EvalEnv(src, scope, e.env())
	if err != nil {
		return nil, err
	}
	return v.Any(), nil
}

// Test returns the truth of src.
func (e Eval) Test(src string, scope map[string]any) (bool, error) {
	v, err := expr.EvalEnv(src, scope, e.env())
	if err != nil {
		return false, err
	}
	return v.Truthy(), nil
}

// Render fills the {{…}} templates of tmpl with text.
func (e Eval) Render(tmpl string, scope map[string]any) (string, error) {
	return expr.Render(tmpl, scope, e.env())
}

// RenderJSON fills the templates in a JSON value.
func (e Eval) RenderJSON(v any, scope map[string]any) (any, error) {
	return expr.RenderJSON(v, scope, e.env())
}
