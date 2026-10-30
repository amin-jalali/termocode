//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Pull requests and CI on the desktop: the list tab (the branch's CI and
// the open pull requests), a pull request's tab, and its actions through
// the laptop; Commit (Amend) and Clone Repository from source control's
// More Actions menu.

// answerReqs answers the page's requests by op until each was answered once.
func answerReqs(t *testing.T, c *HubConn, data map[string]any) {
	t.Helper()
	left := len(data)
	end := time.Now().Add(wait)
	for left > 0 {
		if time.Now().After(end) {
			t.Fatalf("%d requests not asked", left)
		}
		r, ok := c.Next(t).(*webproto.Req)
		if !ok {
			continue
		}
		if d, ok := data[r.Op]; ok {
			reply(t, c, r, d)
			delete(data, r.Op)
			left--
		}
	}
}

func prItem() webproto.PRItem {
	return webproto.PRItem{Number: 14, Title: "Add parser", Author: "ann", Branch: "feat/parser", Base: "main", URL: "https://example.com/o/r/pull/14", Review: "APPROVED", Checks: "fail"}
}

func prViewData() webproto.PRView {
	return webproto.PRView{Item: prItem(), State: "OPEN", Mergeable: "CONFLICTING",
		Body:    &webproto.MdNode{K: "doc", C: []webproto.MdNode{{K: "p", C: []webproto.MdNode{{K: "text", Text: "Adds a parser."}}}}},
		Files:   []webproto.PRFile{{Path: "parser.go", Add: 40, Del: 2}},
		Reviews: []webproto.PRReview{{Author: "bob", State: "CHANGES_REQUESTED", Body: &webproto.MdNode{K: "doc", C: []webproto.MdNode{{K: "p", C: []webproto.MdNode{{K: "text", Text: "Please add tests"}}}}}, When: "2026-10-28T10:00:00Z"}},
		Checks:  []webproto.PRCheck{{Name: "test", State: "fail", URL: "https://example.com/run/1"}, {Name: "build", State: "pass"}}}
}

func ciStatus() webproto.CIStatus {
	return webproto.CIStatus{Branch: "main", State: "pending", Head: "60ed1c8", Runs: []webproto.CIRun{
		{Workflow: "CI", Title: "fix it", Status: "in_progress", State: "pending"}, {Workflow: "Lint", Title: "fix it", Status: "completed", Conclusion: "success", State: "pass"}}}
}

func openPRs(t *testing.T) (*Page, *HubConn) {
	t.Helper()
	p, c := openGit(t, Desktop, gitSnap())
	p.TapJS(`document.querySelector('[data-view="git"] [data-act="prs"]')`)
	answerReqs(t, c, map[string]any{"pr.list": webproto.PRList{Items: []webproto.PRItem{prItem(), {Number: 13, Title: "WIP cache", Author: "bob", Branch: "wip", Base: "main", Draft: true, Checks: "none"}}}, "ci.status": ciStatus()})
	p.WaitJS(`document.querySelectorAll('[data-view="prs"] .pr-list .pr-row').length === 2 && document.querySelectorAll('[data-view="prs"] .pr-ci .pr-check').length === 2`)
	return p, c
}

func TestPRListAndDetailMerge(t *testing.T) {
	p, c := openPRs(t)
	if got := p.Text(`[data-view="prs"] .pr-row[data-pr="14"] .pr-sub`); got != "ann · feat/parser → mainApproved" {
		t.Errorf("row sub %q", got)
	}
	p.TapJS(`document.querySelector('[data-view="prs"] .pr-list .pr-row[data-pr="14"]')`)
	r := waitReq(t, c, "pr.view")
	if canonJSON(t, r.Args) != `{"number":14}` {
		t.Errorf("view args %s", r.Args)
	}
	reply(t, c, r, prViewData())
	p.WaitJS(`document.querySelector('[data-view="pr"] .pr-md')?.textContent.includes("Adds a parser.")`)
	if got := Eval[string](p, `document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent ?? ""`); got != "Pull Request #14" {
		t.Errorf("the tab is %q", got)
	}
	for _, want := range []string{"ann wants to merge feat/parser into main", "This branch has conflicts with main.", "parser.go", "Please add tests"} {
		if !Eval[bool](p, `document.querySelector('[data-view="pr"]').textContent.includes(`+jsq(want)+`)`) {
			t.Errorf("no %q", want)
		}
	}
	Eval[bool](p, `(() => { const s = document.querySelector('[data-view="pr"] .pr-method'); s.value = "rebase"; s.dispatchEvent(new Event("change", {bubbles: true})); return true; })()`)
	p.TapJS(`document.querySelector('[data-view="pr"] [data-act="merge"]')`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	if got := canonJSON(t, waitReq(t, c, "pr.merge").Args); got != `{"method":"rebase","number":14}` {
		t.Errorf("merge args %s", got)
	}
}

func TestPRReviewNeedsAMessage(t *testing.T) {
	p, c := openPRs(t)
	p.TapJS(`document.querySelector('[data-view="prs"] .pr-list .pr-row[data-pr="14"]')`)
	reply(t, c, waitReq(t, c, "pr.view"), prViewData())
	p.WaitJS(`document.querySelector('[data-view="pr"] [data-act="comment"]')`)
	p.TapJS(`document.querySelector('[data-view="pr"] [data-act="comment"]')`)
	p.WaitJS(`document.querySelector('[data-act="send-review"]')?.disabled === true`)
	Eval[bool](p, `(() => { const a = document.querySelector(".pr-review-text"); a.value = "Nice work"; a.dispatchEvent(new Event("input", {bubbles: true})); return true; })()`)
	p.WaitJS(`document.querySelector('[data-act="send-review"]')?.disabled === false`)
	p.TapJS(`document.querySelector('[data-act="send-review"]')`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	if got := canonJSON(t, waitReq(t, c, "pr.review").Args); got != `{"body":"Nice work","kind":"comment","number":14}` {
		t.Errorf("review args %s", got)
	}
}

func TestGitMoreAmendAndClone(t *testing.T) {
	p, c := openGit(t, Desktop, gitSnap())
	more := func(item string) {
		p.TapJS(`document.querySelector('[data-view="git"] [data-act="more"]')`)
		p.WaitJS(`document.querySelector(".ctx-menu")`)
		menuPick(p, item)
		p.WaitJS(`document.activeElement?.classList.contains("gx-input")`)
	}
	more("Commit (Amend)…")
	Eval[bool](p, `(() => { const a = document.querySelector(".gx-input"); a.value = "better subject"; a.dispatchEvent(new Event("input", {bubbles: true})); return true; })()`)
	p.TapJS(`document.querySelector('.gx-form [data-act="next"]')`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	r := waitReq(t, c, "git.amend")
	if canonJSON(t, r.Args) != `{"message":"better subject"}` {
		t.Errorf("amend args %s", r.Args)
	}
	reply(t, c, r, webproto.AskRef{Card: "card-a"})
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: "card-a", By: "laptop", Choice: "cancel"})})
	p.WaitJS(`!document.querySelector(".waiting")`)

	more("Clone Repository…")
	// Next does nothing while the URL is empty.
	p.TapJS(`document.querySelector('.gx-form [data-act="next"]')`)
	if !Eval[bool](p, `!!document.querySelector(".gx-form")`) {
		t.Fatal("an empty URL went on")
	}
	Eval[bool](p, `(() => { const a = document.querySelector(".gx-input"); a.value = "git@example.com:team/tool.git"; a.dispatchEvent(new Event("input", {bubbles: true})); return true; })()`)
	p.TapJS(`document.querySelector('.gx-form [data-act="next"]')`)
	p.WaitJS(`document.querySelector(".ask .ask-text")?.textContent.includes("git@example.com:team/tool.git")`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	if got := canonJSON(t, waitReq(t, c, "git.clone").Args); !strings.Contains(got, `"url":"git@example.com:team/tool.git"`) {
		t.Errorf("clone args %s", got)
	}
}
