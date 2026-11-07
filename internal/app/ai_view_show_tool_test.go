package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/blocks"
	"github.com/amin-jalali/termocode/internal/views"
)

// fakeViewRunner is a registry of one DB orders view and one view over
// a mutating tool; RunView renders fixed rows with the real renderer.
type fakeViewRunner struct {
	defs map[string]*views.Def
	rows []views.Row
	ran  []blocks.ViewSpec
	err  error
}

func newFakeViewRunner(n int) *fakeViewRunner {
	orders := &views.Def{
		ID: "db.rows", Feature: "db", Title: "Table rows",
		Source: views.Source{Op: "db.rows"},
		Params: []views.Param{{ID: "table", Label: "Table", Kind: views.ParamTable, Required: true}},
		Fields: []views.Field{
			{ID: "id", Label: "ID", Kind: views.FieldNum, Sortable: true, Filterable: true, Default: true},
			{ID: "status", Label: "Status", Kind: views.FieldState, Sortable: true, Filterable: true, Default: true},
			{ID: "total", Label: "Total", Kind: views.FieldCost, Sortable: true, Filterable: true, Default: true},
			{ID: "note", Label: "Note", Kind: views.FieldText, Filterable: true},
		},
		Layouts: []string{blocks.KindTable, blocks.KindList},
		Role:    "viewer",
	}
	writer := &views.Def{
		ID: "mcp.shop.refund", Feature: "mcp", Title: "Refund",
		Source:  views.Source{MCPServer: "shop", MCPTool: "refund"},
		Fields:  []views.Field{{ID: "ok", Label: "OK", Kind: views.FieldBool, Default: true}},
		Layouts: []string{blocks.KindCard},
		Role:    "viewer",
	}
	r := &fakeViewRunner{defs: map[string]*views.Def{orders.ID: orders, writer.ID: writer}}
	for i := 1; i <= n; i++ {
		st := "paid"
		if i%3 == 0 {
			st = "open"
		}
		r.rows = append(r.rows, views.Row{ID: fmt.Sprint(i), Cells: map[string]views.Cell{
			"id": {N: float64(i)}, "status": {S: st}, "total": {N: float64(i) * 10}, "note": {S: "row " + fmt.Sprint(i)},
		}})
	}
	return r
}

func (r *fakeViewRunner) ViewDef(id string) (*views.Def, bool) { d, ok := r.defs[id]; return d, ok }
func (r *fakeViewRunner) ViewIDs() []string                    { return []string{"db.rows", "mcp.shop.refund"} }
func (r *fakeViewRunner) CanShow(d *views.Def) bool            { return d.Source.MCPTool == "" }
func (r *fakeViewRunner) RunView(_ context.Context, s blocks.ViewSpec) (blocks.Block, error) {
	r.ran = append(r.ran, s)
	if r.err != nil {
		return blocks.Block{}, r.err
	}
	return views.Render("call-1", r.defs[s.View], s, r.rows, false, 0)
}

func viewShowTool(t *testing.T, r aiViewRunner) ai.Tool {
	t.Helper()
	ts := aiViewShowTools(r)
	if len(ts) != 1 || ts[0].Name != "view_show" {
		t.Fatalf("tools = %+v", ts)
	}
	return ts[0]
}

func ordersSpec() map[string]any {
	return map[string]any{"spec": map[string]any{
		"view": "db.rows", "args": map[string]any{"table": "orders"},
		"fields": []any{"status", "total"},
		"filter": []any{map[string]any{"field": "status", "op": "eq", "value": "paid"}},
		"sort":   []any{map[string]any{"field": "total", "desc": true}},
		"layout": "table", "title": "Paid orders",
	}}
}

// No phone page, no tool: the AI cannot show a view nobody can see.
func TestAIViewShowOffWithoutRunner(t *testing.T) {
	if ts := aiViewShowTools(nil); len(ts) != 0 {
		t.Fatalf("tools without a runner = %+v", ts)
	}
}

// MOBUX-17: "orders table, only status and total" → the spec is checked
// and filled on the laptop, run, the block goes to the View hook with
// the spec it was built from, and the model gets a short text.
func TestAIViewShowBuildsBlockAndText(t *testing.T) {
	r := newFakeViewRunner(30)
	tool := viewShowTool(t, r)
	if !tool.ReadOnly || tool.Mutating {
		t.Fatalf("view_show must only read: %+v", tool)
	}
	a := ordersSpec()
	if err := tool.Validate(a); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got := tool.Describe(a); got != "Show view Paid orders" {
		t.Errorf("Describe = %q", got)
	}
	out, err := tool.Run(context.Background(), a)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(r.ran) != 1 || r.ran[0].Args["table"] != "orders" || r.ran[0].Limit == 0 {
		t.Fatalf("ran = %+v (want a resolved spec)", r.ran)
	}
	for _, want := range []string{`"Paid orders" (db.rows)`, "Built: table · Status, Total · Status = paid · Total ↓", "Status | Total", "⚙"} {
		if !strings.Contains(out, want) {
			t.Errorf("text has no %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "row 1") {
		t.Errorf("a field the spec left out reached the model:\n%s", out)
	}
	b := tool.View(a, out)
	if b == nil || b.Spec == nil || b.Spec.View != "db.rows" || b.Source != "db.rows" {
		t.Fatalf("block = %+v", b)
	}
	if len(b.Spec.Fields) != 2 || b.Spec.Title != "Paid orders" {
		t.Errorf("block spec = %+v (the customizer edits this)", b.Spec)
	}
	if again := tool.View(a, out); again != nil {
		t.Errorf("the block was handed out twice")
	}
}

// The model sees at most 20 rows; the rest stays a count.
func TestAIViewShowTextCapsRows(t *testing.T) {
	r := newFakeViewRunner(90)
	tool := viewShowTool(t, r)
	a := map[string]any{"spec": map[string]any{"view": "db.rows", "args": map[string]any{"table": "orders"}, "limit": 60}}
	out, err := tool.Run(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	tool.Release(a)
	lines := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.Count(l, " | ") == 2 && !strings.HasPrefix(l, "ID |") {
			lines++
		}
	}
	if lines != aiViewShowRows {
		t.Errorf("row lines = %d, want %d:\n%s", lines, aiViewShowRows, out)
	}
	if !strings.Contains(out, "more on the phone") {
		t.Errorf("no count of the rest:\n%s", out)
	}
}

// Bad specs are refused before the run with text the model can fix from.
func TestAIViewShowRefusesBadSpecs(t *testing.T) {
	r := newFakeViewRunner(3)
	tool := viewShowTool(t, r)
	cases := map[string]struct {
		args map[string]any
		want string
	}{
		"no spec":      {map[string]any{}, "spec is required"},
		"not object":   {map[string]any{"spec": 3}, "object"},
		"unknown view": {map[string]any{"spec": map[string]any{"view": "orders"}}, "db.rows, mcp.shop.refund"},
		"unknown key":  {map[string]any{"spec": map[string]any{"view": "db.rows", "sql": "drop table x"}}, "unknown field"},
		"bad field":    {map[string]any{"spec": map[string]any{"view": "db.rows", "args": map[string]any{"table": "orders"}, "fields": []any{"secret"}}}, "views: invalid"},
		"writer view":  {map[string]any{"spec": map[string]any{"view": "mcp.shop.refund"}}, "use the tool itself"},
	}
	for name, c := range cases {
		err := tool.Validate(c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
		if _, err := tool.Run(context.Background(), c.args); err == nil {
			t.Errorf("%s: Run ran", name)
		}
	}
	if len(r.ran) != 0 {
		t.Errorf("a refused spec reached the runner: %+v", r.ran)
	}
}

// A spec sent as a JSON string works too; a failed run leaves no block.
func TestAIViewShowStringSpecAndRunError(t *testing.T) {
	r := newFakeViewRunner(2)
	tool := viewShowTool(t, r)
	a := map[string]any{"spec": `{"view":"db.rows","args":{"table":"orders"}}`}
	if err := tool.Validate(a); err != nil {
		t.Fatalf("string spec: %v", err)
	}
	r.err = errors.New("laptop asleep")
	if _, err := tool.Run(context.Background(), a); err == nil || !strings.Contains(err.Error(), "laptop asleep") {
		t.Fatalf("Run err = %v", err)
	}
	if b := tool.View(a, ""); b != nil {
		t.Errorf("failed run left a block: %+v", b)
	}
}

// The spec chip line names what was built in the user's words.
func TestAIViewSpecSummary(t *testing.T) {
	d, _ := newFakeViewRunner(0).ViewDef("db.rows")
	s := blocks.ViewSpec{View: "db.rows", Layout: "list", Fields: []string{"status", "note"},
		Filter: []blocks.ViewCond{{Field: "status", Op: "in", In: []string{"open", "paid"}}, {Field: "note", Op: "not_empty"}},
		Sort:   []blocks.ViewSort{{Field: "id"}}, Group: "status", Limit: 20, Density: "compact"}
	want := "list · Status, Note · Status in open|paid · Note is set · ID ↑ · by Status · 20 rows · compact"
	if got := aiViewSpecSummary(d, s); got != want {
		t.Errorf("summary = %q\nwant      %q", got, want)
	}
}
