package webproto

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// v2Views are the views of mobile plan 3.11.8 marked v2 (MOB-73).
var v2Views = []string{"git.blame", "git.conflict", "db.conns", "db.tables", "db.rows",
	"http.files", "http.saved", "http.history", "http.response", "containers",
	"kube.contexts", "kube.namespaces", "kube.pods", "kube.describe", "tests", "tests.output",
	"services", "search", "logs.container", "logs.pod", "logs.service", "logs.task", "logs.workflow",
	"problems", "debug.state", "settings", "usage", "lsp.servers", "ext.panel", "workspace.folders",
	"layouts", "terms", "files"}

// topicsWithoutView are the topics that are not data a view shows: the
// page's own plumbing (status, toasts, the open chat, the tree and
// buffers, one terminal's screen) and topics whose data a view reads
// through an op instead (db, containers) or that hold settings of the AI
// picker (ai).
var topicsWithoutView = map[string]string{
	"status": "the page's own status line", "toasts": "the page's toasts", "chat": "the open chat (a view lists chats)",
	"tree": "the file tree (files view reads files.list)", "bufs": "open editors", "buf": "one editor's text",
	"term": "one terminal's screen (terms view lists them)", "db": "db views read db.conns / db.tables / db.rows",
	"containers": "containers view reads containers.list", "ai": "the AI picker's state (usage view reads ai.usage)",
}

// v2ReadOps are the read ops the plan's v2 rows name: each one is the
// source of at least one view.
var v2ReadOps = []string{OpGitBlame, OpGitConflict, OpDBConns, OpDBTables, OpDBRows, OpHTTPFiles, OpHTTPSaved,
	OpHTTPHistory, OpHTTPResponse, OpContainersList, OpContainersLogs, OpKubeContexts, OpKubeNamespaces, OpKubeList,
	OpKubeLogs, OpKubeDescribe, OpTestsOutput, OpServicesLogs, OpSearchRun, OpLSPServers, OpExtPanel,
	OpWorkspaceList, OpLayoutList, OpFilesList, OpAIUsage}

// Spec (MOB-73): every v2 view is registered with rows, typed fields and
// a layout.
func TestViewRegistryV2(t *testing.T) {
	for _, id := range v2Views {
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
}

// Spec (MOB-73, coverage): every feature of the menu has a view; every
// topic has a view unless it is in topicsWithoutView; every read op the
// plan names is a view's source; an op-backed view with params turns its
// args into its op's own args type.
func TestViewCoverage(t *testing.T) {
	byFeature, byTopic, byOp := map[string]int{}, map[string]int{}, map[string]int{}
	for _, d := range Views() {
		byFeature[d.Feature]++
		if d.Source.Topic != "" {
			byTopic[d.Source.Topic]++
		}
		if d.Source.Op != "" {
			byOp[d.Source.Op]++
		}
	}
	for _, f := range ViewFeatures {
		if byFeature[f] == 0 {
			t.Errorf("feature %s has no view", f)
		}
	}
	for _, td := range topicDefs {
		_, allowed := topicsWithoutView[td.Name]
		if byTopic[td.Name] == 0 && !allowed {
			t.Errorf("topic %s has no view (add one or say why in topicsWithoutView)", td.Name)
		}
		if byTopic[td.Name] > 0 && allowed {
			t.Errorf("topic %s has a view but is listed as without one", td.Name)
		}
	}
	for _, op := range v2ReadOps {
		if byOp[op] == 0 {
			t.Errorf("read op %s is no view's source", op)
		}
		if p, ok := Lookup(op); !ok || p.Role != RoleViewer || p.LaptopCard {
			t.Errorf("%s: not a plain viewer read: %+v", op, p)
		}
	}
	for _, d := range Views() {
		if d.Source.Op == "" || len(d.Params) == 0 {
			continue
		}
		p, _ := Lookup(d.Source.Op)
		got := SourceArgs(d, Effective(ViewSpec{View: d.ID}, d).Args)
		if want := reflect.TypeOf(p.New()).Elem(); reflect.TypeOf(got) != want {
			t.Errorf("%s: source args %T, op %s takes %v", d.ID, got, d.Source.Op, want)
		}
	}
}

func v2Rows(t *testing.T, id string, args map[string]string, src any) ([]ViewRow, *Block) {
	t.Helper()
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	rows, whole, err := ViewRows(id, args, b)
	if err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	return rows, whole
}

// Spec (MOB-73): a masked DB cell stays masked (even if the laptop sent
// text next to the flag), NULL shows as NULL, and the table keeps the
// caps (12 columns, 200 rows) and says Truncated.
func TestDBRowsViewKeepsMasked(t *testing.T) {
	cols := make([]string, 14)
	for i := range cols {
		cols[i] = "c"
	}
	r := DBRows{Columns: cols, Rows: [][]DBCell{{{V: "hunter2", M: true}, {N: true}, {V: "ok"}}}}
	for i := 0; i < 230; i++ {
		r.Rows = append(r.Rows, []DBCell{{V: "x"}})
	}
	_, b := v2Rows(t, "db.rows", map[string]string{"conn": "main", "table": "users"}, r)
	if b == nil || b.Kind != blocks.KindTable {
		t.Fatalf("db.rows block: %+v", b)
	}
	if b.Rows[0][0] != DBMaskedValue || b.Rows[0][1] != "NULL" || b.Rows[0][2] != "ok" {
		t.Errorf("first row %q", b.Rows[0][:3])
	}
	bb, _ := json.Marshal(b)
	if strings.Contains(string(bb), "hunter2") {
		t.Error("a masked cell's text reached the block")
	}
	if len(b.Columns) != blocks.MaxColumns || len(b.Rows) != blocks.MaxRows || !b.Truncated || b.Total != 231 {
		t.Errorf("caps: %d cols %d rows truncated %v total %d", len(b.Columns), len(b.Rows), b.Truncated, b.Total)
	}
	b.ID = "v-1"
	if err := b.Validate(); err != nil {
		t.Errorf("block: %v", err)
	}
}

// Spec (MOB-73): an HTTP view never shows a body, a header or a URL's
// query or user info.
func TestHTTPViewsHideBodyHeadersAndQuery(t *testing.T) {
	res := HTTPResponse{Run: 3, Label: "login", Method: "POST", URL: "https://bob:pw@api.example.com/login?token=abc#x",
		ReqHeaders: []HTTPHeader{{Name: "Authorization", Value: "Bearer zzz"}}, Status: 401, Body: `{"secret":"s3"}`}
	rows, _ := v2Rows(t, "http.response", nil, res)
	b := ApplyView(mustView(t, "http.response"), ViewSpec{View: "http.response"}, rows)
	bb, _ := json.Marshal(b)
	for _, bad := range []string{"token=abc", "bob", "pw@", "Bearer", "zzz", "s3", "Authorization"} {
		if strings.Contains(string(bb), bad) {
			t.Errorf("http.response shows %q: %s", bad, bb)
		}
	}
	if !strings.Contains(string(bb), "api.example.com/login") || rows[0].Tone != blocks.ToneWarn {
		t.Errorf("http.response lost the request: %s (tone %s)", bb, rows[0].Tone)
	}
	hist, _ := v2Rows(t, "http.history", nil, HTTPHistory{Entries: []HTTPHistItem{{ID: "h1", Method: "GET", URL: "http://x/y?k=v", Status: 503}}})
	if got := hist[0].Cells["url"].S; got != "http://x/y" || hist[0].Tone != blocks.ToneBad {
		t.Errorf("history url %q tone %s", got, hist[0].Tone)
	}
}

// Spec (MOB-73): the logs family are text views that follow (re-run
// every logFollow seconds when op-backed; live when topic-backed).
func TestLogViewsFollow(t *testing.T) {
	for _, d := range Views() {
		if d.Feature != "logs" {
			continue
		}
		if d.Layouts[0] != blocks.KindText {
			t.Errorf("%s: first layout %s, want text", d.ID, d.Layouts[0])
		}
		if d.Source.Topic == "" && d.Refresh != logFollow {
			t.Errorf("%s: refresh %d, want %d (follow)", d.ID, d.Refresh, logFollow)
		}
		if d.Source.Topic != "" && !d.Live {
			t.Errorf("%s: a topic log that is not live", d.ID)
		}
	}
	rows, _ := v2Rows(t, "logs.service", map[string]string{"name": "api"}, ServiceLogs{Name: "api", Lines: []string{"a", "b"}})
	b := ApplyView(mustView(t, "logs.service"), ViewSpec{View: "logs.service", Args: map[string]string{"name": "api"}}, rows)
	if b.Kind != blocks.KindText || b.MD == nil || b.MD.C[0].Text != "a\nb" {
		t.Errorf("logs.service block: %+v", b)
	}
}

// Spec (MOB-73): stop / restart on containers and pods are risky actions
// whose ops wait for a laptop card; the views add no write op of their own.
func TestV2ActionsAreExistingOps(t *testing.T) {
	for _, id := range v2Views {
		d := mustView(t, id)
		for _, a := range d.Actions {
			p, ok := Lookup(a.Op)
			if a.Kind != blocks.ActOp || !ok {
				t.Errorf("%s: action %s names %q", id, a.ID, a.Op)
				continue
			}
			if p.LaptopCard && !a.Risky {
				t.Errorf("%s: %s waits for a laptop card but is not drawn as risky", id, a.Op)
			}
		}
	}
	for _, id := range []string{"containers", "kube.pods"} {
		d := mustView(t, id)
		if !slices.ContainsFunc(d.Actions, func(a BlockAction) bool { return a.Risky }) {
			t.Errorf("%s: no risky (laptop card) action", id)
		}
	}
	if d := mustView(t, "settings"); len(d.Actions) != 0 || d.Role != RoleViewer {
		t.Errorf("settings must stay read only: %+v", d.Actions)
	}
	if d := mustView(t, "debug.state"); len(d.Actions) != 0 {
		t.Errorf("debug.state must stay view only: %+v", d.Actions)
	}
}

// Spec (MOB-73): usage by model or by agent, cost sorted; a subscription
// line has no cost.
func TestUsageView(t *testing.T) {
	src := AIUsage{Scope: "this session",
		ByModel: []AIUsageLine{{Name: "gpt-x", Calls: 2, CostUSD: 0.5, Priced: true, Billing: "per token"}, {Name: "claude-code", Calls: 1, Billing: "subscription"}},
		ByAgent: []AIUsageLine{{Name: "Planner", Calls: 3}}}
	rows, _ := v2Rows(t, "usage", map[string]string{"by": "agent"}, src)
	if len(rows) != 1 || rows[0].Cells["name"].S != "Planner" {
		t.Errorf("by agent: %+v", rows)
	}
	d := mustView(t, "usage")
	eff := Effective(ViewSpec{View: "usage"}, d)
	rows, _ = v2Rows(t, "usage", eff.Args, src)
	b := ApplyView(d, ViewSpec{View: "usage"}, rows)
	if b.Kind != blocks.KindTable || len(b.Rows) != 2 || b.Rows[0][0] != "gpt-x" {
		t.Errorf("usage table: %+v", b.Rows)
	}
}

// Spec (MOB-73): search gathers search.batch hits; each row refers to its
// file line; files filters to its folder param.
func TestSearchAndFilesRows(t *testing.T) {
	rows, _ := v2Rows(t, "search", map[string]string{"query": "x"}, SearchHits{Hits: []SearchHit{{Path: "a/b.go", Line: 3, Text: "  x := 1"}}})
	if len(rows) != 1 || rows[0].Ref == nil || rows[0].Ref.Kind != "lines" || rows[0].Ref.Line != 3 || rows[0].Cells["text"].S != "x := 1" {
		t.Errorf("search rows: %+v", rows)
	}
	rows, _ = v2Rows(t, "files", map[string]string{"path": "internal/"}, FilesListRes{Paths: []string{"internal/a.go", "internals/b.go", "README.md"}})
	if len(rows) != 1 || rows[0].Cells["path"].S != "internal/a.go" || rows[0].Cells["dir"].S != "internal" {
		t.Errorf("files rows: %+v", rows)
	}
}

func mustView(t *testing.T, id string) ViewDef {
	t.Helper()
	d, ok := LookupView(id)
	if !ok {
		t.Fatalf("no view %s", id)
	}
	return d
}
