//go:build browser

package web

import (
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Opening a database from the page: a failure says why in one line,
// never "Something failed inside termocode".

// dbOpenView opens the desktop's Databases view with dbConns().
func dbOpenView(t *testing.T) (*Page, *HubConn) {
	t.Helper()
	p, c := deskLogin(t, nil)
	Eval[bool](p, `(location.hash = "#/more/db", true)`)
	reply(t, c, expectOp(t, c, webproto.OpDBConns, `{}`), dbConns())
	p.WaitJS(dbxSide + `?.querySelectorAll(".tr[data-conn]").length === 3`)
	return p, c
}

// dbFail answers r with a conflict that says why (data: what is needed).
func dbFail(t *testing.T, c *HubConn, r *webproto.Req, msg string, data any) {
	t.Helper()
	e := &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeConflict, Msg: msg}
	if data != nil {
		e.Data = raw(data)
	}
	c.Send(t, e)
}

func TestDeskDBFailureSaysWhy(t *testing.T) {
	p, c := dbOpenView(t)
	p.TapJS(dbxSide + `.querySelector('.tr[data-conn="local"]')`)
	dbFail(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"local"}`), "connection refused", nil)
	p.WaitJS(dbxSide + `.querySelector('.tr-note.is-err')?.textContent === "connection refused"`)

	// Test Connection says the same reason in its toast.
	Eval[bool](p, dbxSide+`.querySelector('.tr[data-conn="local"] [aria-label="More Actions…"]').click() ?? true`)
	p.TapJS(`[...document.querySelectorAll(".ctx-item")].find((b) => b.textContent === "Test Connection")`)
	dbFail(t, c, expectOp(t, c, webproto.OpDBTest, `{"conn":"local"}`), "password authentication failed", nil)
	p.WaitJS(`[...document.querySelectorAll(".toast")].some((e) => e.textContent.includes("password authentication failed"))`)
	if Eval[bool](p, `document.body.textContent.includes("Something failed inside termocode")`) {
		t.Error("the generic internal text is shown")
	}
}

// An unknown SSH host: the page asks with the host and its key (Cancel
// first and focused); Trust and save asks the laptop to save the key,
// then the tables load. Cancel sends nothing and keeps the reason.
func TestDeskDBTrustsSSHHost(t *testing.T) {
	p, c := dbOpenView(t)
	const ask = dbxSide + `.querySelector('.db-need')`
	need := map[string]any{"need": "host_key", "host": "bastion.example.com", "fingerprint": "SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8", "key_type": "ED25519"}

	p.TapJS(dbxSide + `.querySelector('.tr[data-conn="prod"]')`)
	dbFail(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"prod"}`), "Unknown SSH host bastion.example.com.", need)
	p.WaitJS(ask + `?.textContent.includes("bastion.example.com") && ` + ask + `.textContent.includes("SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8") && ` + ask + `.textContent.includes("ED25519")`)
	if got := Eval[string](p, `[...`+ask+`.querySelectorAll("button")].map((b) => b.textContent).join("|")`); got != "Cancel|Trust and save" {
		t.Errorf("buttons %q, want Cancel first", got)
	}
	p.WaitJS(`document.activeElement?.textContent === "Cancel"`)
	p.TapJS(`[...` + ask + `.querySelectorAll("button")].find((b) => b.textContent === "Cancel")`)
	p.WaitJS(`!` + ask + ` && ` + dbxSide + `.querySelector('.tr-note.is-err')?.textContent === "Unknown SSH host bastion.example.com."`)
	c.Quiet(t, 200*time.Millisecond)

	p.TapJS(dbxSide + `.querySelector('.tr[data-conn="local"]')`)
	dbFail(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"local"}`), "Unknown SSH jump host gw.example.net.", map[string]any{"need": "host_key", "host": "gw.example.net", "fingerprint": "SHA256:gw", "key_type": "ED25519", "jump": true})
	p.WaitJS(ask + `?.textContent.includes("jump host gw.example.net")`)
	p.TapJS(`[...` + ask + `.querySelectorAll("button")].find((b) => b.textContent === "Trust and save")`)
	reply(t, c, expectOp(t, c, "db.trust_host", `{"conn":"local","fingerprint":"SHA256:gw"}`), webproto.DBDone{Text: "Host key saved."})
	reply(t, c, expectOp(t, c, webproto.OpDBTables, `{"conn":"local"}`), webproto.DBTables{Conn: "local", Schema: "public", Tables: []webproto.DBTable{{Name: "users"}}})
	p.WaitJS(dbxSide + `.querySelector('.tr[data-table="users"]') && !` + ask)
}
