package feature

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/blocks"
	"github.com/amin-jalali/termocode/internal/views"
	"github.com/amin-jalali/termocode/internal/webproto"
)

func init() { views.Loc = time.UTC }

const now = int64(1_762_500_000_000)

// sampleRows gives each row-built view a few rows from typical source data.
func sampleRows(t *testing.T) map[string][]views.Row {
	t.Helper()
	git := webproto.GitSnap{Repo: true, Branch: webproto.GitBranch{Name: "main"},
		Files: []webproto.GitFile{{Path: "a.go", X: "M", Y: " ", Staged: true}, {Path: "b/", X: "?", Y: "?", Untracked: true}, {Path: "c.go", X: "U", Y: "U", Conflict: true}},
		Log:   []webproto.GitCommit{{Hash: "1a2b3c4", Subject: "fix: x", Author: "amin", When: now - 3600_000, Unpushed: true}}}
	return map[string][]views.Row{
		ViewGitStatus:   GitStatusRows(git),
		ViewGitLog:      GitLogRows(git),
		ViewGitBranches: GitBranchRows(webproto.GitBranches{Current: "main", Branches: []string{"main", "feat/x"}, Remotes: []string{"origin/main"}}),
		ViewLanes: LaneRows(webproto.WorktreesSnap{State: "ready", Worktrees: []webproto.WorktreeItem{
			{ID: "abc123def456", Title: "lane 1", Branch: "feat/x", Where: "~/src/x", Kind: "agent", Group: "active", State: "working", Ahead: 2, Dirty: 3}}}),
		ViewRuns: RunRows(webproto.RunsSnap{Runs: []webproto.RunItem{
			{Key: "r1", Title: "fix the tests", State: "running", Step: 2, Steps: 5, Started: now - 60_000, CostUSD: 0.42, NeedsYou: true},
			{Key: "r2", Title: "docs", State: "done", Started: now - 7200_000}}}),
		ViewWorkflows: WorkflowRows(webproto.WorkflowsSnap{Workflows: []webproto.WorkflowItem{{Name: "nightly", State: "on", When: "weekdays 09:00", Next: now + 3600_000, Last: "done"}, {Name: "manual", State: "off"}}}),
		ViewWorkflowRuns: WorkflowRunRows(webproto.WorkflowsSnap{Runs: []webproto.WorkflowRun{
			{ID: "w1", Workflow: "nightly", State: "running", Started: now - 1000, Items: 4, Failed: 1, Waiting: "write"}}}),
		ViewTasks: TaskRows(webproto.TasksSnap{Tasks: []webproto.TaskItem{
			{Label: "build", Group: "build", Source: "make", Last: &webproto.TaskLast{State: "failed", Code: 2, When: now - 5000, Buf: 3}},
			{Label: "lint", Source: "npm"}}}),
		ViewHTTPHistory: HTTPHistoryRows(webproto.HTTPHistory{Entries: []webproto.HTTPHistItem{
			{ID: "h1", At: now - 1000, Method: "GET", URL: "https://api.example.test/x", Status: 200, ElapsedMS: 31, Size: 900, File: "api.http"},
			{ID: "h2", At: now - 2000, Method: "POST", URL: "https://api.example.test/y", Err: "timeout"}}}),
		ViewHTTPResponse: HTTPResponseRows(webproto.HTTPResponse{Run: 7, Label: "get x", Method: "GET", URL: "https://api.example.test/x", Status: 404, StatusText: "Not Found",
			Headers: []webproto.HTTPHeader{{Name: "Content-Type", Value: "application/json"}}, Body: `{"error":"nope"}`, ContentType: "application/json"}),
		ViewContainers: ContainerRows(webproto.ContainersList{Engine: "docker", Containers: []webproto.ContainerItem{
			{ID: "0123456789ab", Name: "db", Image: "postgres:16", State: "running", Status: "Up 2 hours"}, {ID: "ba9876543210", Name: "old", State: "exited"}}}),
		ViewKubePods: PodRows(webproto.KubeList{Context: "staging", Namespace: "web", Pods: []webproto.KubePod{
			{Name: "api-1", Ready: "1/1", Status: "Running", Restarts: 4, Containers: []string{"api"}}, {Name: "job-2", Ready: "0/1", Status: "CrashLoopBackOff"}}}),
		ViewTests: TestRows(webproto.TestsSnap{State: "idle", Items: []webproto.TestItem{
			{ID: "p:pkg", Kind: "package", Label: "pkg", State: "failed"},
			{ID: "c:pkg/a_test.go::TestA", Kind: "case", Parent: "f:pkg/a_test.go", Label: "TestA", Path: "pkg/a_test.go", Line: 10, State: "failed", Message: "want 1", FailPath: "pkg/a_test.go", FailLine: 12},
			{ID: "c:pkg/a_test.go::TestB", Kind: "case", Label: "TestB", Path: "pkg/a_test.go", State: "passed", Ms: 3}}}),
		ViewCIChecks: CheckRows(webproto.CIStatus{Branch: "main", State: "fail", Runs: []webproto.CIRun{
			{Workflow: "ci", Title: "fix", Status: "completed", Conclusion: "failure", State: "fail"}, {Workflow: "lint", Status: "in_progress", State: "pending"}}}),
		ViewPRList: PRRows(webproto.PRList{Items: []webproto.PRItem{
			{Number: 418, Title: "Add views", Author: "amin", Branch: "feat/x", Base: "main", Review: "REVIEW_REQUIRED", Checks: "pass"}}}),
		ViewUsage: UsageRows([]UsageLine{{Name: "model-a", Calls: 3, In: 1200, Out: 300, Cost: 0.02, Priced: true, Billing: "per token"}, {Name: "cli", Calls: 1, Billing: "subscription"}}),
	}
}

func TestDefsCheck(t *testing.T) {
	seen := map[string]bool{}
	feats := map[string]bool{}
	for _, d := range Defs() {
		if err := d.Check(); err != nil {
			t.Errorf("%s: %v", d.ID, err)
		}
		if seen[d.ID] {
			t.Errorf("%s: listed twice", d.ID)
		}
		seen[d.ID] = true
		feats[d.Feature] = true
		if d.Role != string(webproto.RoleViewer) {
			t.Errorf("%s: role %q, want viewer (every source is a read)", d.ID, d.Role)
		}
		if op := d.Source.Op; op != "" {
			p, ok := webproto.Lookup(op)
			if !ok || p.Role != webproto.RoleViewer || p.LaptopCard {
				t.Errorf("%s: source op %s must be a viewer op with no laptop card", d.ID, op)
			}
		}
	}
	for _, f := range Features() {
		if !feats[f] {
			t.Errorf("feature %s has no view", f)
		}
	}
	if len(feats) != len(Features()) {
		t.Errorf("features with views %v, menu order lists %v", feats, Features())
	}
}

func TestTopicSourcesAreServed(t *testing.T) {
	served := map[string]bool{"git": true, "worktrees": true, "runs": true, "workflows": true, "tasks": true, "tests": true}
	for _, d := range Defs() {
		if d.Source.Topic != "" && !served[d.Source.Topic] {
			t.Errorf("%s: topic %s", d.ID, d.Source.Topic)
		}
		if d.Live != (d.Source.Topic != "") {
			t.Errorf("%s: live is for topic views only", d.ID)
		}
	}
}

func TestLookupIsACopy(t *testing.T) {
	d, ok := Lookup(ViewRuns)
	if !ok {
		t.Fatal("runs not found")
	}
	d.Fields[0].Label = "changed"
	again, _ := Lookup(ViewRuns)
	if again.Fields[0].Label == "changed" {
		t.Fatal("Lookup shares the registry's slices")
	}
	if _, ok := Lookup("nope"); ok {
		t.Fatal("unknown id found")
	}
}

func TestEveryRowViewRenders(t *testing.T) {
	rows := sampleRows(t)
	for _, d := range Defs() {
		if IsText(&d) || d.ID == ViewDBRows {
			continue
		}
		rs, ok := rows[d.ID]
		if !ok {
			t.Errorf("%s: no sample rows", d.ID)
			continue
		}
		for _, r := range rs {
			for k := range r.Cells {
				if _, ok := d.Field(k); !ok {
					t.Errorf("%s: row cell %q is not a field", d.ID, k)
				}
			}
			if r.Ref != nil && strings.HasPrefix(r.Ref.Path, "/") {
				t.Errorf("%s: absolute ref path %q", d.ID, r.Ref.Path)
			}
		}
		for _, layout := range d.Layouts {
			spec, err := views.Resolve(&d, blocks.ViewSpec{View: d.ID, Layout: layout})
			if err != nil {
				t.Errorf("%s/%s: resolve: %v", d.ID, layout, err)
				continue
			}
			blk, err := views.Render("b1", &d, spec, rs, false, now)
			if err != nil {
				t.Errorf("%s/%s: render: %v", d.ID, layout, err)
				continue
			}
			if blk.Kind != layout {
				t.Errorf("%s: kind %s, want %s", d.ID, blk.Kind, layout)
			}
		}
	}
}

func TestGitStatusStates(t *testing.T) {
	rows := sampleRows(t)[ViewGitStatus]
	want := []string{"staged", "untracked", "conflict"}
	for i, w := range want {
		if got := rows[i].Cells["state"].S; got != w {
			t.Errorf("row %d: state %q, want %q", i, got, w)
		}
	}
	if rows[0].Ref == nil || rows[0].Ref.Kind != "file" || rows[0].Ref.Path != "a.go" {
		t.Errorf("ref %+v", rows[0].Ref)
	}
}

func TestRunNeedsYouIsWarn(t *testing.T) {
	rows := sampleRows(t)[ViewRuns]
	if rows[0].Tone != blocks.ToneWarn || rows[1].Tone != "" {
		t.Fatalf("tones %q %q", rows[0].Tone, rows[1].Tone)
	}
	if rows[0].Cells["cost"].N != 0.42 || rows[0].Ref.ID != "r1" {
		t.Fatalf("row %+v", rows[0])
	}
}

func TestTaskNeverRun(t *testing.T) {
	rows := sampleRows(t)[ViewTasks]
	if rows[0].Cells["state"].S != "failed" || rows[0].Cells["code"].N != 2 {
		t.Fatalf("failed task %+v", rows[0].Cells)
	}
	if !rows[1].Cells["state"].Null || !rows[1].Cells["code"].Null {
		t.Fatalf("a task never run has no state or code: %+v", rows[1].Cells)
	}
}

func TestPodAndContainerTones(t *testing.T) {
	pods := sampleRows(t)[ViewKubePods]
	if pods[0].Tone != blocks.ToneWarn {
		t.Errorf("running pod with restarts: tone %q, want warn", pods[0].Tone)
	}
	if pods[1].Tone != "" {
		t.Errorf("crash loop keeps its state tone, got override %q", pods[1].Tone)
	}
	cs := sampleRows(t)[ViewContainers]
	if cs[0].Tone != blocks.ToneOK || cs[1].Tone != "" {
		t.Errorf("container tones %q %q", cs[0].Tone, cs[1].Tone)
	}
}

func TestTestRowsCasesOnlyAndFailLine(t *testing.T) {
	rows := sampleRows(t)[ViewTests]
	if len(rows) != 2 {
		t.Fatalf("%d rows, want the 2 cases", len(rows))
	}
	if rows[0].Ref.Line != 12 || rows[0].Ref.Path != "pkg/a_test.go" {
		t.Fatalf("failing test ref %+v, want the fail line", rows[0].Ref)
	}
}

func TestHTTPStates(t *testing.T) {
	h := sampleRows(t)[ViewHTTPHistory]
	if h[0].Cells["state"].S != "ok" || h[1].Cells["state"].S != "error" {
		t.Fatalf("states %q %q", h[0].Cells["state"].S, h[1].Cells["state"].S)
	}
	r := sampleRows(t)[ViewHTTPResponse]
	if len(r) != 1 || r[0].Cells["state"].S != "warning" || !strings.Contains(r[0].Cells["headers"].S, "Content-Type: application/json") {
		t.Fatalf("response %+v", r)
	}
	bin := HTTPResponseRows(webproto.HTTPResponse{Run: 1, Status: 200, Binary: true, Size: 10, Body: "x"})
	if !strings.Contains(bin[0].Cells["body"].S, "binary") {
		t.Fatalf("binary body %q", bin[0].Cells["body"].S)
	}
}

func TestPRReviewWords(t *testing.T) {
	rows := sampleRows(t)[ViewPRList]
	if rows[0].Cells["review"].S != "review required" || rows[0].Ref.ID != "418" {
		t.Fatalf("row %+v", rows[0])
	}
}

func TestUsageUnpricedHasNoCost(t *testing.T) {
	rows := sampleRows(t)[ViewUsage]
	if rows[0].Cells["cost"].Null || rows[0].Cells["cost"].N != 0.02 {
		t.Fatalf("priced %+v", rows[0].Cells["cost"])
	}
	if !rows[1].Cells["cost"].Null {
		t.Fatalf("a subscription line shows no dollar cost: %+v", rows[1].Cells["cost"])
	}
}

func TestDBTable(t *testing.T) {
	base, _ := Lookup(ViewDBRows)
	r := webproto.DBRows{Columns: []string{"id", "Total Amount", "total amount", "secret"}, From: 10,
		Rows: [][]webproto.DBCell{{{V: "1"}, {V: "9.5"}, {N: true}, {V: "••••", M: true}}}}
	d, rows := DBTable(base, r)
	if err := d.Check(); err != nil {
		t.Fatalf("check: %v", err)
	}
	ids := []string{}
	for _, f := range d.Fields {
		ids = append(ids, f.ID)
	}
	if got := strings.Join(ids, ","); got != "row,c_id,c_total_amount,c_total_amount_2,c_secret" {
		t.Fatalf("fields %s", got)
	}
	c := rows[0].Cells
	if c["row"].N != 11 || c["c_id"].S != "1" || !c["c_total_amount_2"].Null || c["c_secret"].S != "••••" {
		t.Fatalf("cells %+v", c)
	}
	if len(base.Fields) != 1 {
		t.Fatal("DBTable changed the registry's def")
	}
	spec, err := views.Resolve(&d, blocks.ViewSpec{View: ViewDBRows, Args: map[string]string{"conn": "local", "table": "orders"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := views.Render("b", &d, spec, rows, false, now); err != nil {
		t.Fatalf("render: %v", err)
	}
}

func TestDBTableWideKeepsTableWidth(t *testing.T) {
	base, _ := Lookup(ViewDBRows)
	cols := make([]string, 80)
	for i := range cols {
		cols[i] = "col" + string(rune('a'+i%26)) + strings.Repeat("x", i/26)
	}
	d, _ := DBTable(base, webproto.DBRows{Columns: cols})
	if len(d.Fields) != views.MaxDefFields {
		t.Fatalf("%d fields, want the cap %d", len(d.Fields), views.MaxDefFields)
	}
	on := 0
	for _, f := range d.Fields {
		if f.Default {
			on++
		}
	}
	if on != views.MaxTableFields {
		t.Fatalf("%d default fields, want %d", on, views.MaxTableFields)
	}
	if err := d.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestLogBlock(t *testing.T) {
	d, _ := Lookup(ViewLogsContainer)
	spec, err := views.Resolve(&d, blocks.ViewSpec{View: d.ID, Args: map[string]string{"id": "db"}})
	if err != nil {
		t.Fatal(err)
	}
	blk := LogBlock("b", &d, spec, []string{"one", "two"}, false)
	if blk.Kind != blocks.KindText || blk.Truncated || blk.MD.C[0].Text != "one\ntwo" {
		t.Fatalf("block %+v", blk)
	}
	if err := blk.Validate(); err != nil {
		t.Fatal(err)
	}
	long := make([]string, 4000)
	for i := range long {
		long[i] = strings.Repeat("y", 30)
	}
	blk = LogBlock("b", &d, spec, long, false)
	if !blk.Truncated || len(blk.MD.C[0].Text) > MaxLogBytes || !strings.HasSuffix(blk.MD.C[0].Text, long[0]) {
		t.Fatalf("long log: truncated=%v len=%d", blk.Truncated, len(blk.MD.C[0].Text))
	}
	if err := blk.Validate(); err != nil {
		t.Fatal(err)
	}
	empty := LogBlock("b", &d, spec, nil, false)
	if empty.MD == nil || empty.Total != 0 {
		t.Fatalf("empty %+v", empty)
	}
}
