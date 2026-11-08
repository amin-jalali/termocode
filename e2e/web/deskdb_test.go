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

// The rest of the desktop's Databases view (web parity): the laptop
// session's state on a connection, a Redis connection's keys, row edits
// reviewed on the laptop, a table's DDL, plans, pinned and exported
// results, the connection menu and the add form. Every change asks the
// laptop; nothing changes before its yes.

const dbxSide = `document.querySelector('.wb-side [data-view="db"]')`

func dbxConns() webproto.DBConns {
	return webproto.DBConns{Conns: []webproto.DBConn{
		{ID: "local", Driver: "postgres", Database: "app", SQL: true, Connected: true, Manual: true, Tx: true, Writes: 2},
		{ID: "prod", Driver: "postgres", Database: "app", Env: "production", ReadOnly: true, SQL: true},
		{ID: "cache", Driver: "redis"},
	}}
}

// dbxAsk waits for the laptop question (Cancel first), then asks.
func dbxAsk(t *testing.T, p *Page) {
	t.Helper()
	p.WaitJS(`document.querySelector(".ask-text")?.textContent === "The laptop shows exactly what changes and asks you there." && document.activeElement?.textContent === "Cancel"`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
}

// dbxAnswer answers the page's laptop card.
func dbxAnswer(t *testing.T, c *HubConn, r *webproto.Req, card, choice string) {
	t.Helper()
	reply(t, c, r, webproto.AskRef{Card: card})
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: card, By: "laptop", Choice: choice})})
}

func TestDeskDBParity(t *testing.T) {
	p, c := deskLogin(t, nil)
	Eval[bool](p, `(location.hash = "#/more/db", true)`)
	reply(t, c, expectOp(t, c, webproto.OpDBConns, `{}`), dbxConns())
	replyLocalHosts(t, c)
	p.WaitJS(dbxSide + `?.querySelectorAll(".tr[data-conn]").length === 3`)
	if s := Eval[string](p, dbxSide+`.querySelector('.tr[data-conn="local"] .db-tx')?.textContent ?? ""`); s != "2 not saved" {
		t.Errorf("open transaction mark %q", s)
	}
	if !Eval[bool](p, `!!`+dbxSide+`.querySelector('.tr[data-conn="local"] .db-on')`) || Eval[bool](p, `!!`+dbxSide+`.querySelector('.tr[data-conn="prod"] .db-on')`) {
		t.Error("the connected dot")
	}

	// Redis: the keys (never a value); Delete Key asks the laptop.
	p.TapJS(dbxSide + `.querySelector('.tr[data-conn="cache"]')`)
	reply(t, c, expectOp(t, c, webproto.OpRedisKeys, `{"conn":"cache"}`), webproto.RedisKeys{Keys: []webproto.RedisKey{{Key: "session:ab12", Type: "hash", TTL: 3600000}, {Key: "counter", Type: "string", TTL: -1}}})
	p.WaitJS(dbxSide + `.querySelector('.tr[data-key="session:ab12"]')?.textContent.includes("3600s")`)
	Eval[bool](p, dbxSide+`.querySelector('.tr[data-key="counter"] [aria-label="Delete Key…"]').click() ?? true`)
	dbxAsk(t, p)
	dbxAnswer(t, c, expectOp(t, c, webproto.OpRedisKey, `{"conn":"cache","key":"counter","verb":"delete"}`), "k-1", "cancel")
	p.WaitJS(`!document.querySelector(".ask-text")`)

	// A table with a key: edit a cell, delete a row, submit (the laptop's card).
	p.TapJS(dbxSide + `.querySelector('.tr[data-conn="local"]')`)
	reply(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"local"}`), webproto.DBTables{Conn: "local", Schema: "public", Tables: []webproto.DBTable{{Name: "users"}}})
	p.TapJS(dbxSide + `.querySelector('.tr[data-table="users"]')`)
	rows := webproto.DBRows{Columns: []string{"id", "email"}, Rows: [][]webproto.DBCell{{{V: "1"}, {V: "ada"}}, {{V: "2"}, {V: "bob"}}}, Key: []string{"id"}}
	reply(t, c, expectOp(t, c, webproto.OpDBRows, `{"conn":"local","table":"users","from":0}`), rows)
	const tab = `document.querySelector('[data-view="db-table"]')`
	p.WaitJS(tab + `?.querySelectorAll(".dbg-table tbody tr").length === 2`)
	Eval[bool](p, `(`+tab+`.querySelector('tbody tr[data-row="0"] td:nth-child(3)').dispatchEvent(new MouseEvent("dblclick", {bubbles: true})), true)`)
	p.WaitJS(tab + `.querySelector(".dbg-in") === document.activeElement`)
	p.Do(chromedp.KeyEvent("new@x"))
	p.Key(kb.Enter)
	p.WaitJS(tab + `.querySelector("td.is-changed")?.textContent === "adanew@x" || ` + tab + `.querySelector("td.is-changed")?.textContent === "new@x"`)
	p.TapJS(tab + `.querySelector('tbody tr[data-row="1"] td.dbg-n')`)
	p.TapJS(`document.querySelector('.ed-actions [aria-label="Delete Row"]')`)
	p.WaitJS(tab + `.querySelector('tbody tr[data-row="1"].is-del') && ` + tab + `.querySelector(".dbv-ro.is-dirty")?.textContent === "2 not saved"`)
	p.TapJS(`document.querySelector('.ed-actions [data-act="submit"]')`)
	p.WaitJS(`document.querySelector(".ask-text")?.textContent === "Save 2 changes to users on local? The laptop shows each statement."`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	r := expectOp(t, c, webproto.OpDBEdit, "")
	args := string(r.Args)
	for _, want := range []string{`{"kind":"delete","key":[{"col":"id","v":"2"}]}`, `"set":[{"col":"email","v":`, `"old":[{"col":"email","v":"ada"}]`} {
		if !strings.Contains(args, want) {
			t.Errorf("db.edit args %s: no %s", args, want)
		}
	}
	dbxAnswer(t, c, r, "e-1", "confirm")
	reply(t, c, expectOp(t, c, webproto.OpDBRows, `{"conn":"local","table":"users","from":0}`), rows)
	p.WaitJS(`!` + tab + `.querySelector(".is-changed, .is-del")`)

	// Revert asks in the page (Cancel first) and drops the pending change.
	p.WaitJS(`!document.querySelector(".toast")`) // "Done on the laptop." covers the tab's top
	Eval[bool](p, `(`+tab+`.querySelector('tbody tr[data-row="0"] td:nth-child(3)').dispatchEvent(new MouseEvent("dblclick", {bubbles: true})), true)`)
	p.WaitJS(tab + `.querySelector(".dbg-in") === document.activeElement`)
	p.Do(chromedp.KeyEvent("zz"))
	p.Key(kb.Enter)
	p.WaitJS(tab + `.querySelector("td.is-changed")`)
	p.TapJS(`document.querySelector('.ed-actions [data-act="revert"]')`)
	p.WaitJS(tab + `.querySelector(".pv-ask")?.textContent.includes("Drop 1 changes that are not saved?") && document.activeElement?.textContent === "Cancel"`)
	p.TapJS(`[...` + tab + `.querySelectorAll(".pv-ask button")].find((b) => b.textContent === "Revert Changes")`)
	p.WaitJS(`!` + tab + `.querySelector(".is-changed")`)
	c.Quiet(t, 200*time.Millisecond)

	// Show DDL opens a tab with the laptop's text.
	Eval[bool](p, dbxSide+`.querySelector('.tr[data-table="users"] [aria-label="Show DDL"]').click() ?? true`)
	reply(t, c, expectOp(t, c, webproto.OpDBDDL, `{"conn":"local","table":"users"}`), webproto.DBText{Text: "CREATE TABLE users (id int);"})
	p.WaitJS(`document.querySelector('[data-view="db-ddl"] .editor-host') && document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent === "DDL: users"`)

	// A query: Explain shows the plan; Explain Analyze asks first; a
	// result pins and exports.
	Eval[bool](p, dbxSide+`.querySelector('.tr[data-conn="local"] [aria-label="New Query"]').click() ?? true`)
	const qdoc = `document.querySelector('[data-view="db-query"] .desk-editor')?.contentDocument`
	p.WaitJS(qdoc + `?.querySelector(".view-lines")`)
	Eval[bool](p, `(document.querySelector('[data-view="db-query"] .desk-editor').focus(), `+qdoc+`.querySelector(".native-edit-context, textarea").focus(), true)`)
	p.Do(chromedp.KeyEvent("SELECT 1"))
	p.WaitJS(`!document.querySelector('.ed-actions [data-act="explain"]')?.disabled`)
	p.TapJS(`document.querySelector('.ed-actions [data-act="explain"]')`)
	reply(t, c, expectOp(t, c, webproto.OpDBExplain, `{"conn":"local","sql":"SELECT 1"}`), webproto.DBPlan{Lines: []webproto.DBPlanLine{{Text: "Result  cost=0..0.01", Costly: true}, {Depth: 1, Text: "Output: 1", Detail: true}}})
	p.WaitJS(`document.querySelector('[data-view="db-query"] .dbp-line.is-costly')?.textContent.includes("Result") && document.querySelector('[data-view="db-query"] .dbp-line.is-detail')`)
	p.TapJS(`document.querySelector('.ed-actions [data-act="analyze"]')`)
	p.WaitJS(`document.querySelector('[data-view="db-query"] .pv-ask')?.textContent.includes("Explain Analyze runs this read on local") && document.activeElement?.textContent === "Cancel"`)
	p.TapJS(`[...document.querySelectorAll('[data-view="db-query"] .pv-ask button')].find((b) => b.textContent === "Explain Analyze")`)
	reply(t, c, expectOp(t, c, webproto.OpDBExplain, `{"conn":"local","sql":"SELECT 1","analyze":true}`), webproto.DBPlan{Analyze: true, Lines: []webproto.DBPlanLine{{Text: "Result (actual rows=1)"}}})
	p.WaitJS(`document.querySelector('[data-view="db-query"] .dbp-line')?.textContent.includes("actual rows=1")`)
	p.TapJS(`document.querySelector('.ed-actions [data-act="run-query"]')`)
	p.TapJS(`[...document.querySelectorAll('[data-view="db-query"] .pv-ask button')].find((b) => b.textContent === "Run query")`)
	reply(t, c, expectOp(t, c, webproto.OpDBQuery, `{"conn":"local","sql":"SELECT 1"}`), webproto.DBRows{Columns: []string{"?column?"}, Rows: [][]webproto.DBCell{{{V: "=1+1"}}}})
	p.WaitJS(`document.querySelector('[data-view="db-query"] .dbg-table tbody td:nth-child(2)')?.textContent === "=1+1"`)
	p.TapJS(`document.querySelector('.ed-actions [data-act="pin"]')`)
	p.WaitJS(`document.querySelector('[data-view="db-query"] .dbq-pin')?.textContent.includes("Pinned 1")`)
	p.TapJS(`document.querySelector('.ed-actions [data-act="export"]')`)
	p.WaitJS(`[...document.querySelectorAll(".ctx-item")].map((b) => b.textContent).join() === "CSV,JSON,Markdown"`)
	p.Key(kb.Escape)

	// The connection menu: Commit asks the laptop.
	Eval[bool](p, dbxSide+`.querySelector('.tr[data-conn="local"] [aria-label="More Actions…"]').click() ?? true`)
	p.TapJS(`[...document.querySelectorAll(".ctx-item")].find((b) => b.textContent === "Commit…")`)
	dbxAsk(t, p)
	dbxAnswer(t, c, expectOp(t, c, webproto.OpDBTx, `{"conn":"local","verb":"commit"}`), "tx-1", "cancel")
	p.WaitJS(`!document.querySelector(".ask-text")`)

	// Add Connection: a form with no password field; the laptop saves it.
	p.TapJS(dbxSide + `.querySelector('.pv-title [aria-label="Add Connection…"]')`)
	p.WaitJS(dbxSide + `.querySelector("form.dbf #dbf-host")`)
	if Eval[bool](p, `!!`+dbxSide+`.querySelector('form.dbf input[type="password"], form.dbf #dbf-password')`) {
		t.Error("the add form has a password field")
	}
	for _, f := range [][2]string{{"id", "app2"}, {"host", "db2.internal"}, {"user", "app"}} {
		Eval[bool](p, `(document.querySelector("#dbf-`+f[0]+`").focus(), true)`)
		p.WaitJS(`document.activeElement?.id === "dbf-` + f[0] + `"`)
		p.Do(chromedp.KeyEvent(f[1]))
		p.WaitJS(`document.querySelector("#dbf-` + f[0] + `").value === ` + jsq(f[1]))
	}
	p.TapJS(dbxSide + `.querySelector('form.dbf [type="submit"]')`)
	dbxAsk(t, p)
	dbxAnswer(t, c, expectOp(t, c, webproto.OpDBAdd, `{"id":"app2","driver":"postgres","host":"db2.internal","user":"app"}`), "add-1", "cancel")
}
