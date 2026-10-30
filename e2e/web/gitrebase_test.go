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

// Interactive rebase on the desktop: the commits oldest first with an
// action each; keys set the action, Alt+↑/↓ move a row; a reword row
// takes its message; Start asks the laptop with exactly that list.

func rebasePlan() webproto.GitRebasePlan {
	return webproto.GitRebasePlan{Base: "0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f", Branch: "main", Pushed: 1, Upstream: "origin/main", Steps: []webproto.GitRebaseStep{
		{Hash: "aaaa111", Action: "pick", Subject: "one"},
		{Hash: "bbbb222", Action: "pick", Subject: "two", Body: "why two"},
		{Hash: "cccc333", Action: "pick", Subject: "three"},
	}}
}

// openRebase opens the rebase tab of aaaa111 (as the graph's menu does).
func openRebase(t *testing.T) (*Page, *HubConn) {
	t.Helper()
	p, c := openGit(t, Desktop, gitSnap())
	Eval[bool](p, `(location.hash = "#/files/@git:rebase:aaaa111", true)`)
	r := waitReq(t, c, "git.rebase_plan")
	if canonJSON(t, r.Args) != `{"from":"aaaa111"}` {
		t.Errorf("plan args %s", r.Args)
	}
	reply(t, c, r, rebasePlan())
	p.WaitJS(`document.querySelectorAll('[data-view="rebase"] li.gr-row').length === 3`)
	return p, c
}

func rebaseRows(p *Page) string {
	return strings.Join(Eval[[]string](p, `[...document.querySelectorAll('[data-view="rebase"] li.gr-row')].map((r) => r.querySelector(".gr-action").value + " " + r.dataset.hash)`), ",")
}

func TestRebaseTabOrdersAndAsksLaptop(t *testing.T) {
	p, c := openRebase(t)
	if !Eval[bool](p, `document.querySelector('[data-view="rebase"] .gr-warn')?.textContent.includes("origin/main")`) {
		t.Error("no warning that a commit is pushed already")
	}
	// f on the third row makes it a fixup; Alt+↑ moves it above "two".
	Eval[bool](p, `(document.querySelector('li.gr-row[data-hash="cccc333"]').focus(), true)`)
	p.Key("f")
	p.Do(chromedp.KeyEvent(kb.ArrowUp, chromedp.KeyModifiers(chromedp.ModifierAlt)))
	p.WaitJS(`document.activeElement?.dataset?.hash === "cccc333"`)
	if got := rebaseRows(p); got != "pick aaaa111,fixup cccc333,pick bbbb222" {
		t.Fatalf("rows %s", got)
	}
	// r on "two" makes a reword with its message to edit.
	Eval[bool](p, `(document.querySelector('li.gr-row[data-hash="bbbb222"]').focus(), true)`)
	p.Key("r")
	p.WaitJS(`document.querySelector('li.gr-row[data-hash="bbbb222"] textarea.gr-msg')?.value === "two\n\nwhy two"`)
	Eval[bool](p, `(() => { const a = document.querySelector('li.gr-row[data-hash="bbbb222"] textarea.gr-msg'); a.value = "two, better"; a.dispatchEvent(new Event("input", {bubbles: true})); return true; })()`)
	if got := p.Text(".gr-sum"); got != "3 commits → 2 commits" {
		t.Errorf("summary %q", got)
	}
	p.TapJS(`document.querySelector('[data-view="rebase"] [data-act="start"]')`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	r := waitReq(t, c, "git.rebase")
	want := `{"base":"0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f","steps":[{"action":"pick","hash":"aaaa111"},{"action":"fixup","hash":"cccc333"},{"action":"reword","hash":"bbbb222","message":"two, better"}]}`
	if got := canonJSON(t, r.Args); got != want {
		t.Errorf("rebase args\n got %s\nwant %s", got, want)
	}
	reply(t, c, r, webproto.AskRef{Card: "card-r"})
	p.WaitJS(`document.querySelector(".waiting")`)
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: "card-r", By: "laptop", Choice: "confirm"})})
	// Done: the tab closes.
	p.WaitJS(`!document.querySelector('[data-view="rebase"]')`)
}

// A list git could not run (a fixup on top) cannot start.
func TestRebaseTabRefusesFixupOnTop(t *testing.T) {
	p, c := openRebase(t)
	Eval[bool](p, `(document.querySelector('li.gr-row[data-hash="aaaa111"]').focus(), true)`)
	p.Key("s")
	p.WaitJS(`document.querySelector('[data-view="rebase"] [data-act="start"]').disabled`)
	if !Eval[bool](p, `document.querySelector(".gr-bad")?.textContent === "The first row can't be squash or fixup."`) {
		t.Error("no reason given")
	}
	noReq(t, c, 200*time.Millisecond)
}

// A stopped rebase in source control: where it stopped, and Continue,
// Skip and Abort through the laptop.
func TestRebaseBannerAbortAsksLaptop(t *testing.T) {
	snap := gitSnap()
	snap.Op = "rebase"
	p, c := openGit(t, Desktop, snap)
	reply(t, c, waitReq(t, c, "git.rebase_state"), webproto.GitRebaseState{InProgress: true, Step: 2, Total: 3, Action: "pick", Hash: "bbbb222", Subject: "two", Conflicts: []string{"a.go"}})
	p.WaitJS(`document.querySelector(".gr-banner")?.textContent.includes("Rebase stopped at bbbb222 “two” (step 2 of 3).")`)
	p.TapJS(`document.querySelector('.gr-banner [data-act="abort"]')`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	r := waitReq(t, c, "git.rebase_ctl")
	if canonJSON(t, r.Args) != `{"do":"abort"}` {
		t.Errorf("args %s", r.Args)
	}
}
