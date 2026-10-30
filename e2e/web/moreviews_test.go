//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The v2 pages under More (WEB-43 … WEB-46) against the fake hub.

// openMoreSub logs in and opens More › the link of sub, which asks for
// topic.
func openMoreSub(t *testing.T, vp Viewport, sub, topic string) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-more2")
	p.GoTab("more")
	p.TapJS(`document.querySelector('.more-link[data-sub="` + sub + `"]')`)
	c.WaitSub(t, topic)
	return p, c
}

// Settings list the laptop's values by group, read only: a changed value
// shows its default, there is no field and no button that changes one.
func TestSettingsReadOnly(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openMoreSub(t, vp, "settings", "settings")
			c.Send(t, Snap("settings", 1, webproto.SettingsSnap{Items: []webproto.SettingItem{
				{Key: "theme", Group: "Appearance", Type: "string", Value: "one-dark", Default: "vscode-dark-plus", Description: "Color theme ID."},
				{Key: "tab_size", Group: "Editor", Type: "int", Value: "4", Default: "4"},
				{Key: "word_wrap", Group: "Editor", Type: "bool", Value: "<b>true</b>", Default: "false"},
			}}))
			p.WaitJS(`document.querySelectorAll(".kv-row").length === 3`)
			if n := Eval[int](p, `document.querySelectorAll(".settings .group-title").length`); n != 2 {
				t.Errorf("%d groups", n)
			}
			if v := p.Text(`.kv-row[data-key="theme"] .kv-value`); v != "one-dark" {
				t.Errorf("theme value %q", v)
			}
			if d := p.Text(`.kv-row[data-key="theme"] .kv-def`); d != "default vscode-dark-plus" {
				t.Errorf("theme default %q", d)
			}
			if n := Eval[int](p, `document.querySelectorAll('.kv-row[data-key="tab_size"] .kv-def').length`); n != 0 {
				t.Error("an unchanged value shows its default")
			}
			if n := Eval[int](p, `document.querySelectorAll(".settings b, .settings input, .settings select, .settings textarea, .settings .btn").length`); n != 0 {
				t.Fatalf("%d fields, buttons or HTML on the settings page", n)
			}
			if !strings.Contains(p.Text(".settings-note"), "Read only") {
				t.Errorf("note %q", p.Text(".settings-note"))
			}
			c.Send(t, Delta("settings", 2, webproto.SettingsSnap{Items: []webproto.SettingItem{{Key: "tab_size", Group: "Editor", Type: "int", Value: "8", Default: "4"}}}))
			p.WaitJS(`document.querySelectorAll(".kv-row").length === 1`)
			p.noOverflow("settings")
		})
	}
}

// MCP shows the server's state, the agents and their last calls; there
// is no button at all; not listening says why.
func TestMCPStatusReadOnly(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openMoreSub(t, vp, "mcp", "mcp")
			c.Send(t, Snap("mcp", 1, webproto.MCPSnap{Mode: "auto", Listening: true,
				Agents: []webproto.MCPAgent{{Name: "agent-x", Since: 1_700_000_000_000, Calls: 14, Asked: 2}, {Since: 1_700_000_000_000}},
				Calls:  []string{"12:03 agent-x · read_file · ran without asking"}}))
			p.WaitJS(`document.querySelectorAll(".mcp-agent").length === 2`)
			if s := p.Text(".mcp-state"); s != "Listening for outside agents." {
				t.Errorf("state %q", s)
			}
			if m := p.Text(".mcp-agent .mcp-meta"); !strings.Contains(m, "14 calls, 2 asked") {
				t.Errorf("meta %q", m)
			}
			if n := Eval[string](p, `document.querySelectorAll(".mcp-agent .mcp-name")[1].textContent`); n != "(no name yet)" {
				t.Errorf("unnamed agent %q", n)
			}
			if n := Eval[int](p, `document.querySelectorAll(".mcpv button, .mcpv input").length`); n != 0 {
				t.Fatalf("%d controls on the MCP page", n)
			}
			c.Send(t, Delta("mcp", 2, webproto.MCPSnap{Mode: "off", Why: "The MCP server is turned off.", Agents: []webproto.MCPAgent{}, Calls: []string{}}))
			p.WaitJS(`document.querySelector(".mcp-why")?.textContent === "The MCP server is turned off."`)
			if s := p.Text(".mcp-state"); s != "Not listening." {
				t.Errorf("state %q", s)
			}
			p.noOverflow("mcp")
		})
	}
}

func ctrList() webproto.ContainersList {
	return webproto.ContainersList{Engine: "docker", Context: "context default", Containers: []webproto.ContainerItem{
		{ID: "9f3c2e1a1234", Name: "api-1", Image: "ghcr.io/acme/api:1.4", State: "running", Status: "Up 2 hours", Ports: "0.0.0.0:8081->8080/tcp", Project: "shop"},
		{ID: "aabbccdd1122", Name: "old", Image: "nginx:1.27", State: "exited", Status: "Exited (1) 3 days ago"},
	}}
}

// openContainers opens More › Containers and answers its list.
func openContainers(t *testing.T, vp Viewport, meta *webproto.Meta) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	if meta == nil {
		meta = Meta("c-ctr")
	}
	c := p.LoginMeta(meta)
	p.GoTab("more")
	p.TapJS(`document.querySelector('.more-link[data-sub="containers"]')`)
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	p.WaitJS(`document.querySelectorAll(".ctr").length === 2`)
	return p, c
}

func ctrSel(id string) string { return `document.querySelector('.ctr[data-id="` + id + `"]')` }

// Containers: the list, a log tail as text, restart asked in the card,
// stop through the laptop; a stopped one offers start and remove.
func TestContainersView(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openContainers(t, vp, nil)
			if m := p.Text(`.ctr[data-id="9f3c2e1a1234"] .ctr-meta`); !strings.Contains(m, "ghcr.io/acme/api:1.4 · shop · Up 2 hours") {
				t.Errorf("meta %q", m)
			}
			p.TapJS(ctrSel("9f3c2e1a1234") + `.querySelector(".ctr-line")`)
			p.TapJS(`[...` + ctrSel("9f3c2e1a1234") + `.querySelectorAll(".btn")].find((b) => b.textContent === "Logs")`)
			reply(t, c, expectOp(t, c, webproto.OpContainersLogs, `{"id":"9f3c2e1a1234"}`), webproto.ContainerLogs{ID: "9f3c2e1a1234", Lines: []string{"start", "<b>ok</b> [REDACTED]"}, Redacted: 1})
			p.WaitJS(ctrSel("9f3c2e1a1234") + `.querySelectorAll(".trow").length === 2`)
			if n := Eval[int](p, `document.querySelectorAll(".ctr-logs b").length`); n != 0 {
				t.Fatal("log text became HTML")
			}
			if !strings.Contains(p.Text(".ctr-note"), "Secret values hidden: 1.") {
				t.Errorf("note %q", p.Text(".ctr-note"))
			}
			if n := Eval[int](p, `[...`+ctrSel("9f3c2e1a1234")+`.querySelectorAll(".btn")].filter((b) => b.textContent === "Start" || b.textContent === "Remove…").length`); n != 0 {
				t.Error("a running container offers start or remove")
			}
			p.TapJS(`[...` + ctrSel("9f3c2e1a1234") + `.querySelectorAll(".btn")].find((b) => b.textContent === "Restart")`)
			p.WaitJS(`document.activeElement === document.querySelector(".row-ask button")`)
			p.Key(kb.Enter) // Cancel
			c.Quiet(t, 200*time.Millisecond)
			p.TapJS(`[...` + ctrSel("9f3c2e1a1234") + `.querySelectorAll(".btn")].find((b) => b.textContent === "Restart")`)
			p.TapJS(`[...document.querySelectorAll(".row-ask .btn")].find((b) => b.textContent === "Restart")`)
			reply(t, c, expectOp(t, c, webproto.OpContainersRestart, `{"id":"9f3c2e1a1234"}`), webproto.ContainerDone{State: "running"})
			reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
			p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("api-1 is running now.")`)

			p.TapJS(`[...` + ctrSel("9f3c2e1a1234") + `.querySelectorAll(".btn")].find((b) => b.textContent === "Stop…")`)
			p.WaitJS(`document.querySelector(".ask-text")?.textContent === "Stop api-1? What runs in it ends."`)
			p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
			p.TapJS(ButtonByText("Ask the laptop"))
			reply(t, c, expectOp(t, c, webproto.OpContainersStop, `{"id":"9f3c2e1a1234"}`), webproto.AskRef{Card: "card-11"})
			c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: "card-11", By: "laptop", Choice: "confirm"})})
			reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
			p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Done on the laptop.")`)

			p.TapJS(ctrSel("aabbccdd1122") + `.querySelector(".ctr-line")`)
			if n := Eval[int](p, `[...`+ctrSel("aabbccdd1122")+`.querySelectorAll(".btn")].filter((b) => b.textContent === "Start" || b.textContent === "Remove…").length`); n != 2 {
				t.Errorf("a stopped container offers %d of start and remove", n)
			}
			p.noOverflow("containers")
		})
	}
}

// A viewer reads the list and the logs only.
func TestContainersViewer(t *testing.T) {
	m := Meta("c-v")
	m.Role = webproto.RoleViewer
	p, _ := openContainers(t, Phone, m)
	p.TapJS(ctrSel("9f3c2e1a1234") + `.querySelector(".ctr-line")`)
	if got := Eval[[]string](p, `[...`+ctrSel("9f3c2e1a1234")+`.querySelectorAll(".ctr-actions .btn")].map((b) => b.textContent)`); len(got) != 1 || got[0] != "Logs" {
		t.Fatalf("viewer buttons %q", got)
	}
}

// openDB opens More › Databases and answers the connection list.
func openDB(t *testing.T, vp Viewport, meta *webproto.Meta) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	if meta == nil {
		meta = Meta("c-db")
	}
	c := p.LoginMeta(meta)
	p.GoTab("more")
	p.TapJS(`document.querySelector('.more-link[data-sub="db"]')`)
	reply(t, c, expectOp(t, c, webproto.OpDBConns, `{}`), webproto.DBConns{Conns: []webproto.DBConn{
		{ID: "local", Driver: "postgres", Database: "app", SQL: true},
		{ID: "prod", Driver: "postgres", Database: "app", Env: "production", SQL: true},
		{ID: "cache", Driver: "redis"},
	}})
	p.WaitJS(`document.querySelectorAll(".db-conn").length === 3`)
	return p, c
}

// Databases: connections (no host shown, a Redis one only on the
// laptop), tables, rows with NULL and masked cells as text; a read query
// asks in the page; a write asks the laptop.
func TestDBView(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openDB(t, vp, nil)
			if !Eval[bool](p, `document.querySelector('.db-conn[data-conn="cache"]').disabled`) {
				t.Error("a Redis connection can be opened here")
			}
			if s := p.Text(`.db-conn[data-conn="prod"] .db-chip.is-prod`); s != "production" {
				t.Errorf("prod chip %q", s)
			}
			p.TapJS(`document.querySelector('.db-conn[data-conn="local"]')`)
			reply(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"local"}`), webproto.DBTables{Conn: "local", Schema: "public",
				Tables: []webproto.DBTable{{Name: "users"}, {Name: "active", View: true}}})
			p.WaitJS(`document.querySelectorAll(".db-table").length === 2`)
			p.TapJS(`document.querySelector('.db-table[data-table="users"]')`)
			reply(t, c, expectOp(t, c, webproto.OpDBRows, `{"conn":"local","table":"users","from":0}`), webproto.DBRows{
				Columns: []string{"id", "email", "password"},
				Rows:    [][]webproto.DBCell{{{V: "1"}, {V: "<b>ada</b>"}, {V: webproto.DBMaskedValue, M: true}}, {{V: "2"}, {N: true}, {V: webproto.DBMaskedValue, M: true}}},
				Next:    100, Masked: 2})
			p.WaitJS(`document.querySelectorAll(".db-grid tbody tr").length === 2`)
			if n := Eval[int](p, `document.querySelectorAll(".db-grid b").length`); n != 0 {
				t.Fatal("a cell became HTML")
			}
			if s := p.Text(".db-grid td.is-null"); s != "NULL" {
				t.Errorf("null cell %q", s)
			}
			if n := Eval[int](p, `document.querySelectorAll(".db-grid td.is-masked").length`); n != 2 {
				t.Errorf("%d masked cells", n)
			}
			p.TapJS(ButtonByText("Next rows"))
			reply(t, c, expectOp(t, c, webproto.OpDBRows, `{"conn":"local","table":"users","from":100}`), webproto.DBRows{Columns: []string{"id"}, Rows: [][]webproto.DBCell{{{V: "101"}}}, From: 100})
			p.WaitJS(`document.querySelectorAll(".db-grid tbody tr").length === 1`)
			p.TapJS(`document.querySelector(".dbv .sub-back")`)
			reply(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"local"}`), webproto.DBTables{Conn: "local", Tables: []webproto.DBTable{{Name: "users"}}})
			p.WaitJS(`document.querySelector(".db-sql")`)
			setInput(p, `document.querySelector(".db-sql")`, "SELECT count(*) FROM users")
			p.TapJS(ButtonByText("Run query"))
			p.WaitJS(`document.activeElement === document.querySelector(".row-ask button")`)
			p.Key(kb.Enter) // Cancel
			c.Quiet(t, 200*time.Millisecond)
			p.TapJS(ButtonByText("Run query"))
			p.TapJS(`[...document.querySelectorAll(".row-ask .btn")].find((b) => b.textContent === "Run query")`)
			reply(t, c, expectOp(t, c, webproto.OpDBQuery, `{"conn":"local","sql":"SELECT count(*) FROM users"}`), webproto.DBRows{Columns: []string{"count"}, Rows: [][]webproto.DBCell{{{V: "2"}}}, Ms: 3})
			p.WaitJS(`document.querySelector(".db-query .db-grid td")?.textContent === "2"`)

			setInput(p, `document.querySelector(".db-sql")`, "DELETE FROM users")
			p.TapJS(ButtonByText("Run query"))
			p.TapJS(`[...document.querySelectorAll(".row-ask .btn")].find((b) => b.textContent === "Run query")`)
			r := expectOp(t, c, webproto.OpDBQuery, `{"conn":"local","sql":"DELETE FROM users"}`)
			c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeForbidden, Msg: "This statement may change data. It needs the laptop."})
			p.WaitJS(`document.querySelector(".ask-text")?.textContent === "This may change data on local. Ask the laptop to run it?"`)
			p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
			p.TapJS(ButtonByText("Ask the laptop"))
			reply(t, c, expectOp(t, c, webproto.OpDBExec, `{"conn":"local","sql":"DELETE FROM users"}`), webproto.AskRef{Card: "card-21"})
			p.WaitJS(`document.querySelector(".waiting")`)
			p.noOverflow("databases")
		})
	}
}

// A viewer browses but gets no query box.
func TestDBViewer(t *testing.T) {
	m := Meta("c-v")
	m.Role = webproto.RoleViewer
	p, c := openDB(t, Phone, m)
	p.TapJS(`document.querySelector('.db-conn[data-conn="local"]')`)
	reply(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"local"}`), webproto.DBTables{Conn: "local", Tables: []webproto.DBTable{{Name: "users"}}})
	p.WaitJS(`document.querySelectorAll(".db-table").length === 1`)
	if n := Eval[int](p, `document.querySelectorAll(".db-sql, .db-query").length`); n != 0 {
		t.Fatal("a viewer gets a query box")
	}
}
