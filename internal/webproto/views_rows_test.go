package webproto

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// viewRowCase is one view fed a source that walks its branches: want lists
// per row (by index) the cells, tone and ref that row must have.
type viewRowCase struct {
	view string
	args map[string]string
	src  any
	want []viewRowWant
}

type viewRowWant struct {
	cells map[string]any // string, int (num), bool, int64 (time)
	tone  string         // "-" = not checked
	ref   *BlockRef      // nil = not checked
}

func checkViewRow(t *testing.T, id string, i int, got ViewRow, w viewRowWant) {
	t.Helper()
	for k, v := range w.cells {
		c, ok := got.Cells[k]
		if !ok {
			t.Errorf("%s row %d: no cell %q", id, i, k)
			continue
		}
		switch v := v.(type) {
		case string:
			if c.S != v {
				t.Errorf("%s row %d %s: %q, want %q", id, i, k, c.S, v)
			}
		case int:
			if c.N != float64(v) {
				t.Errorf("%s row %d %s: %v, want %d", id, i, k, c.N, v)
			}
		case float64:
			if c.N != v {
				t.Errorf("%s row %d %s: %v, want %v", id, i, k, c.N, v)
			}
		case bool:
			if c.B != v {
				t.Errorf("%s row %d %s: %v, want %v", id, i, k, c.B, v)
			}
		case int64:
			if c.T != v {
				t.Errorf("%s row %d %s: %d, want %d", id, i, k, c.T, v)
			}
		}
	}
	if w.tone != "-" && got.Tone != w.tone {
		t.Errorf("%s row %d: tone %q, want %q", id, i, got.Tone, w.tone)
	}
	if w.ref != nil && (got.Ref == nil || *got.Ref != *w.ref) {
		t.Errorf("%s row %d: ref %+v, want %+v", id, i, got.Ref, *w.ref)
	}
}

func wantCells(kv ...any) map[string]any {
	m := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

// Spec (MOB-9, MOB-73): every list view turns its source into rows with
// the right cells, the right tone for each state and a ref to the thing
// the row is about; each view's default block passes Block.Validate.
func TestViewRowsCellsTonesAndRefs(t *testing.T) {
	cases := []viewRowCase{
		// git (v1)
		{view: "git.status", src: GitSnap{Files: []GitFile{{Path: "a.go", Conflict: true}, {Path: "b.go", Untracked: true},
			{Path: "c.go", Staged: true}, {Path: "d.go", Staged: true, Unstaged: true}}},
			want: []viewRowWant{{cells: wantCells("state", "conflict"), tone: blocks.ToneBad, ref: &BlockRef{Kind: "file", Path: "a.go"}},
				{cells: wantCells("state", "new"), tone: blocks.ToneInfo}, {cells: wantCells("state", "staged"), tone: blocks.ToneOK},
				{cells: wantCells("state", "changed"), tone: blocks.ToneWarn}}},
		{view: "git.log", src: GitSnap{Log: []GitCommit{{Hash: "abc", Subject: "fix", Author: "amy", When: 5, Unpushed: true}}},
			want: []viewRowWant{{cells: wantCells("subject", "fix", "hash", "abc", "author", "amy", "when", int64(5), "unpushed", true), tone: "",
				ref: &BlockRef{Kind: "commit", ID: "abc"}}}},
		{view: "git.stash", src: GitSnap{Stashes: []GitStashEntry{{Ref: "stash@{0}", Message: "wip"}}},
			want: []viewRowWant{{cells: wantCells("message", "wip", "ref", "stash@{0}"), tone: "", ref: &BlockRef{Kind: "stash", ID: "stash@{0}"}}}},
		{view: "git.branches", src: GitBranches{Current: "main", Branches: []string{"main", "dev"}, Remotes: []string{"origin/main"}},
			want: []viewRowWant{{cells: wantCells("name", "main", "current", true, "remote", false), tone: blocks.ToneOK},
				{cells: wantCells("name", "dev", "current", false), tone: ""},
				{cells: wantCells("name", "origin/main", "remote", true), tone: "", ref: &BlockRef{Kind: "branch", ID: "origin/main"}}}},
		{view: "git.tags", src: GitTags{Tags: []string{"v1"}},
			want: []viewRowWant{{cells: wantCells("name", "v1"), tone: "", ref: &BlockRef{Kind: "tag", ID: "v1"}}}},
		// git (v2)
		{view: "git.blame", args: map[string]string{"path": "x.go"}, src: GitBlame{Path: "x.go",
			Commits: []GitBlameCommit{{Hash: "h1", Author: "amy", Subject: "s", When: 7}, {Uncommitted: true}}, Lines: []int{0, 1, 9, -1}},
			want: []viewRowWant{{cells: wantCells("line", 1, "hash", "h1", "author", "amy", "when", int64(7)), tone: "", ref: &BlockRef{Kind: "file", Path: "x.go", Line: 1}},
				{cells: wantCells("line", 2, "hash", "not committed"), tone: ""}}},
		{view: "git.conflict", args: map[string]string{"path": "x.go"}, src: GitConflict{Path: "x.go",
			Blocks: []GitConflictPart{{Line: 4, Ours: "a", Theirs: "b", OursLabel: "HEAD", TheirsLabel: "dev"}}},
			want: []viewRowWant{{cells: wantCells("line", 4, "ours", "a", "theirs", "b", "ours_label", "HEAD", "theirs_label", "dev"), tone: blocks.ToneBad,
				ref: &BlockRef{Kind: "file", Path: "x.go", Line: 4}}}},
		// lanes, races, runs
		{view: "lanes", src: WorktreesSnap{Worktrees: []WorktreeItem{{ID: "w1", Title: "fix", Branch: "b", State: "working", Agents: 2, Dirty: 3, Ahead: 1, Behind: 4, Where: "/x"}}},
			want: []viewRowWant{{cells: wantCells("title", "fix", "agents", 2, "dirty", 3, "ahead", 1, "behind", 4, "where", "/x"), tone: blocks.ToneBusy,
				ref: &BlockRef{Kind: "lane", ID: "w1"}}}},
		{view: "races", src: RacesSnap{Races: []RaceItem{{Name: "r", State: "done", Lanes: []RaceLane{{}, {}}, Winner: "b"}}},
			want: []viewRowWant{{cells: wantCells("name", "r", "lanes", 2, "winner", "b"), tone: blocks.ToneOK}}},
		{view: "lanes.agent", src: AIWorktrees{Items: []AIWorktree{{ID: "a1", Branch: "ai/x", Owner: "Planner", Running: true, Files: []AIWorktreeChange{{}}},
			{ID: "a2", Branch: "ai/y"}}},
			want: []viewRowWant{{cells: wantCells("owner", "Planner", "running", true, "files", 1), tone: blocks.ToneBusy, ref: &BlockRef{Kind: "lane", ID: "a1"}},
				{cells: wantCells("running", false, "files", 0), tone: blocks.ToneIdle}}},
		{view: "runs", src: RunsSnap{Runs: []RunItem{{Key: "k1", Title: "t", State: "running", Step: 2, Steps: 5, Started: 9, CostUSD: 0.25},
			{Key: "k2", State: "running", NeedsYou: true}}},
			want: []viewRowWant{{cells: wantCells("step", 2, "steps", 5, "when", int64(9), "cost", 0.25), tone: blocks.ToneBusy, ref: &BlockRef{Kind: "run", ID: "k1"}},
				{cells: wantCells("needs_you", true), tone: blocks.ToneWarn}}},
		// workflows, tasks, projects
		{view: "workflows", src: WorkflowsSnap{Workflows: []WorkflowItem{{Name: "nightly", Title: "Nightly", State: "on", When: "02:00", Next: 3, Last: "ok"},
			{Name: "weekly", State: "off"}}},
			want: []viewRowWant{{cells: wantCells("title", "Nightly", "when", "02:00", "next", int64(3), "last", "ok"), tone: blocks.ToneOK, ref: &BlockRef{Kind: "workflow", ID: "nightly"}},
				{cells: wantCells("title", "weekly"), tone: blocks.ToneIdle}}},
		{view: "workflows.runs", src: WorkflowsSnap{Runs: []WorkflowRun{{ID: "r1", Workflow: "nightly", State: "failed", Started: 1, Ended: 2, Items: 3, Failed: 1, Dry: true}}},
			want: []viewRowWant{{cells: wantCells("workflow", "nightly", "ended", int64(2), "items", 3, "failed", 1, "dry", true), tone: blocks.ToneBad,
				ref: &BlockRef{Kind: "workflow_run", ID: "r1"}}}},
		{view: "workflow.tail", args: map[string]string{"run": "r2"}, src: WorkflowsSnap{Runs: []WorkflowRun{{ID: "r1", Tail: []string{"no"}}, {ID: "r2", Tail: []string{"one", "two"}}}},
			want: []viewRowWant{{cells: wantCells("line", "one"), tone: ""}, {cells: wantCells("line", "two"), tone: ""}}},
		{view: "tasks", src: TasksSnap{Tasks: []TaskItem{{Label: "build", Group: "build", Source: "make", Last: &TaskLast{State: "fail", When: 8}}, {Label: "lint"}}},
			want: []viewRowWant{{cells: wantCells("label", "build", "state", "fail", "when", int64(8)), tone: blocks.ToneBad, ref: &BlockRef{Kind: "task", ID: "build"}},
				{cells: wantCells("state", ""), tone: blocks.ToneIdle}}},
		{view: "task.tail", src: TaskTail{Lines: []string{"ok"}}, want: []viewRowWant{{cells: wantCells("line", "ok"), tone: ""}}},
		{view: "logs.task", src: TaskTail{Lines: []string{"ok"}}, want: []viewRowWant{{cells: wantCells("line", "ok"), tone: ""}}},
		{view: "projects", src: ProjectsSnap{Projects: []ProjectItem{{ID: "p1", Name: "app", State: "ready", Working: 1, OpenedAt: 4}, {ID: "p2", State: "ready", Waiting: 2}}},
			want: []viewRowWant{{cells: wantCells("name", "app", "working", 1, "when", int64(4)), tone: blocks.ToneOK, ref: &BlockRef{Kind: "project", ID: "p1"}},
				{cells: wantCells("waiting", 2), tone: blocks.ToneWarn}}},
		// inbox, cards, chats
		{view: "inbox", src: InboxSnap{Items: []InboxItem{{Kind: "run", Ref: "k1", Title: "t", State: "needs_you", Since: 3, Lane: "l", Chat: "c"}}},
			want: []viewRowWant{{cells: wantCells("title", "t", "kind", "run", "when", int64(3), "lane", "l", "chat", "c"), tone: blocks.ToneWarn,
				ref: &BlockRef{Kind: "run", ID: "k1"}}}},
		{view: "cards", src: CardsSnap{Cards: []Card{{ID: "c1", Kind: "ask", Title: "Allow?", Since: 2, ReadOnly: true}}},
			want: []viewRowWant{{cells: wantCells("title", "Allow?", "laptop", true), tone: blocks.ToneWarn, ref: &BlockRef{Kind: "card", ID: "c1"}}}},
		{view: "chats", src: ChatsSnap{Chats: []ChatItem{{ID: "c1", Title: "a", Msgs: 3, Live: true, Open: true}, {ID: "c2"}}},
			want: []viewRowWant{{cells: wantCells("msgs", 3, "live", true, "open", true), tone: blocks.ToneBusy, ref: &BlockRef{Kind: "chat", ID: "c1"}},
				{cells: wantCells("live", false), tone: blocks.ToneIdle}}},
		// ci
		{view: "pr.list", src: PRList{Items: []PRItem{{Number: 7, Title: "t", Checks: "fail", Review: "APPROVED", Author: "amy", Branch: "b", Base: "main", Draft: true}}},
			want: []viewRowWant{{cells: wantCells("number", 7, "checks", "fail", "draft", true), tone: blocks.ToneBad, ref: &BlockRef{Kind: "pr", ID: "7"}}}},
		{view: "pr", args: map[string]string{"number": "7"}, src: PRView{Item: PRItem{Number: 7, Checks: "pass"}, State: "OPEN", Mergeable: "MERGEABLE", Files: []PRFile{{}, {}}},
			want: []viewRowWant{{cells: wantCells("state", "OPEN", "files", 2, "mergeable", "MERGEABLE"), tone: blocks.ToneOK}}},
		{view: "ci.checks", src: CIStatus{Runs: []CIRun{{Workflow: "test", State: "pending", Title: "t", Status: "queued", Conclusion: ""}}},
			want: []viewRowWant{{cells: wantCells("workflow", "test", "status", "queued"), tone: blocks.ToneWarn}}},
		{view: "mcp.status", src: MCPSnap{Agents: []MCPAgent{{Name: "bot", Since: 1, Calls: 4, Asked: 1}, {}}},
			want: []viewRowWant{{cells: wantCells("name", "bot", "calls", 4, "asked", 1), tone: blocks.ToneOK}, {cells: wantCells("name", "(no name yet)"), tone: blocks.ToneOK}}},
		// db
		{view: "db.conns", src: DBConns{Conns: []DBConn{{ID: "d1", Name: "main", Driver: "postgres", Connected: true, ReadOnly: true},
			{ID: "d2", Problem: "no route"}, {ID: "d3", Name: "idle"}}},
			want: []viewRowWant{{cells: wantCells("name", "main", "connected", true, "readonly", true), tone: blocks.ToneOK, ref: &BlockRef{Kind: "db", ID: "d1"}},
				{cells: wantCells("name", "d2", "problem", "no route"), tone: blocks.ToneBad}, {cells: wantCells("name", "idle"), tone: blocks.ToneIdle}}},
		{view: "db.tables", args: map[string]string{"conn": "main"}, src: DBTables{Conn: "main", Tables: []DBTable{{Name: "users"}, {Name: "v_users", View: true}}},
			want: []viewRowWant{{cells: wantCells("name", "users", "view", false), tone: "", ref: &BlockRef{Kind: "db_table", ID: "main/users"}},
				{cells: wantCells("view", true), tone: ""}}},
		// http
		{view: "http.files", src: HTTPFiles{Files: []HTTPFile{{Path: "api.http", Requests: []HTTPReqItem{{Index: 0, Name: "login", Method: "POST", URL: "https://x/login?k=v", Line: 3},
			{Index: 1, Method: "GET", URL: "https://x/me#top"}}}}},
			want: []viewRowWant{{cells: wantCells("name", "login", "url", "https://x/login", "file", "api.http"), tone: "",
				ref: &BlockRef{Kind: "http_request", ID: "0", Path: "api.http", Line: 3}},
				{cells: wantCells("name", "GET https://x/me"), tone: ""}}},
		{view: "http.saved", src: HTTPSaved{Requests: []HTTPReqItem{{Index: 2, Name: "ping", Method: "GET", URL: "http://h/p"}}},
			want: []viewRowWant{{cells: wantCells("name", "ping", "file", ""), tone: "", ref: &BlockRef{Kind: "http_request", ID: "2"}}}},
		{view: "http.history", src: HTTPHistory{Entries: []HTTPHistItem{{ID: "a", Status: 200}, {ID: "b", Status: 302}, {ID: "c", Status: 404},
			{ID: "d", Err: "refused"}, {ID: "e"}}},
			want: []viewRowWant{{tone: blocks.ToneOK, ref: &BlockRef{Kind: "http_history", ID: "a"}}, {tone: blocks.ToneInfo}, {tone: blocks.ToneWarn},
				{cells: wantCells("err", "refused"), tone: blocks.ToneBad}, {tone: blocks.ToneIdle}}},
		{view: "http.response", args: map[string]string{"run": "4"}, src: HTTPResponse{Run: 4, Status: 200, Running: true},
			want: []viewRowWant{{cells: wantCells("running", true), tone: blocks.ToneBusy, ref: &BlockRef{Kind: "http_run", ID: "4"}}}},
		// containers, kube
		{view: "containers", src: ContainersList{Containers: []ContainerItem{{ID: "c1", Name: "db", State: "running", Image: "pg", Ports: "5432", Project: "app"},
			{ID: "c2", State: "paused"}, {ID: "c3", State: "running", Busy: true}, {ID: "c4", State: "exited"}, {ID: "c5", State: "dead"}}},
			want: []viewRowWant{{cells: wantCells("name", "db", "image", "pg", "ports", "5432", "project", "app"), tone: blocks.ToneOK, ref: &BlockRef{Kind: "container", ID: "c1"}},
				{tone: blocks.ToneWarn}, {tone: blocks.ToneBusy}, {tone: blocks.ToneIdle}, {tone: blocks.ToneBad}}},
		{view: "kube.contexts", src: KubeContexts{Contexts: []string{"prod", "dev"}, Current: "dev"},
			want: []viewRowWant{{cells: wantCells("name", "prod", "current", false), tone: "", ref: &BlockRef{Kind: "kube_context", ID: "prod"}},
				{cells: wantCells("current", true), tone: blocks.ToneOK}}},
		{view: "kube.namespaces", src: KubeNamespaces{Namespaces: []string{"default"}},
			want: []viewRowWant{{cells: wantCells("name", "default"), tone: "", ref: &BlockRef{Kind: "kube_namespace", ID: "default"}}}},
		{view: "kube.pods", src: KubeList{Pods: []KubePod{{Name: "api", Status: "Running", Ready: "1/1", Restarts: 2, Created: 6},
			{Name: "w", Status: "Pending"}, {Name: "x", Status: "CrashLoopBackOff"}, {Name: "y", Status: "Terminating"}}},
			want: []viewRowWant{{cells: wantCells("ready", "1/1", "restarts", 2, "when", int64(6)), tone: blocks.ToneOK, ref: &BlockRef{Kind: "pod", ID: "api"}},
				{tone: blocks.ToneBusy}, {tone: blocks.ToneBad}, {tone: blocks.ToneWarn}}},
		{view: "kube.describe", args: map[string]string{"kind": "pod", "name": "api"}, src: KubeText{Lines: []string{"Name: api"}},
			want: []viewRowWant{{cells: wantCells("line", "Name: api"), tone: ""}}},
		{view: "logs.pod", args: map[string]string{"pod": "api"}, src: KubeText{Lines: []string{"x", "y"}},
			want: []viewRowWant{{cells: wantCells("line", "x"), tone: ""}, {cells: wantCells("line", "y"), tone: ""}}},
		{view: "logs.container", args: map[string]string{"id": "c1"}, src: ContainerLogs{Lines: []string{"up"}},
			want: []viewRowWant{{cells: wantCells("line", "up"), tone: ""}}},
		// tests, services
		{view: "tests", src: TestsSnap{Items: []TestItem{{ID: "t1", Label: "TestA", State: "failed", Kind: "case", Path: "a_test.go", Ms: 12, Message: "boom", FailPath: "a.go", FailLine: 9},
			{ID: "t2", State: "passed"}, {ID: "t3", State: "running"}, {ID: "t4", State: "skipped"}, {ID: "t5", State: "queued"}, {ID: "t6", State: "failed"}}},
			want: []viewRowWant{{cells: wantCells("label", "TestA", "ms", 12, "message", "boom"), tone: blocks.ToneBad, ref: &BlockRef{Kind: "test", ID: "t1", Path: "a.go", Line: 9}},
				{tone: blocks.ToneOK}, {tone: blocks.ToneBusy}, {tone: blocks.ToneIdle}, {tone: blocks.ToneIdle},
				{tone: blocks.ToneBad, ref: &BlockRef{Kind: "test", ID: "t6"}}}},
		{view: "tests.output", src: TestOutput{Lines: []string{"--- FAIL"}}, want: []viewRowWant{{cells: wantCells("line", "--- FAIL"), tone: ""}}},
		{view: "services", src: ServicesSnap{Services: []ServiceItem{{Name: "api", State: "running", Health: "healthy", Kind: "compose", Since: 5,
			Ports: []ServicePort{{Published: 80}, {Published: 443}}}, {Name: "w", State: "running", Busy: true}, {Name: "x", State: "running", Health: "unhealthy"},
			{Name: "y", State: "exited", Exit: 1}, {Name: "z", State: "exited"}}},
			want: []viewRowWant{{cells: wantCells("ports", "80, 443", "health", "healthy", "when", int64(5)), tone: blocks.ToneBusy, ref: &BlockRef{Kind: "service", ID: "api"}},
				{tone: blocks.ToneBusy}, {tone: blocks.ToneBad}, {tone: blocks.ToneBad}, {tone: blocks.ToneInfo}}},
		// diagnostics, debug, settings
		{view: "problems", src: DiagSnap{Files: []DiagFile{{Path: "a.go", Items: []Diag{{Line: 1, Sev: 1, Message: "e", Source: "gopls"}, {Sev: 2}, {Sev: 3}, {Sev: 4}}}}},
			want: []viewRowWant{{cells: wantCells("severity", "error", "path", "a.go", "source", "gopls"), tone: blocks.ToneBad, ref: &BlockRef{Kind: "lines", Path: "a.go", Line: 1}},
				{cells: wantCells("severity", "warning"), tone: blocks.ToneWarn}, {cells: wantCells("severity", "info"), tone: blocks.ToneInfo},
				{cells: wantCells("severity", "hint"), tone: blocks.ToneIdle}}},
		{view: "debug.state", src: DebugSnap{Active: true, Stopped: true, Name: "go", Status: "paused",
			Frames: []DebugFrame{{Name: "main", File: "main.go", Path: "cmd/main.go", Line: 3}, {Name: "rt"}},
			Scopes: []DebugScope{{Vars: []DebugVar{{Name: "x", Value: "1"}}}}, Watches: []DebugWatch{{Expr: "x+1", Value: "2"}}},
			want: []viewRowWant{{cells: wantCells("part", "session", "name", "go", "value", "paused"), tone: blocks.ToneWarn},
				{cells: wantCells("part", "stack", "value", "main.go"), tone: "", ref: &BlockRef{Kind: "lines", Path: "cmd/main.go", Line: 3}},
				{cells: wantCells("part", "stack", "name", "rt"), tone: ""}, {cells: wantCells("part", "variables", "name", "x", "value", "1"), tone: ""},
				{cells: wantCells("part", "watch", "name", "x+1", "value", "2"), tone: ""}}},
		{view: "debug.state", src: DebugSnap{Active: true, Status: "running"}, want: []viewRowWant{{cells: wantCells("value", "running"), tone: blocks.ToneBusy}}},
		{view: "debug.state", src: DebugSnap{Status: "gone"}, want: []viewRowWant{{cells: wantCells("value", "not running"), tone: blocks.ToneIdle}}},
		{view: "settings", src: SettingsSnap{Items: []SettingItem{{Key: "font", Value: "14", Default: "12", Group: "editor", Description: "size"}, {Key: "tab", Value: "4", Default: "4"}}},
			want: []viewRowWant{{cells: wantCells("key", "font", "changed", true, "about", "size"), tone: blocks.ToneInfo, ref: &BlockRef{Kind: "setting", ID: "font"}},
				{cells: wantCells("changed", false), tone: ""}}},
		// lsp, ext, workspace, layouts
		{view: "lsp.servers", src: LSPServerList{Items: []LSPServer{{Name: "gopls", Title: "Go", State: "managed", Used: true, Langs: []string{"go", "mod"}, Version: "1"},
			{Name: "p", State: "installing"}, {Name: "q", State: "missing"}, {Name: "r", State: "managed", Problem: "crashed"}}},
			want: []viewRowWant{{cells: wantCells("title", "Go", "langs", "go, mod", "used", true, "version", "1"), tone: blocks.ToneOK, ref: &BlockRef{Kind: "lsp_server", ID: "gopls"}},
				{tone: blocks.ToneBusy}, {tone: blocks.ToneIdle}, {cells: wantCells("problem", "crashed"), tone: blocks.ToneBad}}},
		{view: "ext.panel", args: map[string]string{"id": "x"}, src: ExtPanelView{Lines: [][]ExtSpan{{{T: "a"}, {T: "b"}}, {}}},
			want: []viewRowWant{{cells: wantCells("line", "ab"), tone: ""}, {cells: wantCells("line", ""), tone: ""}}},
		{view: "workspace.folders", src: WorkspaceList{Folders: []WorkspaceEntry{{ID: 1, Name: "app"}}, Recent: []WorkspaceEntry{{ID: 2, Name: "old"}}},
			want: []viewRowWant{{cells: wantCells("name", "app", "recent", false), tone: blocks.ToneOK, ref: &BlockRef{Kind: "folder", ID: "1"}},
				{cells: wantCells("name", "old", "recent", true), tone: blocks.ToneIdle, ref: &BlockRef{Kind: "folder", ID: "2"}}}},
		{view: "layouts", src: LayoutList{Items: []LayoutEntry{{Name: "wide", Source: "builtin", Description: "two panes"}}},
			want: []viewRowWant{{cells: wantCells("name", "wide", "source", "builtin", "about", "two panes"), tone: "", ref: &BlockRef{Kind: "layout", ID: "wide"}}}},
		// terms, files
		{view: "terms", src: TermsSnap{Terms: []TermItem{{Buf: 3, Title: "zsh", Cwd: "/x", LastCmd: "ls", Task: "t", Exited: true, Busy: true},
			{Buf: 4, Busy: true}, {Buf: 5, Active: true}, {Buf: 6}}},
			want: []viewRowWant{{cells: wantCells("title", "zsh", "last", "ls", "cwd", "/x", "task", "t", "exited", true), tone: blocks.ToneIdle, ref: &BlockRef{Kind: "term", ID: "3"}},
				{tone: blocks.ToneBusy}, {tone: blocks.ToneOK}, {tone: blocks.ToneIdle}}},
		{view: "files", src: FilesListRes{Paths: []string{"README.md", "a/b.go"}},
			want: []viewRowWant{{cells: wantCells("path", "README.md", "dir", ""), tone: "", ref: &BlockRef{Kind: "file", Path: "README.md"}},
				{cells: wantCells("dir", "a"), tone: ""}}},
		{view: "files", args: map[string]string{"path": "a/"}, src: FilesListRes{Paths: []string{"a", "a/b.go", "ab/c.go"}},
			want: []viewRowWant{{cells: wantCells("path", "a"), tone: ""}, {cells: wantCells("path", "a/b.go"), tone: ""}}},
	}
	for _, c := range cases {
		rows, whole := v2Rows(t, c.view, c.args, c.src)
		if whole != nil {
			t.Errorf("%s: a whole block, want rows", c.view)
			continue
		}
		if len(rows) != len(c.want) {
			t.Errorf("%s: %d rows, want %d: %+v", c.view, len(rows), len(c.want), rows)
			continue
		}
		for i, w := range c.want {
			checkViewRow(t, c.view, i, rows[i], w)
		}
		d := mustView(t, c.view)
		spec := ViewSpec{View: c.view, Args: c.args}
		for _, layout := range d.Layouts {
			spec.Layout = layout
			b := ApplyView(d, spec, rows)
			b.ID = "v-1"
			if err := b.Validate(); err != nil {
				t.Errorf("%s (%s): block: %v", c.view, layout, err)
			}
		}
	}
}

// Spec (MOB-9): a source that is not the view's JSON is an error, not
// empty rows; a view without rows says so.
func TestViewRowsBadSource(t *testing.T) {
	for _, id := range []string{"git.status", "db.rows", "git.diff", "containers"} {
		if _, _, err := ViewRows(id, nil, []byte(`[1,2`)); err == nil {
			t.Errorf("%s: bad JSON gave no error", id)
		}
	}
	if _, _, err := ViewRows("no.such.view", nil, []byte(`{}`)); err == nil {
		t.Error("an unknown view gave rows")
	}
}

// Spec (MOB-9): every filter op keeps the rows it should, typed by the
// field's kind (num, time, cost, bool, text).
func TestApplyViewFilterOps(t *testing.T) {
	hist := HTTPHistory{Entries: []HTTPHistItem{
		{ID: "a", At: 100, Method: "GET", URL: "http://x/users", Status: 200, Env: "dev"},
		{ID: "b", At: 200, Method: "POST", URL: "http://x/login", Status: 401, Err: "denied"},
		{ID: "c", At: 300, Method: "GET", URL: "http://y/health", Status: 503}}}
	rows, _ := v2Rows(t, "http.history", nil, hist)
	d := mustView(t, "http.history")
	count := func(conds ...ViewCond) int {
		t.Helper()
		spec := ViewSpec{View: "http.history", Filter: conds, Layout: blocks.KindTable}
		if err := CheckViewSpec(&spec, d); err != nil {
			t.Fatalf("%+v: %v", conds, err)
		}
		return len(ApplyView(d, spec, rows).Rows)
	}
	for _, c := range []struct {
		cond ViewCond
		want int
	}{
		{ViewCond{Field: "err", Op: "empty"}, 2},
		{ViewCond{Field: "err", Op: "not_empty"}, 1},
		{ViewCond{Field: "url", Op: "contains", Value: "X/LOG"}, 1},
		{ViewCond{Field: "url", Op: "starts", Value: "http://y"}, 1},
		{ViewCond{Field: "method", Op: "in", In: []string{"post", "put"}}, 1},
		{ViewCond{Field: "method", Op: "eq", Value: "get"}, 2},
		{ViewCond{Field: "method", Op: "ne", Value: "get"}, 1},
		{ViewCond{Field: "status", Op: "eq", Value: "401"}, 1},
		{ViewCond{Field: "status", Op: "eq", Value: "x"}, 0},
		{ViewCond{Field: "status", Op: "gt", Value: "400"}, 2},
		{ViewCond{Field: "status", Op: "lt", Value: "401"}, 1},
		{ViewCond{Field: "status", Op: "ge", Value: "401"}, 2},
		{ViewCond{Field: "status", Op: "le", Value: "200"}, 1},
		{ViewCond{Field: "when", Op: "ge", Value: "200"}, 2},
		{ViewCond{Field: "when", Op: "not_empty"}, 3},
		{ViewCond{Field: "env", Op: "empty"}, 2},
	} {
		if got := count(c.cond); got != c.want {
			t.Errorf("%+v: %d rows, want %d", c.cond, got, c.want)
		}
	}
	if got := count(ViewCond{Field: "method", Op: "eq", Value: "get"}, ViewCond{Field: "status", Op: "gt", Value: "300"}); got != 1 {
		t.Errorf("two conditions: %d rows, want 1", got)
	}
	// bool and cost fields
	conns, _ := v2Rows(t, "db.conns", nil, DBConns{Conns: []DBConn{{ID: "a", Connected: true}, {ID: "b"}}})
	dc := mustView(t, "db.conns")
	for v, want := range map[string]int{"true": 1, "yes": 1, "1": 1, "false": 1} {
		spec := ViewSpec{View: "db.conns", Filter: []ViewCond{{Field: "connected", Op: "eq", Value: v}}}
		if got := len(ApplyView(dc, spec, conns).Items); got != want {
			t.Errorf("connected eq %s: %d items, want %d", v, got, want)
		}
	}
	usage, _ := v2Rows(t, "usage", map[string]string{"by": "model"}, AIUsage{ByModel: []AIUsageLine{{Name: "a", CostUSD: 0.5}, {Name: "b", CostUSD: 2}}})
	du := mustView(t, "usage")
	spec := ViewSpec{View: "usage", Filter: []ViewCond{{Field: "cost", Op: "gt", Value: "1"}}, Layout: blocks.KindTable}
	if b := ApplyView(du, spec, usage); len(b.Rows) != 1 || b.Rows[0][0] != "b" {
		t.Errorf("cost gt 1: %+v", b.Rows)
	}
}

// Spec (MOB-9): a spec that breaks a view's rules is refused with a
// message that says which rule.
func TestCheckViewSpecRefusesByRule(t *testing.T) {
	for _, c := range []struct {
		view string
		spec ViewSpec
		want string
	}{
		{"db.tables", ViewSpec{View: "db.conns"}, "run as"},
		{"db.tables", ViewSpec{View: "db.tables", Limit: -1}, "limit"},
		{"db.tables", ViewSpec{View: "db.tables", Args: map[string]string{"conn": "main", "x": "1"}}, "unknown param \"x\" (one of conn)"},
		{"db.tables", ViewSpec{View: "db.tables", Args: map[string]string{"conn": "a b"}}, "a name, not"},
		{"db.rows", ViewSpec{View: "db.rows", Args: map[string]string{"conn": "main", "table": "t", "from": "-1"}}, "a whole number"},
		{"usage", ViewSpec{View: "usage", Args: map[string]string{"by": "day"}}, "one of model, agent"},
		{"files", ViewSpec{View: "files", Args: map[string]string{"path": "a/../b"}}, "inside the workspace"},
		{"http.history", ViewSpec{View: "http.history", Filter: []ViewCond{{Field: "status", Op: "contains", Value: "4"}}}, "needs a text field"},
		{"http.history", ViewSpec{View: "http.history", Filter: []ViewCond{{Field: "status", Op: "gt", Value: "x"}}}, "needs a number"},
		{"http.history", ViewSpec{View: "http.history", Filter: []ViewCond{{Field: "nope", Op: "eq"}}}, "filterable: when, method"},
		{"http.history", ViewSpec{View: "http.history", Group: "nope"}, "cannot group on"},
	} {
		d := mustView(t, c.view)
		err := CheckViewSpec(&c.spec, d)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %+v: %v, want %q", c.view, c.spec, err, c.want)
		}
	}
}
