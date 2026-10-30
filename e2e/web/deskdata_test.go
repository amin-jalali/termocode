//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop's Databases, Containers and MCP views (views/desk).

func deskLogin(t *testing.T, meta *webproto.Meta) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	if meta == nil {
		meta = Meta("c-desk")
	}
	return p, p.LoginMeta(meta)
}

func dbConns() webproto.DBConns {
	return webproto.DBConns{Conns: []webproto.DBConn{
		{ID: "local", Driver: "postgres", Database: "app", SQL: true},
		{ID: "prod", Driver: "postgres", Database: "app", Env: "production", ReadOnly: true, SQL: true},
		{ID: "cache", Driver: "redis"},
	}}
}

// Databases: a tree of connections, schema, tables and views; a table's
// rows open in an editor tab as a grid (sticky header, numbers right,
// NULL dim, masked values as ••••, text never HTML, pages); New Query
// opens a SQL editor tab: Ctrl/Cmd+Enter asks first (Cancel first and
// focused), a read runs, a change asks the laptop.
func TestDeskDBTreeGridAndQuery(t *testing.T) {
	p, c := deskLogin(t, nil)
	Eval[bool](p, `(location.hash = "#/more/db", true)`)
	reply(t, c, expectOp(t, c, webproto.OpDBConns, `{}`), dbConns())
	const side = `document.querySelector('.wb-side [data-view="db"]')`
	p.WaitJS(side + `?.querySelectorAll(".tr[data-conn]").length === 3`)
	if Eval[bool](p, `!!`+side+`.querySelector('.tr[data-conn="cache"][data-twist]')`) {
		t.Error("a Redis connection opens here")
	}
	if s := Eval[string](p, side+`.querySelector('.tr[data-conn="prod"] .db-env.is-prod')?.textContent ?? ""`); s != "production" {
		t.Errorf("prod mark %q", s)
	}
	p.TapJS(side + `.querySelector('.tr[data-conn="local"]')`)
	reply(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"local"}`), webproto.DBTables{Conn: "local", Schema: "public", Tables: []webproto.DBTable{{Name: "users"}, {Name: "active", View: true}}})
	p.WaitJS(side + `.querySelector('.tr[data-table="users"]') && ` + side + `.querySelector('.tr[data-table="active"] .cod-eye')`)
	p.TapJS(side + `.querySelector('.tr[data-table="users"]')`)
	reply(t, c, expectOp(t, c, webproto.OpDBRows, `{"conn":"local","table":"users","from":0}`), webproto.DBRows{
		Columns: []string{"id", "email", "password"},
		Rows:    [][]webproto.DBCell{{{V: "1"}, {V: "<b>ada</b>"}, {V: webproto.DBMaskedValue, M: true}}, {{V: "20"}, {N: true}, {V: webproto.DBMaskedValue, M: true}}},
		Next:    100, Masked: 2})
	const grid = `document.querySelector('[data-view="db-table"] .dbg-table')`
	p.WaitJS(grid + `?.querySelectorAll("tbody tr").length === 2`)
	if got := Eval[string](p, `document.querySelector(".ed-tab.is-on .ed-tab-name").textContent`); got != "users" {
		t.Errorf("tab %q", got)
	}
	if n := Eval[int](p, grid+`.querySelectorAll("b").length`); n != 0 {
		t.Fatal("a cell became HTML")
	}
	if got := Eval[string](p, `[...`+grid+`.querySelectorAll("tbody tr:nth-child(2) td")].map((td) => td.textContent + (td.classList.contains("is-num") ? "#" : "") + (td.classList.contains("is-null") ? "~" : "")).join("|")`); got != "2|20#|NULL~|••••" {
		t.Errorf("row 2 %q", got)
	}
	if !Eval[bool](p, `getComputedStyle(`+grid+`.querySelector("thead th")).position === "sticky"`) {
		t.Error("the header does not stay on top")
	}
	p.TapJS(`document.querySelector('[data-view="db-table"] [aria-label="Next rows"]')`)
	reply(t, c, expectOp(t, c, webproto.OpDBRows, `{"conn":"local","table":"users","from":100}`), webproto.DBRows{Columns: []string{"id"}, Rows: [][]webproto.DBCell{{{V: "101"}}}, From: 100})
	p.WaitJS(grid + `?.querySelectorAll("tbody tr").length === 1 && ` + grid + `.querySelector("tbody td").textContent === "101"`)

	// New Query: a SQL editor tab.
	Eval[bool](p, side+`.querySelector('.tr[data-conn="local"] [aria-label="New Query"]').click() ?? true`)
	const qdoc = `document.querySelector('[data-view="db-query"] .desk-editor')?.contentDocument`
	p.WaitJS(qdoc + `?.querySelector(".view-lines")`)
	Eval[bool](p, `(document.querySelector('[data-view="db-query"] .desk-editor').focus(), `+qdoc+`.querySelector(".native-edit-context, textarea").focus(), true)`)
	p.Do(chromedp.KeyEvent("SELECT count(*) FROM users"))
	p.WaitJS(qdoc + `.querySelector(".view-lines .mtk1, .view-lines span")`)
	p.Do(chromedp.KeyEvent(kb.Enter, modKey()))
	p.WaitJS(`document.querySelector('[data-view="db-query"] .pv-ask')?.textContent.includes("Run this read on local?") && document.activeElement?.textContent === "Cancel"`)
	p.Key(kb.Enter) // Cancel
	p.WaitJS(`!document.querySelector('[data-view="db-query"] .pv-ask')`)
	c.Quiet(t, 200*time.Millisecond)
	p.TapJS(`document.querySelector('.ed-actions [data-act="run-query"]')`)
	p.TapJS(`[...document.querySelectorAll('[data-view="db-query"] .pv-ask button')].find((b) => b.textContent === "Run query")`)
	r := expectOp(t, c, webproto.OpDBQuery, "")
	if !strings.Contains(string(r.Args), "SELECT count(*) FROM users") {
		t.Fatalf("query args %s", r.Args)
	}
	reply(t, c, r, webproto.DBRows{Columns: []string{"count"}, Rows: [][]webproto.DBCell{{{V: "2"}}}, Ms: 3})
	p.WaitJS(`document.querySelector('[data-view="db-query"] .dbg-table tbody td.is-num')?.textContent === "2"`)
	// A change asks the laptop.
	p.TapJS(`document.querySelector('.ed-actions [data-act="run-query"]')`)
	p.TapJS(`[...document.querySelectorAll('[data-view="db-query"] .pv-ask button')].find((b) => b.textContent === "Run query")`)
	r = expectOp(t, c, webproto.OpDBQuery, "")
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeForbidden, Msg: "This statement may change data. It needs the laptop."})
	p.WaitJS(`document.querySelector(".ask-text")?.textContent === "This may change data on local. Ask the laptop to run it?"`)
}

// Containers: grouped by compose project with state icons; Logs show in
// the panel's OUTPUT tab (text, secrets hidden); Restart asks in the
// view; Stop asks the laptop.
func TestDeskContainers(t *testing.T) {
	p, c := deskLogin(t, nil)
	Eval[bool](p, `(location.hash = "#/more/containers", true)`)
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	const side = `document.querySelector('.wb-side [data-view="containers"]')`
	p.WaitJS(side + `?.querySelectorAll(".tr[data-id]").length === 2`)
	if !Eval[bool](p, `!!`+side+`.querySelector('.tr[data-project="shop"]') && !!`+side+`.querySelector('.tr[data-id="9f3c2e1a1234"] .c-ok .cod-vm-running') && !!`+side+`.querySelector('.tr[data-id="aabbccdd1122"] .c-err')`) {
		t.Error("projects or state icons are wrong")
	}
	p.TapJS(side + `.querySelector('.tr[data-id="9f3c2e1a1234"]')`)
	reply(t, c, expectOp(t, c, webproto.OpContainersLogs, `{"id":"9f3c2e1a1234"}`), webproto.ContainerLogs{ID: "9f3c2e1a1234", Lines: []string{"start", "<b>ok</b> [REDACTED]"}, Redacted: 1})
	p.WaitJS(`document.querySelectorAll('.wb-panel [data-view="output"] .outv-line').length === 2`)
	if n := Eval[int](p, `document.querySelectorAll('.wb-panel [data-view="output"] b').length`); n != 0 {
		t.Fatal("log text became HTML")
	}
	Eval[bool](p, side+`.querySelector('.tr[data-id="9f3c2e1a1234"] [aria-label="Restart"]').click() ?? true`)
	p.WaitJS(side + `.querySelector(".pv-ask")?.textContent.includes("Restart api-1?") && document.activeElement?.textContent === "Cancel"`)
	p.TapJS(`[...` + side + `.querySelectorAll(".pv-ask button")].find((b) => b.textContent === "Restart")`)
	reply(t, c, expectOp(t, c, webproto.OpContainersRestart, `{"id":"9f3c2e1a1234"}`), webproto.ContainerDone{State: "running"})
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	Eval[bool](p, side+`.querySelector('.tr[data-id="9f3c2e1a1234"] [aria-label="Stop…"]').click() ?? true`)
	p.WaitJS(`document.querySelector(".ask-text")?.textContent === "Stop api-1? What runs in it ends."`)
}

// MCP: the server with its state, the agents, the last calls; an agent's
// details open in an editor tab. No control changes anything.
func TestDeskMCP(t *testing.T) {
	p, c := deskLogin(t, nil)
	Eval[bool](p, `(location.hash = "#/more/mcp", true)`)
	c.WaitSub(t, "mcp")
	c.Send(t, Snap("mcp", 1, webproto.MCPSnap{Mode: "auto", Listening: true,
		Agents: []webproto.MCPAgent{{Name: "agent-x", Since: 1_700_000_000_000, Calls: 14, Asked: 2}},
		Calls:  []string{"12:03 agent-x · read_file · ran without asking", "12:04 agent-x · run_task · asked, you allowed"}}))
	const side = `document.querySelector('.wb-side [data-view="mcp"]')`
	p.WaitJS(side + `?.querySelector('.tr[data-server] .mcp-dot.is-on') && ` + side + `.querySelector('.tr[data-agent="agent-x"]')`)
	if got := Eval[string](p, `[...`+side+`.querySelectorAll('[data-section="calls"] .tr')].map((r) => r.querySelector(".tr-label").textContent + (r.querySelector(".cod-shield") ? "?" : "")).join(",")`); got != "read_file,run_task?" {
		t.Errorf("calls %q", got)
	}
	if n := Eval[int](p, side+`.querySelectorAll("input, textarea, .tr button").length`); n != 0 {
		t.Errorf("%d controls in the MCP view", n)
	}
	p.TapJS(side + `.querySelector('.tr[data-agent="agent-x"]')`)
	p.WaitJS(`document.querySelector('[data-view="mcp-agent"] h1')?.textContent === "agent-x" && document.querySelectorAll('[data-view="mcp-agent"] tbody tr').length === 2`)
}
