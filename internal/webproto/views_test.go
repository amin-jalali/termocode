package webproto

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// v1Views are the views of mobile plan 3.11.8 marked v1 (MOB-9).
var v1Views = []string{"git.status", "git.log", "git.stash", "git.branches", "git.tags", "git.diff",
	"lanes", "lanes.agent", "runs", "workflows", "workflows.runs", "workflow.tail", "tasks", "task.tail",
	"projects", "inbox", "cards", "chats", "pr.list", "pr", "ci.checks", "mcp.status"}

// Spec (MOB-9): every v1 view is in the registry with rows, in menu
// (feature) order, with at least one layout and typed fields.
func TestViewRegistryV1(t *testing.T) {
	for _, id := range v1Views {
		d, ok := LookupView(id)
		if !ok {
			t.Errorf("%s: not registered", id)
			continue
		}
		if viewRows[id] == nil {
			t.Errorf("%s: no rows func", id)
		}
		if len(d.Fields) == 0 || len(d.Layouts) == 0 || d.Title == "" {
			t.Errorf("%s: fields %d layouts %d title %q", id, len(d.Fields), len(d.Layouts), d.Title)
		}
	}
	last := -1
	for _, d := range Views() {
		i := slices.Index(ViewFeatures, d.Feature)
		if i < last {
			t.Fatalf("%s (%s) out of feature order", d.ID, d.Feature)
		}
		last = i
	}
}

// Spec (MOB-9, policy walk): a view's role is never lower than its
// source op's; a view a role may not run is not in its catalog; no view
// reads through an op that waits for a laptop card; topic views are live.
func TestViewPolicyWalk(t *testing.T) {
	for _, d := range Views() {
		if d.Source.Op != "" {
			p, _ := Lookup(d.Source.Op)
			if p.Role == RoleController && d.Role != RoleController {
				t.Errorf("%s: role %s below its op %s (%s)", d.ID, d.Role, p.Op, p.Role)
			}
			if p.LaptopCard || p.Gateway {
				t.Errorf("%s: reads through %s, which is not a plain read", d.ID, p.Op)
			}
		}
		if d.Source.Topic != "" && !d.Live {
			t.Errorf("%s: a topic view that is not live", d.ID)
		}
		for _, a := range d.Actions {
			if a.Kind == "op" {
				if _, ok := Lookup(a.Op); !ok {
					t.Errorf("%s: action names unknown op %s", d.ID, a.Op)
				}
			}
		}
	}
	for _, d := range ViewsFor(RoleViewer) {
		if d.Role != RoleViewer {
			t.Errorf("viewer catalog lists %s (%s)", d.ID, d.Role)
		}
	}
	for _, op := range []string{OpViewRun, OpViewStop, OpViewCatalog} {
		if p, ok := Lookup(op); !ok || p.Role != RoleViewer {
			t.Errorf("%s: %+v", op, p)
		}
	}
}

// Spec (MOB-9): a spec with an unknown field, param, layout or sort key
// is refused, and the message names the allowed ids (so the AI or a
// hand-made link can fix it). gt on a text field is refused too.
func TestCheckViewSpecRefusesWithAllowedIDs(t *testing.T) {
	d, _ := LookupView("pr.list")
	for _, c := range []struct {
		spec ViewSpec
		want string
	}{
		{ViewSpec{View: "pr.list", Fields: []string{"nope"}}, "number, title, checks"},
		{ViewSpec{View: "pr.list", Layout: "board"}, "table, list"},
		{ViewSpec{View: "pr.list", Sort: []ViewSort{{Field: "nope"}}}, "sortable: number"},
		{ViewSpec{View: "pr.list", Args: map[string]string{"x": "1"}}, "takes no params"},
		{ViewSpec{View: "pr.list", Filter: []ViewCond{{Field: "title", Op: "gt", Value: "1"}}}, "num, time or cost"},
		{ViewSpec{View: "pr.list", Refresh: 10}, "at least 60"},
	} {
		err := CheckViewSpec(&c.spec, d)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v, want %q", c.spec, err, c.want)
		}
	}
	pr, _ := LookupView("pr")
	if err := CheckViewSpec(&ViewSpec{View: "pr"}, pr); err == nil || !strings.Contains(err.Error(), "number is needed") {
		t.Errorf("pr without its number: %v", err)
	}
	if err := CheckViewSpec(&ViewSpec{View: "pr", Args: map[string]string{"number": "x"}}, pr); err == nil {
		t.Error("pr number x was accepted")
	}
	git, _ := LookupView("git.status")
	if err := CheckViewSpec(&ViewSpec{View: "git.status", Refresh: 30}, git); err == nil {
		t.Error("a refresh on a live view was accepted")
	}
	diff, _ := LookupView("git.diff")
	if err := CheckViewSpec(&ViewSpec{View: "git.diff", Args: map[string]string{"path": "/etc/passwd"}}, diff); err == nil {
		t.Error("an absolute path was accepted")
	}
}

// viewSubset reports whether got holds everything in want: objects by
// key (extra keys allowed), arrays element by element with equal length.
func viewSubset(want, got any) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range w {
			if !viewSubset(v, g[k]) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !viewSubset(w[i], g[i]) {
				return false
			}
		}
		return true
	}
	wb, _ := json.Marshal(want)
	gb, _ := json.Marshal(got)
	return string(wb) == string(gb)
}

// Spec (MOB-9): the fixtures (testdata/views/*.json: a spec, the source's
// JSON and the block it makes) hold: one implementation of a spec, on the
// laptop. Each block passes Block.Validate.
func TestViewFixtures(t *testing.T) {
	paths, _ := filepath.Glob(filepath.Join("testdata", "views", "*.json"))
	if len(paths) < 5 {
		t.Fatalf("%d fixtures", len(paths))
	}
	for _, p := range paths {
		t.Run(filepath.Base(p), func(t *testing.T) {
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			var fx struct {
				Spec ViewSpec        `json:"spec"`
				Src  json.RawMessage `json:"src"`
				Want any             `json:"want"`
			}
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}
			d, ok := LookupView(fx.Spec.View)
			if !ok {
				t.Fatalf("unknown view %s", fx.Spec.View)
			}
			if err := CheckViewSpec(&fx.Spec, d); err != nil {
				t.Fatal(err)
			}
			rows, whole, err := ViewRows(d.ID, Effective(fx.Spec, d).Args, fx.Src)
			if err != nil || whole != nil {
				t.Fatalf("rows: %v (whole %v)", err, whole != nil)
			}
			b := ApplyView(d, fx.Spec, rows)
			b.ID = "v-1"
			if err := b.Validate(); err != nil {
				t.Fatalf("block: %v", err)
			}
			bb, _ := json.Marshal(b)
			var got any
			_ = json.Unmarshal(bb, &got)
			if !viewSubset(fx.Want, got) {
				t.Errorf("got %s", bb)
			}
		})
	}
}

// Spec (MOB-9): limit, Total and Truncated; a block over the size cap
// loses rows and still passes Validate; group past MaxGroups → "Other".
func TestApplyViewLimitTotalAndCaps(t *testing.T) {
	d, _ := LookupView("chats")
	rows := make([]ViewRow, 1000)
	for i := range rows {
		rows[i] = ViewRow{ID: "c" + strconv.Itoa(i), Cells: map[string]ViewCell{
			"title": {S: strings.Repeat("x", 300) + strconv.Itoa(i)}, "when": {T: int64(i)}, "msgs": {N: float64(i % 70)}}}
	}
	b := ApplyView(d, ViewSpec{View: "chats", Limit: 200}, rows)
	b.ID = "v-1"
	if b.Total != 1000 || !b.Truncated || len(b.Items) == 0 || len(b.Items) > 200 {
		t.Fatalf("total %d truncated %v items %d", b.Total, b.Truncated, len(b.Items))
	}
	if b.Items[0].Time != 999 {
		t.Errorf("default sort (newest first): first %d", b.Items[0].Time)
	}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	g := ApplyView(d, ViewSpec{View: "chats", Group: "msgs", Limit: 200}, rows)
	g.ID = "v-2"
	if len(g.Groups) > 50 || g.Groups[len(g.Groups)-1].Label != "Other" {
		t.Errorf("%d groups, last %q", len(g.Groups), g.Groups[len(g.Groups)-1].Label)
	}
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
}

// Spec (MOB-9): git.diff is one diff block from git.diff's rows.
func TestViewGitDiffBlock(t *testing.T) {
	src := `{"path":"a.go","rows":[{"k":"file","text":"a.go"},{"k":"hunk","text":"@@ -1 +1 @@"},{"k":"del","text":"x","old":1},{"k":"add","text":"y","new":1}],"from":0,"added":1,"removed":1}`
	_, whole, err := ViewRows("git.diff", map[string]string{"path": "a.go"}, json.RawMessage(src))
	if err != nil || whole == nil || whole.Diff == nil || len(whole.Diff.Hunks) != 1 || len(whole.Diff.Hunks[0].Lines) != 2 {
		t.Fatalf("%v %+v", err, whole)
	}
	if err := whole.Validate(); err != nil {
		t.Fatal(err)
	}
}

// FuzzViewSpec: any spec either fails CheckViewSpec or makes a block
// that passes Validate, for every view; nothing panics.
func FuzzViewSpec(f *testing.F) {
	f.Add([]byte(`{"view":"pr.list","filter":[{"field":"checks","op":"eq","value":"fail"}]}`))
	f.Add([]byte(`{"view":"runs","group":"state","layout":"table","limit":3}`))
	f.Add([]byte(`{"view":"lanes","layout":"board"}`))
	rows := map[string][]ViewRow{}
	for _, d := range Views() {
		var rs []ViewRow
		for i := range 30 {
			c := map[string]ViewCell{}
			for _, fl := range d.Fields {
				c[fl.ID] = ViewCell{S: fl.ID + strconv.Itoa(i%4), N: float64(i), T: int64(i), B: i%2 == 0}
			}
			rs = append(rs, ViewRow{ID: "r" + strconv.Itoa(i), Cells: c, Tone: "ok"})
		}
		rows[d.ID] = rs
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		var s ViewSpec
		if json.Unmarshal(raw, &s) != nil {
			return
		}
		d, ok := LookupView(s.View)
		if !ok || CheckViewSpec(&s, d) != nil || slices.Contains(d.Layouts, "diff") {
			return
		}
		b := ApplyView(d, s, rows[d.ID])
		b.ID = "v-1"
		if err := b.Validate(); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	})
}

// Perf guard (MOB-9): one view.run of a 200-row table (filter, sort,
// layout) on the laptop, median of 9.
func TestPerfViewRunTable200(t *testing.T) {
	perfbudget.Require(t)
	d, _ := LookupView("pr.list")
	rows := make([]ViewRow, 200)
	for i := range rows {
		p := PRItem{Number: i + 1, Title: "Change " + strconv.Itoa(i), Author: "a", Branch: "b", Base: "main", Checks: []string{"pass", "fail", "pending"}[i%3]}
		rows[i] = prRow(p)
	}
	spec := ViewSpec{View: "pr.list", Filter: []ViewCond{{Field: "checks", Op: "ne", Value: "pending"}},
		Sort: []ViewSort{{Field: "title"}}, Limit: 200}
	var took []time.Duration
	for range 9 {
		t0 := time.Now()
		b := ApplyView(d, spec, rows)
		took = append(took, time.Since(t0))
		if b.Total == 0 {
			t.Fatal("no rows")
		}
	}
	slices.Sort(took)
	perfbudget.Check(t, "web_view_run_table_200", float64(took[len(took)/2].Microseconds())/1000)
}
