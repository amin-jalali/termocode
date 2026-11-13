//go:build unix

package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestFrames sorts what c got: its ask evs and answered evs.
func webTestFrames(t *testing.T, r *webFilesRig, c *webConn) (asks []webproto.AskCard, answered []webproto.Answered) {
	t.Helper()
	for _, f := range r.takeOn(c) {
		if f["t"] != "ev" {
			continue
		}
		switch f["kind"] {
		case webproto.EventAsk:
			var a webproto.AskCard
			webTestData(t, f, &a)
			asks = append(asks, a)
		case webproto.EventAnswered:
			var a webproto.Answered
			webTestData(t, f, &a)
			answered = append(answered, a)
		}
	}
	return asks, answered
}

// Spec (owner 2026-11-08, "match the TUI exactly"): where the TUI does
// an action without a question (switch branch, check out a tag, stash,
// pop or apply a stash, push, pull, use a profile, a model or an MCP
// profile, auto-approve, add or remove a workspace folder, open a
// workspace, open a database from a container, open a host terminal or
// a container shell, pull an image, check out a PR), the page asks
// nothing either: the op runs at once, the page hears answered
// (confirm), the audit says it ran from the page. With
// web_approve_on_laptop on, the laptop card stays as before.
func TestWebNoQuestionWhereTheTUIAsksNone(t *testing.T) {
	want := []string{
		webproto.OpAIUseMCPProfile, webproto.OpAIUseModel, webproto.OpAIUseProfile, webproto.OpAISetAutoApprove,
		webproto.OpContainersExec, webproto.OpContainersHostTerminal, webproto.OpContainersOpenDB,
		webproto.OpGitCheckout, webproto.OpGitCheckoutTag, webproto.OpGitPull, webproto.OpGitPush,
		webproto.OpGitStash, webproto.OpGitStashApply, webproto.OpGitStashPop, webproto.OpPRCheckout,
		webproto.OpWorkspaceAddFolder, webproto.OpWorkspaceOpen, webproto.OpWorkspaceRemoveFolder,
	}
	var got []string
	for op := range webTUIAsksNone {
		got = append(got, op)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("ops the TUI does without a question:\n got %v\nwant %v", got, want)
	}
	for _, op := range got {
		if p, ok := webproto.Lookup(op); !ok || !p.LaptopCard {
			t.Errorf("%s: not a laptop-card op (the setting must still bring its card back)", op)
		}
	}

	m := webAITopicModel(t)
	h := newWebHub()
	t.Cleanup(h.stop)
	h.approveOnLaptop = false
	m.web = h
	m.output = newOutputStore()
	r := &webFilesRig{t: t, m: m, h: h, kept: map[*webConn][]map[string]any{}}
	r.c = r.conn("c-1", webproto.RoleController)
	r.c.device = "phone"
	model := ai.SuggestedModels(ai.KindAnthropic)[1]

	f := r.call(webproto.OpAIUseModel, &webproto.AIUseArgs{Name: model})
	var ref webproto.AskRef
	webTestData(t, f, &ref)
	webDrain(t, m, m.webAfterUpdate(nil))
	asks, answered := webTestFrames(t, r, r.c)
	if len(asks) != 0 {
		t.Fatalf("the page was asked although the TUI asks nothing: %+v", asks)
	}
	if m.confirmOpen {
		t.Fatal("a dialog opened on the laptop")
	}
	if m.ai.settings.Model != model {
		t.Fatalf("model %q, want %q: the op did not run", m.ai.settings.Model, model)
	}
	if len(answered) != 1 || answered[0] != (webproto.Answered{ID: ref.Card, By: "phone", Choice: webproto.ChoiceConfirm}) {
		t.Fatalf("answered %+v (card %q)", answered, ref.Card)
	}
	if a := strings.Join(m.output.Lines(webAuditChannel), "\n"); !strings.Contains(a, "ai.use_model  "+model+"  by=web:phone  → ran on phone (page)") {
		t.Errorf("audit:\n%s", a)
	}

	// The laptop setting on: its card, as before.
	h.approveOnLaptop = true
	r.call(webproto.OpAIUseProfile, &webproto.AIUseArgs{Name: ai.ProfileReview})
	if !m.confirmOpen || m.confirmKind != confirmKindWebAsk {
		t.Fatal("no laptop card with web_approve_on_laptop on")
	}
}

// Spec (owner 2026-11-08): the variants of one op follow the TUI too:
// an image pull and a container shell run at once from the page (the
// TUI asks nothing), a push still asks in the page (the TUI asks too).
func TestWebContainersVariantsFollowTheTUI(t *testing.T) {
	m, c, terms, _ := webTestHost(t)
	m.web.approveOnLaptop = false
	webTestCallBg(t, m, c, webproto.OpContainersList, `{}`)
	webTestCallBg(t, m, c, webproto.OpContainersHost, `{}`)
	for _, a := range [][2]string{
		{webproto.OpContainersImage, `{"verb":"pull","ref":"redis:7"}`},
		{webproto.OpContainersExec, `{"id":"9f3c2e1a1234","kind":"shell"}`},
	} {
		res, ask := webTestHereCall(t, m, c, a[0], a[1])
		if res["t"] != "res" || ask != nil || m.confirmOpen {
			t.Fatalf("%s %s: res %v ask %+v laptop dialog %v", a[0], a[1], res, ask, m.confirmOpen)
		}
	}
	if len(*terms) != 2 || !strings.Contains((*terms)[0], "pull redis:7") || !strings.Contains((*terms)[1], "exec -it 9f3c2e1a1234") {
		t.Fatalf("terminals %q", *terms)
	}
	if _, ask := webTestHereCall(t, m, c, webproto.OpContainersImage, `{"verb":"push","ref":"postgres:16"}`); ask == nil || ask.Action != "Push" || len(*terms) != 2 {
		t.Fatalf("push: ask %+v terminals %q", ask, *terms)
	}
}
