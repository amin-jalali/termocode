//go:build browser

package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webhl"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Screenshots of the main views for a person to look at (not compared):
// WEB_SHOTS_DIR=/some/dir go test -tags browser -run TestShots ./web/
// The fake hub answers like a real one: a Go file coloured by the hub's
// own lexer, a chat, a shell and a git status.

const shotsEnv = "WEB_SHOTS_DIR"

var shotFile = `package main

import (
	"bytes"
	"io"
	"strings"
)

// shortenCoverLine cuts the package list that "go test -coverpkg" repeats
// on every "coverage: N% of statements in ..." line.
func shortenCoverLine(line string) string {
	const marker = "of statements in "
	i := strings.Index(line, marker)
	if i < 0 {
		return line
	}
	tail := ""
	if strings.HasSuffix(line, "\n") {
		tail = "\n"
	}
	return line[:i+len(marker)] + "-coverpkg" + tail
}

// lineFilter passes whole lines through f to w. Close writes the last
// unfinished line.
type lineFilter struct {
	w   io.Writer
	f   func(string) string
	buf []byte
}

func (l *lineFilter) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := string(l.buf[:i+1])
		l.buf = l.buf[i+1:]
		if _, err := io.WriteString(l.w, l.f(line)); err != nil {
			return len(p), err
		}
	}
}
`

// shotHub answers the page from fixed data until the test ends.
func shotHub(t *testing.T, c *HubConn) {
	lines := strings.Split(strings.TrimSuffix(shotFile, "\n"), "\n")
	snaps := map[string]any{
		"chat": chatWith(false,
			userMsg(0, "Why does the coverage line repeat the package list?"),
			aiMsg(1, md("doc", mdPara("go test -coverpkg prints every package on each line. shortenCoverLine in cmd/covreport/filter.go cuts it after the marker."),
				mdPara("I can add a test for a line without a newline at the end."))),
			userMsg(2, "Yes, add it please")),
		"cards": map[string]any{"cards": []any{}},
		"chats": map[string]any{"chats": []any{}},
		"tree": webproto.TreeSnap{Roots: []webproto.TreeRoot{{Path: "", Label: "termocode"}}, Dirs: map[string]webproto.TreeDir{
			"":              {Entries: []webproto.TreeEntry{{Name: "cmd", Dir: true}, {Name: "internal", Dir: true}, {Name: "docs", Dir: true}, {Name: "go.mod"}, {Name: "README.md", Git: "M"}, {Name: "Makefile"}}},
			"cmd":           {Entries: []webproto.TreeEntry{{Name: "covreport", Dir: true}, {Name: "termocode", Dir: true}}},
			"cmd/covreport": {Entries: []webproto.TreeEntry{{Name: "filter.go", Git: "M"}, {Name: "filter_test.go"}, {Name: "main.go"}}},
		}},
		"bufs":  map[string]any{"bufs": []any{map[string]any{"buf": 2, "path": "cmd/covreport/filter.go", "dirty": false, "active": true}, map[string]any{"buf": 4, "path": "README.md", "dirty": true, "active": false}}},
		"buf:2": bufSnap(2, "cmd/covreport/filter.go", lines...),
		"terms": termsSnap(termItem(7, "zsh", false, true), termItem(8, "go test", true, false)),
		"term:7": termSnap(7, 0, 100, "~/termocode main ❯ go test ./cmd/covreport/", "ok  	github.com/amin-jalali/termocode/cmd/covreport	0.412s",
			"~/termocode main ❯ git status --short", " M cmd/covreport/filter.go", " M README.md", "~/termocode main ❯ "),
		"git": webproto.GitSnap{Repo: true, Branch: webproto.GitBranch{Name: "main", Ahead: 1},
			Files: []webproto.GitFile{{Path: "cmd/covreport/filter.go", X: " ", Y: "M", Unstaged: true}, {Path: "README.md", X: "M", Y: " ", Staged: true}, {Path: "notes.txt", X: "?", Y: "?", Untracked: true}},
			Log:   []webproto.GitCommit{{Hash: "60ed1c8", Subject: "test(e2e): the MCP relay's stderr is read under a lock", Author: "Amin", When: time.Now().Add(-time.Hour).UnixMilli()}}},
		"tasks": webproto.TasksSnap{Tasks: []webproto.TaskItem{
			{Label: "build", Group: "build", Default: true, Source: "make", Detail: "make build", Last: &webproto.TaskLast{State: "ok", When: time.Now().Add(-20 * time.Minute).UnixMilli(), Buf: 8}},
			{Label: "test", Group: "test", Source: "go", Detail: "go test ./...", Last: &webproto.TaskLast{State: "running", When: time.Now().Add(-40 * time.Second).UnixMilli(), Buf: 8}},
			{Label: "lint", Source: "tasks.json", Detail: "golangci-lint run", Last: &webproto.TaskLast{State: "failed", Code: 1, When: time.Now().Add(-3 * time.Hour).UnixMilli(), Buf: 9}},
			{Label: "docs", Source: "npm", Detail: "npm run docs"},
		}},
		"runs": webproto.RunsSnap{Runs: []webproto.RunItem{
			{Key: "c1/r1", Title: "fix the toast flicker", Chat: "Fix the build", Profile: "coder", State: "running", Step: 2, Steps: 4, StepTitle: "Run the tests", ElapsedMs: 41000, Live: true, CostUSD: 0.12, Can: []string{"stop", "pause"}},
			{Key: "c1/r0", Title: "add a test for a line without a newline", Profile: "review", State: "done", Step: 3, Steps: 3, ElapsedMs: 125000, CostUSD: 0.31},
			{Key: "c2/r4", Title: "rename the cover report flags", Profile: "coder", State: "paused", Why: "Waiting for you to answer a card.", Step: 1, Steps: 5, ElapsedMs: 9000, NeedsYou: true, Can: []string{"resume", "stop"}},
		}},
		"mcp": webproto.MCPSnap{Mode: "auto", Listening: true,
			Agents: []webproto.MCPAgent{{Name: "claude-desktop", Since: time.Now().Add(-25 * time.Minute).UnixMilli(), Calls: 14, Asked: 2}, {Name: "lint-agent", Since: time.Now().Add(-4 * time.Minute).UnixMilli(), Calls: 3}},
			Calls: []string{"12:03 claude-desktop · read_file · ran without asking", "12:04 claude-desktop · run_task · asked, you allowed", "12:09 lint-agent · search · ran without asking"}},
		"settings": webproto.SettingsSnap{Items: []webproto.SettingItem{
			{Key: "theme", Group: "Appearance", Type: "enum", Value: "vscode-dark-plus", Default: "vscode-dark-plus", Description: "The colour theme of the editor and the panels."},
			{Key: "font_size", Group: "Appearance", Type: "int", Value: "14", Default: "13", Description: "The size of the editor's text, in points."},
			{Key: "tab_size", Group: "Editor", Type: "int", Value: "4", Default: "4", Description: "How many spaces a tab is wide."},
			{Key: "word_wrap", Group: "Editor", Type: "bool", Value: "false", Default: "false", Description: "Wrap long lines at the window's edge."},
			{Key: "format_on_save", Group: "Editor", Type: "bool", Value: "true", Default: "false", Description: "Format a file with its language server when it is saved."},
			{Key: "ai.profile", Group: "AI", Type: "string", Value: "coder", Default: "coder", Description: "The agent profile a new chat starts with."},
			{Key: "mcp_server", Group: "AI", Type: "enum", Value: "auto", Default: "auto", Description: "Let outside agents use termocode as an MCP server."},
		}},
	}
	for k, v := range runShotSnaps() {
		snaps[k] = v
	}
	for _, tp := range []string{"chat", "cards", "chats", "tree", "bufs", "git"} { // asked for at login (git: on a desktop)
		c.Send(t, Snap(tp, 1, snaps[tp]))
	}
	tick := 3
	tailRow := slices.Index(lines, "	tail := \"\"")
	go func() {
		for {
			var m webproto.Msg
			select {
			case m = <-c.in:
			case <-c.done:
				return
			}
			switch v := m.(type) {
			case *webproto.Sub:
				for _, tp := range v.Topics {
					if d, ok := snaps[tp]; ok {
						_ = c.send(Snap(tp, 1, d))
					}
				}
			case *webproto.Req:
				var data any
				switch v.Op {
				case "buf.open":
					data = map[string]any{"buf": 2, "path": "cmd/covreport/filter.go", "tick": 3, "lines": len(lines), "dirty": false, "ft": "go", "readonly": false}
				case "git.text":
					// The index's copy: the file before its two changed rows.
					old := strings.Replace(strings.Replace(shotFile, `"-coverpkg"`, `"…"`, 1), "\ttail := \"\"\n", "\tvar tail string\n", 1)
					data = webproto.GitText{Path: "cmd/covreport/filter.go", Rev: "index", Text: old}
				case "search.run":
					var a struct {
						Query string `json:"query"`
					}
					_ = json.Unmarshal(v.Args, &a)
					hits := []webproto.SearchHit{}
					for i, l := range lines {
						if j := strings.Index(l, a.Query); j >= 0 && a.Query != "" {
							hits = append(hits, webproto.SearchHit{Path: "cmd/covreport/filter.go", Line: i + 1, Col: j + 1, Text: l, Marks: []webproto.SearchMark{{S: j, E: j + len(a.Query)}}})
						}
					}
					hits = append(hits, webproto.SearchHit{Path: "cmd/covreport/main.go", Line: 12, Col: 5, Text: "\tw := &lineFilter{w: os.Stdout, f: shortenCoverLine}", Marks: []webproto.SearchMark{{S: 30, E: 30 + len(a.Query)}}},
						webproto.SearchHit{Path: "internal/report/cover.go", Line: 88, Col: 2, Text: "\t// lineFilter keeps the coverage line short", Marks: []webproto.SearchMark{{S: 4, E: 4 + len(a.Query)}}})
					_ = c.send(&webproto.Event{T: webproto.KindEv, Kind: webproto.EventSearchBatch, Data: raw(webproto.SearchBatch{ID: uint64(v.ID), Results: hits})})
					data = webproto.SearchSummary{Files: 3, Matches: len(hits), Ms: 412}
				case "db.conns":
					data = webproto.DBConns{Conns: []webproto.DBConn{
						{ID: "local", Name: "shop (local)", Driver: "postgres", Database: "shop", SQL: true},
						{ID: "prod", Name: "shop (production)", Driver: "postgres", Database: "shop", Env: "production", ReadOnly: true, SQL: true},
						{ID: "cache", Name: "cache", Driver: "redis"},
					}}
				case "db.tables":
					data = webproto.DBTables{Conn: "local", Schema: "public", Tables: []webproto.DBTable{{Name: "customers"}, {Name: "orders"}, {Name: "order_items"}, {Name: "products"}, {Name: "sales_by_month", View: true}}}
				case "db.rows", "db.query":
					cell := func(s string) webproto.DBCell { return webproto.DBCell{V: s} }
					data = webproto.DBRows{Columns: []string{"id", "email", "name", "total", "api_token", "created_at"}, Ms: 14, Masked: 3, Rows: [][]webproto.DBCell{
						{cell("1"), cell("ann@example.com"), cell("Ann Lee"), cell("1250.00"), {V: "••••", M: true}, cell("2026-10-01 09:12:44")},
						{cell("2"), cell("bo@example.com"), cell("Bo Chen"), cell("89.90"), {V: "••••", M: true}, cell("2026-10-02 17:03:10")},
						{cell("3"), cell("cy@example.com"), {N: true}, cell("0.00"), {N: true}, cell("2026-10-05 11:40:02")},
						{cell("4"), cell("dee@example.com"), cell("Dee Park"), cell("312.45"), {V: "••••", M: true}, cell("2026-10-09 08:00:00")},
					}}
				case "containers.list":
					data = webproto.ContainersList{Engine: "docker", Context: "default", Containers: []webproto.ContainerItem{
						{ID: "9f3c2e1a1234", Name: "shop-api-1", Image: "ghcr.io/acme/api:1.4", State: "running", Status: "Up 2 hours (healthy)", Ports: "0.0.0.0:8081->8080/tcp", Project: "shop"},
						{ID: "9f3c2e1a5678", Name: "shop-db-1", Image: "postgres:17", State: "running", Status: "Up 2 hours", Ports: "5432/tcp", Project: "shop"},
						{ID: "aabbccdd1122", Name: "old-nginx", Image: "nginx:1.27", State: "exited", Status: "Exited (1) 3 days ago"},
						{ID: "aabbccdd3344", Name: "redis-cache", Image: "redis:7", State: "paused", Status: "Up 5 days (Paused)"},
					}}
				case "containers.logs":
					data = webproto.ContainerLogs{ID: "9f3c2e1a1234", Lines: []string{"2026-10-30T09:12:44Z listening on :8080", "2026-10-30T09:12:45Z connected to postgres shop-db-1:5432", "2026-10-30T09:13:02Z GET /api/orders 200 14ms", "2026-10-30T09:13:05Z token=[REDACTED]"}, Redacted: 1}
				case "tasks.tail":
					data = webproto.TaskTail{Buf: 8, Lines: []string{"=== RUN   TestShorten", "--- PASS: TestShorten (0.00s)", "ok  \tgithub.com/amin-jalali/termocode/cmd/covreport\t0.412s"}}
				case "git.graph":
					data = graphRows()
				case "git.rebase_plan":
					data = rebasePlan()
				case "git.conflict":
					data = mergeConflict()
				case "pr.list":
					data = webproto.PRList{Items: []webproto.PRItem{prItem(), {Number: 13, Title: "WIP cache", Author: "bob", Branch: "wip/cache", Base: "main", Draft: true, Checks: "none"}}}
				case "ci.status":
					data = ciStatus()
				case "pr.view":
					data = prViewData()
				case "git.show":
					data = webproto.GitDiff{Commit: &webproto.GitCommitInfo{Hash: "cccc333", Subject: "Feature work", Body: "Why it changed.", Author: "Bob"},
						Rows: []webproto.DiffRow{{K: "file", Text: "src/a.go"}, {K: "add", Text: "x"}, {K: "del", Text: "z"}, {K: "file", Text: "README.md"}, {K: "add", Text: "y"}}}
				case "buf.set_text":
					tick++
					data = webproto.EditRes{Tick: tick, Dirty: true}
				case "ai.next_edit":
					var a webproto.AINextEditArgs
					_ = json.Unmarshal(v.Args, &a)
					data = webproto.AINextEdit{None: true, Old: []string{}, Lines: []string{}}
					if !a.Seed && tailRow > 0 {
						data = webproto.AINextEdit{Start: tailRow, End: tailRow, Old: []string{lines[tailRow]}, Lines: []string{"\ttail := \"\" // the line ends without a newline"}}
					}
				case "ai.complete":
					data = webproto.AICompletion{None: true}
				case "ai.action":
					data = webproto.AIAction{Kind: "doc", Title: "AI doc comment", Start: 0, End: 1, Lines: []string{"// Command covreport shortens the package list of coverage lines.", "package main"}}
				case "ai.worktrees":
					data = webproto.AIWorktrees{Items: []webproto.AIWorktree{
						{ID: "r1-left", Branch: "termocode/agent-r1-left", Owner: "Add a test", Files: []webproto.AIWorktreeChange{{Path: "cmd/covreport/filter.go", Status: "M"}, {Path: "cmd/covreport/filter_test.go", Status: "A"}}},
						{ID: "r1-right", Branch: "termocode/agent-r1-right", Owner: "Docs", Files: []webproto.AIWorktreeChange{{Path: "README.md", Status: "M"}}},
						{ID: "r1-join", Branch: "termocode/agent-r1-join", Running: true, Files: []webproto.AIWorktreeChange{}},
					}}
				case "ai.worktree_text":
					after := strings.Replace(shotFile, "\tif i < 0 {", "\tif i < 0 || line == \"\" {", 1)
					data = webproto.AIWorktreeText{ID: "r1-left", Path: "cmd/covreport/filter.go", Before: shotFile, After: after}
				case "buf.hl":
					var a webproto.HlArgs
					_ = json.Unmarshal(v.Args, &a)
					res, _ := webhl.Chroma("filter.go", "go", lines, 3, a.First, a.Last, webproto.MaxHlSpans)
					data = res
				default:
					d, ok := runShotAnswer(v.Op)
					if !ok {
						_ = c.send(&webproto.ErrMsg{T: webproto.KindErr, ID: v.ID, Code: webproto.CodeNotFound, Msg: "Not in this test."})
						continue
					}
					data = d
				}
				_ = c.send(&webproto.Res{T: webproto.KindRes, ID: v.ID, Data: raw(data)})
			}
		}
	}()
}

func TestShots(t *testing.T) {
	dir := os.Getenv(shotsEnv)
	if dir == "" {
		t.Skip("set " + shotsEnv + " to write screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	wide := Viewport{Name: "desktop", W: 1600, H: 960, Scale: 1}
	for _, vp := range []Viewport{wide, Phone} {
		for _, scheme := range []string{"dark", "light"} {
			t.Run(vp.Name+"-"+scheme, func(t *testing.T) {
				s := StartServer(t)
				p := NewPage(t, s, vp)
				p.Media(map[string]string{"prefers-reduced-motion": "reduce", "prefers-color-scheme": scheme})
				c := p.Login("c-shot")
				shotHub(t, c)
				if !vp.Mobile { // the workbench asked for the AI at login
					c.Send(t, Snap("ai", 1, aiFixture(true, true)))
				}
				shot := func(name, hash, ready string) {
					Eval[bool](p, `(location.hash = `+jsq(hash)+`, true)`)
					snap(t, p, filepath.Join(dir, vp.Name+"-"+scheme+"-"+name+".png"), ready)
				}
				fileReady := `document.querySelector('.crow .hl-k')`
				if !vp.Mobile {
					fileReady = `document.querySelector(".desk-editor")?.contentDocument?.querySelector(".view-line .hl-k")`
				}
				shot("file", "#/files/cmd/covreport/filter.go", fileReady)
				shot("chat", "#/chat", `document.querySelector('[data-view="chat"] .msg')`)
				shot("term", "#/term", `document.querySelector('[data-view="term"] .trow')`)
				shot("git", "#/git", `document.querySelector('.git-row-wrap, .tr[data-group="changes"]')`)
				if !vp.Mobile {
					// Every view of the activity bar, with data.
					has := func(text string) string { return `document.querySelector(".wb").innerText.includes(` + jsq(text) + `)` }
					Eval[bool](p, `(location.hash = "#/more/search", true)`)
					p.WaitJS(`document.querySelector(".wb-side input")`)
					Eval[bool](p, `(() => { const i = document.querySelector(".wb-side input"); i.focus(); i.value = "lineFilter"; i.dispatchEvent(new Event("input", { bubbles: true })); return true; })()`)
					p.Key("\r")
					snap(t, p, filepath.Join(dir, vp.Name+"-"+scheme+"-search.png"), has("cover.go"))
					shot("run", "#/more/tasks", has("fix the toast flicker")+` && `+has("lint"))
					shot("db", "#/more/db", has("shop (local)"))
					p.TapJS(`document.querySelector('.wb-side .tr[data-conn="local"]')`)
					snap(t, p, filepath.Join(dir, vp.Name+"-"+scheme+"-db-tables.png"), has("customers"))
					p.TapJS(`document.querySelector('.wb-side .tr[data-table="customers"]')`)
					snap(t, p, filepath.Join(dir, vp.Name+"-"+scheme+"-db-rows.png"), has("ann@example.com"))
					Eval[bool](p, `(document.querySelector('.wb-side .tr[data-conn="local"] [data-act], .wb-side .tr[data-conn="local"] .act').click(), true)`)
					snap(t, p, filepath.Join(dir, vp.Name+"-"+scheme+"-db-query.png"), `document.querySelector('[data-view="db-query"] .desk-editor')?.contentDocument?.querySelector(".view-lines")`)
					shot("containers", "#/more/containers", has("shop-api-1"))
					p.TapJS(`document.querySelector('.wb-side .tr[data-id="9f3c2e1a1234"]')`)
					snap(t, p, filepath.Join(dir, vp.Name+"-"+scheme+"-containers-logs.png"), `document.querySelector('.wb-panel [data-view="output"] .outv-line')`)
					shot("mcp", "#/more/mcp", has("claude-desktop"))
					p.TapJS(`document.querySelector('.wb-side .tr[data-agent="claude-desktop"]')`)
					snap(t, p, filepath.Join(dir, vp.Name+"-"+scheme+"-mcp-agent.png"), `document.querySelector('[data-view="mcp-agent"]')`)
					shot("settings", "#/more/settings", has("format_on_save")+` || `+has("Format On Save"))
					shot("device", "#/more", `document.querySelector('[data-view="more"], [data-view="device"]')`)
					diffDoc := `document.querySelector('[data-view="diff"] .desk-editor')?.contentDocument`
					shot("diff", "#/git/cmd/covreport/filter.go", diffDoc+`?.querySelector(".editor.modified .char-insert") && `+diffDoc+`.querySelector(".editor.modified .view-line .hl-kc")`)
				}
				for _, rs := range runShots(vp) {
					shot(rs.name, rs.hash, rs.ready)
				}
				if !vp.Mobile {
					shot("graph", "#/files/@git:graph", `document.querySelectorAll('tr.gg-row').length === 4`)
					Eval[bool](p, `(document.querySelector('tr.gg-row[data-hash="cccc333"]').click(), true)`)
					shot("graph-open", "#/files/@git:graph", `document.querySelectorAll('.gg-details .gg-file').length === 2`)
					shot("rebase", "#/files/@git:rebase:aaaa111", `document.querySelectorAll('li.gr-row').length === 3 && (() => {
						const set = (h, a) => { const s = document.querySelector('li.gr-row[data-hash="' + h + '"] select'); if (s.value !== a) { s.value = a; s.dispatchEvent(new Event("change", {bubbles: true})); } };
						set("bbbb222", "reword"); set("cccc333", "fixup"); return !!document.querySelector("textarea.gr-msg"); })()`)
					shot("prs", "#/files/@git:prs", `document.querySelectorAll('[data-view="prs"] .pr-row').length === 2`)
					shot("pr", "#/files/@git:pr:14", `document.querySelector('[data-view="pr"] .pr-md')`)
					shot("merge", "#/files/@git:merge:src/a.go", `(() => { const f = [...document.querySelectorAll('[data-view="merge"] .desk-editor')].map((e) => e.contentDocument); return f.length === 3 && f.every((d) => d && d.querySelector(".tc-mg-current, .tc-mg-incoming")); })()`)
				}
				if vp.Mobile {
					return
				}
				// The laptop's AI (WEBP-3): agent worktrees, an agent's change,
				// the next edit suggestion and an AI suggestion to accept.
				shot("agents", "#/more/agents", `document.querySelectorAll('.agent-file').length === 3`)
				shot("agentdiff", "#/files/@agent:r1-left:cmd/covreport/filter.go", `document.querySelector('[data-view="ai-diff"] .desk-editor')?.contentDocument?.querySelector(".editor.modified .line-insert, .editor.modified .char-insert")`)
				Eval[bool](p, `(location.hash = "#/files/cmd/covreport/filter.go", true)`)
				p.WaitJS(fileReady)
				deskCaret(p, 3, 6)
				p.Do(chromedp.KeyEvent(" "))
				shot("nes", "#/files/cmd/covreport/filter.go", `document.querySelector(".desk-editor")?.contentDocument?.querySelector(".tc-nes")`)
				p.Do(chromedp.KeyEvent(kb.Escape))
				editorRightClick(p)
				menuPick(p, "Generate Doc Comment with AI")
				p.WaitJS(`document.querySelector('[data-view="ai-diff"]')`)
				shot("aidiff", Eval[string](p, `location.hash`), `document.querySelector('[data-view="ai-diff"] .desk-editor')?.contentDocument?.querySelector(".editor.modified .line-insert, .editor.modified .char-insert")`)
			})
		}
	}
}

// snap waits for ready (and the fonts) and writes a screenshot to file.
func snap(t *testing.T, p *Page, file, ready string) {
	t.Helper()
	p.WaitJS(ready + ` && document.fonts.status === "loaded"`)
	ctx, cancel := context.WithTimeout(p.Ctx, 10*time.Second)
	defer cancel()
	png, err := chromedp.Run(ctx, chromedp.CaptureScreenshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, png, 0o644); err != nil {
		t.Fatal(err)
	}
}

// The workbench's layout at the common laptop screen sizes, in its
// states: the panel shown, maximized and on the right, the side bar
// hidden (WEB_SHOTS_DIR as TestShots).
func TestLayoutShots(t *testing.T) {
	dir := os.Getenv(shotsEnv)
	if dir == "" {
		t.Skip("set " + shotsEnv + " to write screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, vp := range []Viewport{{Name: "1280", W: 1280, H: 800, Scale: 1}, {Name: "1440", W: 1440, H: 900, Scale: 1}, {Name: "1920", W: 1920, H: 1080, Scale: 1}} {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			p.Media(map[string]string{"prefers-reduced-motion": "reduce", "prefers-color-scheme": "dark"})
			c := p.Login("c-shot")
			shotHub(t, c)
			name := func(n string) string { return filepath.Join(dir, "layout-"+vp.Name+"-"+n+".png") }
			fileReady := `document.querySelector(".desk-editor")?.contentDocument?.querySelector(".view-line .hl-k")`
			Eval[bool](p, `(location.hash = "#/files/cmd/covreport/filter.go", true)`)
			snap(t, p, name("editor"), fileReady)
			Eval[bool](p, `(location.hash = "#/term", true)`)
			snap(t, p, name("panel"), fileReady+` && document.querySelector(".wb-panel .trow")`)
			layoutKey(p, "b")
			snap(t, p, name("noside"), `!document.querySelector(".wb-side")`)
			layoutKey(p, "b")
			p.TapJS(`document.querySelector('.wb-panel [data-act="panel-max"]')`)
			snap(t, p, name("panelmax"), `document.querySelector(".wb.is-panel-max")`)
			p.TapJS(`document.querySelector('.wb-panel [data-act="panel-max"]')`)
			p.TapJS(`document.querySelector('.wb-panel [data-act="panel-more"]')`)
			p.TapJS(`[...document.querySelectorAll(".ctx-menu button, [role=menuitem]")].find((b) => b.textContent.includes("Move Panel Right"))`)
			snap(t, p, name("panelright"), `document.querySelector(".wb.is-panel-right")`)
			Eval[bool](p, `(location.hash = "#/git/cmd/covreport/filter.go", true)`)
			diffDoc := `document.querySelector('[data-view="diff"] .desk-editor')?.contentDocument`
			snap(t, p, name("diff"), diffDoc+`?.querySelector(".editor.modified .char-insert")`)
		})
	}
}

// layoutKey presses Ctrl/Cmd and k on the page.
func layoutKey(p *Page, k string) {
	p.T.Helper()
	Eval[bool](p, `(dispatchEvent(new KeyboardEvent("keydown", { key: `+jsq(k)+`, ctrlKey: true, bubbles: true })), true)`)
}
