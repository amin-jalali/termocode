//go:build unix

package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// Spec (owner 2026-11-08): a question the laptop's own flow asks after
// a page's action (Sleep's "Keep running" card, the steps of a worktree
// Archive or Delete, ...) asks in that page too, through the same path:
// the page gets an ask ev with the dialog's text, Cancel (the dialog's
// safe answer) first, and its yes presses the dialog's main button on
// the laptop. A trust button is never offered. When the laptop answers
// first, the page hears answered by the laptop. A dialog the laptop
// opens with no page action before it stays on the laptop.
func TestWebMirrorsTheLaptopsFollowUpDialog(t *testing.T) {
	if len(webTUIDialogOps) != 2 || !webTUIDialogOps[webproto.OpWorktreesArchive] || !webTUIDialogOps[webproto.OpWorktreesDelete] {
		t.Errorf("ops that ask with the TUI's own dialog: %v", webTUIDialogOps)
	}
	// Never a web card (pairing, control, the start card, devices, TOTP,
	// a laptop card), an AI card or a trust: those stay on the laptop.
	for _, k := range []confirmKindEnum{confirmKindWebAsk, confirmKindWebStart, confirmKindWebPairAsk, confirmKindWebPairQR,
		confirmKindWebGrant, confirmKindWebDevice, confirmKindWebDeviceRm, confirmKindWebForward, confirmKindWebUseLAN,
		confirmKindWebTotpReplace, confirmKindWebTotpShow, confirmKindAITool, confirmKindAIPlan, confirmKindMCPServe,
		confirmKindKeyCard, confirmKindWTRunTrust} {
		if webMirrorKinds[k] {
			t.Errorf("dialog kind %d may be answered from a page", k)
		}
	}
	m := webAITopicModel(t)
	m.output = newOutputStore()
	h := newWebHub()
	t.Cleanup(h.stop)
	h.approveOnLaptop = false
	m.web = h
	webTestWT(t, m)
	r := &webFilesRig{t: t, m: m, h: h, kept: map[*webConn][]map[string]any{}}
	r.c = r.conn("c-1", webproto.RoleController)
	r.c.device = "phone"
	other := r.conn("c-2", webproto.RoleController)

	const sidebar = "/src/app.worktrees/wt-sidebar"
	m.terminalTabs = []terminalTab{{BufID: 5, Agent: "claude", Cwd: sidebar, AgentState: agents.Working}}
	stopped := wtStopSeam(t)
	r.call(webproto.OpWorktreesSleep, &webproto.WorktreeOnArgs{ID: webWorktreeID(sidebar), On: true})
	webDrain(t, m, m.webAfterUpdate(nil))
	if !m.confirmOpen || m.confirmKind != confirmKindWTSleep {
		t.Fatal("the laptop's sleep card did not open")
	}
	asks, _ := webTestFrames(t, r, r.c)
	if len(asks) != 1 || !strings.HasPrefix(asks[0].Title, "Put ") || asks[0].Action != "Sleep" || !strings.Contains(asks[0].Body, "agent") {
		t.Fatalf("the page was not asked the laptop's question: %+v", asks)
	}
	if a, _ := webTestFrames(t, r, other); len(a) != 0 {
		t.Fatalf("another page was asked: %+v", a)
	}
	f := r.call(webproto.OpCardsAnswer, &webproto.CardsAnswerArgs{ID: asks[0].ID, Choice: webproto.ChoiceConfirm})
	if f["t"] != "res" {
		t.Fatalf("answer: %v", f)
	}
	webDrain(t, m, m.webAfterUpdate(nil))
	if m.confirmOpen || len(*stopped) != 1 {
		t.Fatalf("the page's yes did not answer the laptop's card: open %v stopped %v", m.confirmOpen, *stopped)
	}
	all, _ := worktrees.Load(m.wt.common)
	if all[sidebar].State != worktrees.StateAsleep {
		t.Fatalf("not asleep: %+v", all[sidebar])
	}

	// The laptop answers first: the page hears it.
	r.call(webproto.OpWorktreesSleep, &webproto.WorktreeOnArgs{ID: webWorktreeID(sidebar), On: false}) // wake
	m.terminalTabs = []terminalTab{{BufID: 6, Agent: "claude", Cwd: sidebar, AgentState: agents.Working}}
	r.call(webproto.OpWorktreesSleep, &webproto.WorktreeOnArgs{ID: webWorktreeID(sidebar), On: true})
	webDrain(t, m, m.webAfterUpdate(nil))
	asks, _ = webTestFrames(t, r, r.c)
	if len(asks) != 1 {
		t.Fatalf("asks %+v", asks)
	}
	webTestLaptopSelect(t, m, "keep")
	if _, ans := webTestFrames(t, r, r.c); len(ans) != 1 || ans[0].ID != asks[0].ID || ans[0].By != "laptop" {
		t.Fatalf("answered %+v", ans)
	}
	if f := r.call(webproto.OpCardsAnswer, &webproto.CardsAnswerArgs{ID: asks[0].ID, Choice: webproto.ChoiceConfirm}); webTestErr(f) != "gone" {
		t.Fatalf("a late answer: %v", f)
	}

	// No page action before it: the laptop's own dialog stays there.
	h.asks.origin = nil
	m.confirm = confirm.Risky("Put x to sleep?", "body", confirm.Button{ID: "keep", Title: "Keep running"}, confirm.Button{ID: "one", Title: "Sleep"})
	m.confirmOpen, m.confirmKind = true, confirmKindWTSleep
	m.webAfterUpdate(nil)
	if a, _ := webTestFrames(t, r, r.c); len(a) != 0 {
		t.Fatalf("a laptop-only dialog went to the page: %+v", a)
	}
	m.confirmOpen = false
	m.webAfterUpdate(nil)

	// A service's first run asks for its command on the laptop: after a
	// page's start, the page is asked too, with the exact command.
	h.asks.origin = &webAskOrigin{conn: r.c, at: webNow()}
	m.confirm = confirm.New("Run api?", "This project runs a command from Procfile:\n\nnpm run dev\n\nRun it in a terminal tab?",
		[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "run", Title: "Run", Style: confirm.StylePrimary}})
	m.confirmOpen, m.confirmKind = true, confirmKindServicesLocal
	m.webAfterUpdate(nil)
	if a, _ := webTestFrames(t, r, r.c); len(a) != 1 || a[0].Action != "Run" || !strings.Contains(a[0].Body, "npm run dev") {
		t.Fatalf("the service's question did not ask in the page: %+v", a)
	}
}

// Spec (owner 2026-11-08): the page's yes never presses a trust button
// or "Always": the main button of the laptop's dialog is its primary or
// destructive one that is neither.
func TestWebMirrorButton(t *testing.T) {
	for _, c := range []struct {
		name string
		btns []confirm.Button
		want string
	}{
		{"one", []confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "delete", Title: "Delete…", Style: confirm.StyleDestructive}}, "delete"},
		{"primary wins", []confirm.Button{{ID: "keep", Title: "Keep"}, {ID: "one", Title: "Only this"}, {ID: "all", Title: "All", Style: confirm.StylePrimary}}, "all"},
		{"no trust", []confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "archive", Title: "Archive"}, {ID: "trust", Title: "Trust script and archive", Style: confirm.StyleDestructive}}, "archive"},
		{"no always", []confirm.Button{{ID: "deny", Title: "Deny"}, {ID: "always", Title: "Always", Style: confirm.StylePrimary}}, ""},
	} {
		d := confirm.Risky("T", "B", c.btns[0], c.btns[1:]...)
		if got, _ := webMirrorButton(d); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
