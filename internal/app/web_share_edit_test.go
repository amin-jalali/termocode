package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Edit rights for a share (G-COL). A share is a viewer by default; the
// laptop's card for the person who opened the link may also let them
// edit files (a risky choice, never the primary one, and only there:
// not on a web page, not for a plain pairing). The hub enforces it.

// Spec (G-COL): the share card offers "Let them edit files" next to
// "Let them watch" (still the primary, Not now still first); picking it
// answers the edit role.
func TestWebShareCardOffersEdit(t *testing.T) {
	r := newAccessRig(t)
	r.ctl.send(pairAskLine(1, true))
	r.pumpUntil("the share card", func() bool { return r.m.confirmOpen && r.m.confirmKind == confirmKindWebPairAsk })
	var edit, primary string
	for _, b := range r.m.confirm.Buttons() {
		if b.ID == "edit" {
			edit = b.Title
		}
		if b.Style == confirm.StylePrimary {
			primary = b.ID
		}
	}
	if edit == "" || primary != "viewer" || r.m.confirm.SafeID() != "not-now" {
		t.Fatalf("buttons %+v", r.m.confirm.Buttons())
	}
	if view := ansiStrip(r.m.confirm.View()); !strings.Contains(view, "edit") {
		t.Errorf("the card does not say what editing means:\n%s", view)
	}
	webAccessSelect(r.m, "edit")
	if d, _ := r.nextMsg(r.ctl)["data"].(map[string]any); d["role"] != webdevices.RoleEdit {
		t.Fatalf("share role %v", d)
	}
}

// Spec (G-COL): a plain pairing card has no edit choice, and an edit
// answer for it is Not now.
func TestWebPairCardHasNoEdit(t *testing.T) {
	r := newAccessRig(t)
	r.ctl.send(pairAskLine(1, false))
	r.pumpUntil("the pair card", func() bool { return r.m.confirmOpen && r.m.confirmKind == confirmKindWebPairAsk })
	for _, b := range r.m.confirm.Buttons() {
		if b.ID == "edit" {
			t.Fatal("a plain pairing offers edit")
		}
	}
	webAccessSelect(r.m, "edit")
	if res := r.nextMsg(r.ctl); res["t"] != "err" || res["code"] != "forbidden" {
		t.Fatalf("edit for a plain pairing: %v", res)
	}
}

// Spec (G-COL): only the laptop's card gives edit rights: the same
// answer from a web page is Not now.
func TestWebPageMayNotGiveEdit(t *testing.T) {
	r := newAccessRig(t)
	r.ctl.send(pairAskLine(1, true))
	r.pumpUntil("the share card", func() bool { return r.m.confirmOpen && r.m.confirmKind == confirmKindWebPairAsk })
	r.m.webPairDecide(webdevices.RoleEdit, "Laptop browser")
	if res := r.nextMsg(r.ctl); res["t"] != "err" || res["code"] != "forbidden" {
		t.Fatalf("edit from a page: %v", res)
	}
}

// Spec (G-COL): the hub enforces the role: a viewer's set_text is
// refused; an editor's goes through, but it may not type in a terminal
// or commit.
func TestEditorRoleAtTheHub(t *testing.T) {
	h := newWebHub()
	t.Cleanup(h.stop)
	v := webPolicyConn(h, "v-1", webproto.RoleViewer)
	e := webPolicyConn(h, "e-1", webproto.RoleEditor)
	set := json.RawMessage(`{"buf":3,"tick":1,"start":{"row":0,"col":0},"end":{"row":0,"col":0},"lines":["x"]}`)
	if got := webPolicyReq(t, v, webproto.OpBufSetText, set); got != "forbidden" {
		t.Fatalf("viewer set_text: %s", got)
	}
	if got := webPolicyReq(t, e, webproto.OpBufSetText, set); got != "forwarded" {
		t.Fatalf("editor set_text: %s", got)
	}
	if got := webPolicyReq(t, e, webproto.OpTermSend, json.RawMessage(`{"buf":3,"text":"ls\r"}`)); got != "forbidden" {
		t.Fatalf("editor term.send: %s", got)
	}
	if got := webPolicyReq(t, e, webproto.OpGitCommit, json.RawMessage(`{"message":"x"}`)); got != "forbidden" {
		t.Fatalf("editor git.commit: %s", got)
	}
}
