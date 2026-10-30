package app

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (web design 3.2, 3.9): term.close waits for the laptop, like
// git.discard. The phone gets a card id at once and the shell keeps
// running; the laptop shows a Risky card (Cancel first) naming the
// terminal and the device; the web cannot confirm it. Confirmed on the
// laptop, the shell stops and its tab goes; the other tab stays. The Web
// channel keeps one line. An unlisted shell is not_found, a viewer is
// forbidden.
func TestTermCloseNeedsLaptopCard(t *testing.T) {
	r := newWebFilesRig(t)
	r.m.output = newOutputStore()
	r.withNvim()
	keep := webTestShell(t, r, "sh")
	gone := webTestShell(t, r, "sh")
	r.c.device = "phone"

	v := r.conn("v-1", webproto.RoleViewer)
	v.request(&webproto.Req{T: webproto.KindReq, ID: 1, Op: webproto.OpTermClose, Args: json.RawMessage(`{"buf":` + strconv.Itoa(gone) + `}`)})
	if fs := r.takeOn(v); len(fs) != 1 || webErrCode(fs[0]) != "forbidden" {
		t.Fatalf("viewer: %v", fs)
	}
	if f := r.call(webproto.OpTermClose, &webproto.BufArgs{Buf: 99999}); webErrCode(f) != "not_found" {
		t.Fatalf("unlisted shell: %v", f)
	}

	var ref webproto.AskRef
	webResData(t, r.call(webproto.OpTermClose, &webproto.BufArgs{Buf: gone}), &ref)
	if ref.Card == "" {
		t.Fatal("no card id")
	}
	if len(r.m.terminalTabs) != 2 {
		t.Fatal("closed before the laptop said so")
	}
	if !r.m.confirmOpen || r.m.confirmKind != confirmKindWebAsk || r.m.confirm.SafeID() != webproto.ChoiceCancel {
		t.Fatalf("laptop dialog open=%v kind=%v safe=%q", r.m.confirmOpen, r.m.confirmKind, r.m.confirm.SafeID())
	}
	if b := r.m.confirm.Buttons(); len(b) != 2 || b[0].ID != webproto.ChoiceCancel || b[1].Title != "Close" {
		t.Fatalf("buttons %+v", b)
	}
	if v := r.m.confirm.View(); !strings.Contains(v, "terminal 2") || !strings.Contains(v, "phone") {
		t.Errorf("the laptop card does not name the terminal and the device:\n%s", v)
	}
	if f := r.call(webproto.OpCardsAnswer, &webproto.CardsAnswerArgs{ID: ref.Card, Choice: webproto.ChoiceConfirm}); webErrCode(f) != "forbidden" {
		t.Fatalf("the web confirmed a laptop card: %v", f)
	}

	webTestLaptopSelect(t, r.m, webproto.ChoiceConfirm)
	if len(r.m.terminalTabs) != 1 || r.m.terminalTabs[0].BufID != keep {
		t.Fatalf("tabs after the laptop's yes: %+v", r.m.terminalTabs)
	}
	var valid bool
	r.lua(`return vim.api.nvim_buf_is_valid(...)`, &valid, gone)
	if valid {
		t.Error("the closed shell's buffer is still there")
	}
	if a := webTestAudit(r.m); !strings.Contains(a, "term.close  terminal 2  by=web:phone  → confirmed on the laptop") {
		t.Errorf("audit:\n%s", a)
	}
}
