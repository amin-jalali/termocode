//go:build browser

package web

import (
	"testing"

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
