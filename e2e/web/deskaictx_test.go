//go:build browser

package web

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Add to AI Chat (desktop web): any database connection, table,
// column, row, rows or cell, any host, compose project or container
// goes to the AI chat as context without typing. Each one waits as a
// removable chip above the chat input ("local › users › email") and
// goes with the next message as an item attach: its exact address in
// the names termocode's MCP tools take, and a small summary (a table's
// DDL, the picked rows).

const aiChips = `[...document.querySelectorAll('[data-view="chat"] .composer-attach .chat-chip > span')].map((e) => e.textContent)`

// aiOpenChat logs in on a desktop with the laptop's chat open.
func aiOpenChat(t *testing.T) (*Page, *HubConn) {
	t.Helper()
	p, c := deskLogin(t, nil)
	c.Send(t, Snap("chat", 1, chatWith(false, userMsg(0, "hi"))))
	p.WaitJS(chatView + `?.querySelector(".composer textarea")`)
	return p, c
}

// aiAdd right-clicks el and picks Add to AI Chat.
func aiAdd(p *Page, el string) {
	p.T.Helper()
	rightClick(p, el)
	menuPick(p, "Add to AI Chat")
}

// aiWaitChips waits for the chips to read want.
func aiWaitChips(t *testing.T, p *Page, want ...string) {
	t.Helper()
	b, _ := json.Marshal(want)
	for end := time.Now().Add(wait); !Eval[bool](p, `JSON.stringify(`+aiChips+`) === `+jsq(string(b))); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("chips %q, want %q", Eval[[]string](p, aiChips), want)
		}
	}
}

// aiSend sends text and returns the item specs of chat.send by label.
func aiSend(t *testing.T, p *Page, c *HubConn, text string) map[string]webproto.ItemSpec {
	t.Helper()
	p.TapJS(composer + `.querySelector("textarea")`)
	p.Key(text)
	p.Key(kb.Enter)
	r := expectReq(t, c, "chat.send")
	var a webproto.ChatSendArgs
	if err := json.Unmarshal(r.Args, &a); err != nil || a.Text != text {
		t.Fatalf("chat.send args %s", r.Args)
	}
	out := map[string]webproto.ItemSpec{}
	for _, at := range a.Attach {
		if at.Kind != webproto.AttachItem {
			t.Fatalf("attach %+v is not an item", at)
		}
		s, err := webproto.ParseItemSpec(at.Spec)
		if err != nil {
			t.Fatalf("attach %s: %v", at.Spec, err)
		}
		out[at.Label] = s
	}
	reply(t, c, r, map[string]any{"id": "c-1"})
	p.WaitJS(`!document.querySelector('[data-view="chat"] .composer-attach')`)
	return out
}

func aiStr(s string) *string { return &s }

func TestDeskAddToAIChatDatabase(t *testing.T) {
	p, c := aiOpenChat(t)
	Eval[bool](p, `(location.hash = "#/more/db", true)`)
	reply(t, c, expectOp(t, c, webproto.OpDBConns, `{}`), dbConns())
	replyLocalHosts(t, c)
	p.WaitJS(dbxSide + `?.querySelectorAll(".tr[data-conn]").length === 3`)

	// A connection and a table of the tree; a table brings its DDL.
	aiAdd(p, dbxSide+`.querySelector('.tr[data-conn="local"]')`)
	aiWaitChips(t, p, "local")
	p.TapJS(dbxSide + `.querySelector('.tr[data-conn="local"]')`)
	reply(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"local"}`), webproto.DBTables{Conn: "local", Schema: "public", Tables: []webproto.DBTable{{Name: "users"}}})
	aiAdd(p, dbxSide+`.querySelector('.tr[data-table="users"]')`)
	reply(t, c, expectOp(t, c, webproto.OpDBDDL, `{"conn":"local","table":"users"}`), webproto.DBText{Text: "CREATE TABLE users (id int, email text);"})
	aiWaitChips(t, p, "local", "local › users")

	// The grid: a column (its header), a cell, a row (its number) and the
	// shown rows (the corner).
	p.TapJS(dbxSide + `.querySelector('.tr[data-table="users"]')`)
	rows := webproto.DBRows{Columns: []string{"id", "email"}, Rows: [][]webproto.DBCell{{{V: "1"}, {V: "ada@x"}}, {{V: "2"}, {N: true}}}, Key: []string{"id"}}
	reply(t, c, expectOp(t, c, webproto.OpDBRows, `{"conn":"local","table":"users","from":0}`), rows)
	const grid = `document.querySelector('[data-view="db-table"] .dbg-table')`
	p.WaitJS(grid + `?.querySelectorAll("tbody tr").length === 2`)
	aiAdd(p, grid+`.querySelector("thead th:nth-child(3)")`)
	aiAdd(p, grid+`.querySelector('tbody tr[data-row="0"] td:nth-child(3)')`)
	aiAdd(p, grid+`.querySelector('tbody tr[data-row="1"] td.dbg-n')`)
	aiAdd(p, grid+`.querySelector("thead th.dbg-n")`)
	aiWaitChips(t, p, "local", "local › users", "local › users › email", "local › users › row 1 › email", "local › users › row 2", "local › users › 2 rows")

	// The same item twice stays one chip; a chip's × takes it out.
	aiAdd(p, dbxSide+`.querySelector('.tr[data-conn="local"]')`)
	aiWaitChips(t, p, "local", "local › users", "local › users › email", "local › users › row 1 › email", "local › users › row 2", "local › users › 2 rows")
	p.TapJS(`[...document.querySelectorAll('[data-view="chat"] .composer-attach .chat-chip')].find((e) => e.textContent.startsWith("local › users › row 2")).querySelector(".chip-x")`)
	aiWaitChips(t, p, "local", "local › users", "local › users › email", "local › users › row 1 › email", "local › users › 2 rows")

	got := aiSend(t, p, c, "why is email empty?")
	ddl := "CREATE TABLE users (id int, email text);"
	want := map[string]webproto.ItemSpec{
		"local":                         {Item: "db_conn", Conn: "local"},
		"local › users":                 {Item: "db_table", Conn: "local", Schema: "public", Table: "users", DDL: ddl},
		"local › users › email":         {Item: "db_column", Conn: "local", Schema: "public", Table: "users", Column: "email"},
		"local › users › row 1 › email": {Item: "db_cell", Conn: "local", Schema: "public", Table: "users", Column: "email", Key: []string{"id"}, Columns: []string{"id", "email"}, Rows: [][]*string{{aiStr("1"), aiStr("ada@x")}}},
		"local › users › 2 rows":        {Item: "db_rows", Conn: "local", Schema: "public", Table: "users", Key: []string{"id"}, Columns: []string{"id", "email"}, Rows: [][]*string{{aiStr("1"), aiStr("ada@x")}, {aiStr("2"), nil}}},
	}
	if !reflect.DeepEqual(got, want) {
		gb, _ := json.MarshalIndent(got, "", " ")
		wb, _ := json.MarshalIndent(want, "", " ")
		t.Fatalf("items sent\n%s\nwant\n%s", gb, wb)
	}
}

func TestDeskAddToAIChatContainers(t *testing.T) {
	p, c := aiOpenChat(t)
	Eval[bool](p, `(location.hash = "#/more/containers", true)`)
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	reply(t, c, expectOp(t, c, webproto.OpContainersHost, `{}`), hostsCtrHost())
	p.WaitJS(deskCtr + `?.querySelectorAll('[data-section="containers"] .tr[data-id]').length === 2`)

	// The host in use, its compose project and a container.
	aiAdd(p, deskCtr+`.querySelector('.tr[data-context="default"]')`)
	aiAdd(p, deskCtr+`.querySelector('.tr[data-project="shop"]')`)
	aiAdd(p, deskCtr+`.querySelector('.tr[data-id="9f3c2e1a1234"]')`)
	// Another host, read when it opens: the host, a project, a container.
	p.TapJS(deskCtr + `.querySelector('.tr[data-context="baaytona"]')`)
	reply(t, c, expectOp(t, c, webproto.OpContainersServer, `{"name":"baaytona"}`), baaytonaCtrs())
	aiAdd(p, deskCtr+`.querySelector('.tr[data-context="baaytona"]')`)
	aiAdd(p, deskCtr+`.querySelector('.tr[data-host-project="baaytona/baaytona"]')`)
	aiAdd(p, deskCtr+`.querySelector('.tr[data-host-ctr="c0ffee01aa00"]')`)
	aiWaitChips(t, p, "default", "default › shop", "default › shop › api-1", "baaytona", "baaytona › baaytona", "baaytona › baaytona › baaytona-db-1")
	if !strings.Contains(menuLabelsAfter(p, deskCtr+`.querySelector('.tr[data-id="9f3c2e1a1234"]')`), "Logs") {
		t.Error("a container's own menu items are gone")
	}

	got := aiSend(t, p, c, "why does it restart?")
	want := map[string]webproto.ItemSpec{
		"default":                {Item: "host", Host: "default"},
		"default › shop":         {Item: "compose_project", Host: "default", Project: "shop"},
		"default › shop › api-1": {Item: "container", Host: "default", Project: "shop", Container: "api-1"},
		"baaytona":               {Item: "host", Host: "baaytona"},
		"baaytona › baaytona":    {Item: "compose_project", Host: "baaytona", Project: "baaytona"},
		"baaytona › baaytona › baaytona-db-1": {Item: "container", Host: "baaytona", Project: "baaytona", Container: "baaytona-db-1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("items sent %+v\nwant %+v", got, want)
	}
}

// menuLabelsAfter right-clicks el, reads its menu and closes it.
func menuLabelsAfter(p *Page, el string) string {
	p.T.Helper()
	rightClick(p, el)
	s := menuLabels(p)
	p.Key(kb.Escape)
	p.WaitJS(`!document.querySelector(".ctx-menu")`)
	return s
}

// aiOpenUsers opens the users table of local in a grid (its rows: id,
// email, password_hash, api_key).
func aiOpenUsers(t *testing.T, p *Page, c *HubConn) string {
	t.Helper()
	Eval[bool](p, `(location.hash = "#/more/db", true)`)
	reply(t, c, expectOp(t, c, webproto.OpDBConns, `{}`), dbConns())
	replyLocalHosts(t, c)
	p.WaitJS(dbxSide + `?.querySelectorAll(".tr[data-conn]").length === 3`)
	p.TapJS(dbxSide + `.querySelector('.tr[data-conn="local"]')`)
	reply(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"local"}`), webproto.DBTables{Conn: "local", Tables: []webproto.DBTable{{Name: "users"}}})
	p.TapJS(dbxSide + `.querySelector('.tr[data-table="users"]')`)
	rows := webproto.DBRows{Columns: []string{"id", "email", "Password_Hash", "api_key"}, Rows: [][]webproto.DBCell{
		{{V: "1"}, {V: "ada@x"}, {V: "$2b$10$abc"}, {V: "k-123"}},
		{{V: "2"}, {V: "bob@x"}, {V: "$2b$10$def"}, {N: true}},
	}, Key: []string{"id"}}
	reply(t, c, expectOp(t, c, webproto.OpDBRows, `{"conn":"local","table":"users","from":0}`), rows)
	const grid = `document.querySelector('[data-view="db-table"] .dbg-table')`
	p.WaitJS(grid + `?.querySelectorAll("tbody tr").length === 2`)
	return grid
}

const aiSecret = `document.querySelector(".ai-secret")`

// aiSecretAsk sends text and waits for the warning card: it names the
// secret-looking columns, its buttons are Cancel (focused), Mask and
// send, Send all, and nothing is sent yet.
func aiSecretAsk(t *testing.T, p *Page, c *HubConn, text, cols string) {
	t.Helper()
	p.TapJS(composer + `.querySelector("textarea")`)
	p.Key(text)
	p.Key(kb.Enter)
	p.WaitJS(aiSecret + ` && document.activeElement?.textContent === "Cancel"`)
	if got := Eval[string](p, aiSecret+`.querySelector(".ai-secret-cols").textContent`); got != cols {
		t.Errorf("the card names %q, want %q", got, cols)
	}
	if got := Eval[string](p, `[...`+aiSecret+`.querySelectorAll("button")].map((b) => b.textContent).join("|")`); got != "Cancel|Mask and send|Send all" {
		t.Errorf("the card's buttons %q", got)
	}
	c.Quiet(t, 200*time.Millisecond)
}

// Rows with columns that look secret (password, hash, token, api key, …
// by name, any case) never go to the AI unasked: before the send, a card
// in the page names those columns. Cancel (first, focused) sends
// nothing; Mask and send puts ••• in their place; Send all sends them.
// No laptop step. A cell of such a row asks too; rows without one never
// ask.
func TestDeskAddToAIChatSecretColumns(t *testing.T) {
	p, c := aiOpenChat(t)
	grid := aiOpenUsers(t, p, c)
	aiAdd(p, grid+`.querySelector("thead th.dbg-n")`)
	aiWaitChips(t, p, "local › users › 2 rows")

	// Cancel: nothing is sent, the draft and the chip stay.
	aiSecretAsk(t, p, c, "who signed up?", "Password_Hash, api_key")
	p.TapJS(`[...` + aiSecret + `.querySelectorAll("button")].find((b) => b.textContent === "Cancel")`)
	p.WaitJS(`!` + aiSecret)
	c.Quiet(t, 200*time.Millisecond)
	aiWaitChips(t, p, "local › users › 2 rows")
	if got := Eval[string](p, composer+`.querySelector("textarea").value`); got != "who signed up?" {
		t.Fatalf("after Cancel the draft is %q", got)
	}

	// Mask and send: those values are •••, the rest goes as it is.
	Eval[bool](p, `(`+composer+`.querySelector("textarea").value = "", `+composer+`.querySelector("textarea").dispatchEvent(new Event("input", {bubbles: true})), true)`)
	aiSecretAsk(t, p, c, "who signed up?", "Password_Hash, api_key")
	p.TapJS(`[...` + aiSecret + `.querySelectorAll("button")].find((b) => b.textContent === "Mask and send")`)
	r := expectReq(t, c, "chat.send")
	if strings.Contains(string(r.Args), "$2b$10$abc") || strings.Contains(string(r.Args), "k-123") {
		t.Fatalf("a masked value was sent: %s", r.Args)
	}
	s := aiItemOf(t, r, "local › users › 2 rows")
	if want := [][]*string{{aiStr("1"), aiStr("ada@x"), aiStr("•••"), aiStr("•••")}, {aiStr("2"), aiStr("bob@x"), aiStr("•••"), nil}}; !reflect.DeepEqual(s.Rows, want) || strings.Join(s.Masked, ",") != "Password_Hash,api_key" {
		t.Fatalf("masked rows %+v masked %v", s.Rows, s.Masked)
	}
	reply(t, c, r, map[string]any{"id": "c-1"})
	p.WaitJS(`!` + aiSecret + ` && !document.querySelector('[data-view="chat"] .composer-attach')`)

	// Send all: the values go as they are.
	aiAdd(p, grid+`.querySelector('tbody tr[data-row="0"] td:nth-child(3)')`)
	aiWaitChips(t, p, "local › users › row 1 › email")
	aiSecretAsk(t, p, c, "is this one real?", "Password_Hash, api_key")
	p.TapJS(`[...` + aiSecret + `.querySelectorAll("button")].find((b) => b.textContent === "Send all")`)
	r = expectReq(t, c, "chat.send")
	s = aiItemOf(t, r, "local › users › row 1 › email")
	if want := [][]*string{{aiStr("1"), aiStr("ada@x"), aiStr("$2b$10$abc"), aiStr("k-123")}}; !reflect.DeepEqual(s.Rows, want) || len(s.Masked) != 0 {
		t.Fatalf("rows sent %+v masked %v", s.Rows, s.Masked)
	}
	reply(t, c, r, map[string]any{"id": "c-1"})
	p.WaitJS(`!document.querySelector('[data-view="chat"] .composer-attach')`)

	// A column item carries no values: it never asks.
	aiAdd(p, grid+`.querySelector("thead th:nth-child(4)")`)
	got := aiSend(t, p, c, "what is this column?")
	if got["local › users › Password_Hash"].Column != "Password_Hash" {
		t.Fatalf("sent %+v", got)
	}
}

// aiItemOf is the spec of r's item attach labelled label.
func aiItemOf(t *testing.T, r *webproto.Req, label string) webproto.ItemSpec {
	t.Helper()
	var a webproto.ChatSendArgs
	if err := json.Unmarshal(r.Args, &a); err != nil {
		t.Fatal(err)
	}
	for _, at := range a.Attach {
		if at.Label == label {
			s, err := webproto.ParseItemSpec(at.Spec)
			if err != nil {
				t.Fatal(err)
			}
			return s
		}
	}
	t.Fatalf("no item %q in %s", label, r.Args)
	return webproto.ItemSpec{}
}
