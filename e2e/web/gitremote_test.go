//go:build browser

package web

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Branch and remote (WEB-42): the ⇅ in the Git head lists the branches,
// and push, pull, stash and checkout each ask in the page, then wait for
// the laptop.

func openRemote(t *testing.T, vp Viewport) (*Page, *HubConn) {
	t.Helper()
	p, c := openGit(t, vp, gitSnap())
	p.TapJS(`document.querySelector(".head-remote")`)
	reply(t, c, expectOp(t, c, webproto.OpGitBranches, `{}`), webproto.GitBranches{Current: "main", Branches: []string{"feat/web", "main"}, Stashes: 1, Upstream: true})
	p.WaitJS(`document.querySelector('.branch-row[data-branch="feat/web"]')`)
	return p, c
}

func TestGitPushWaitsForLaptop(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openRemote(t, vp)
			if n := Eval[int](p, `document.querySelectorAll('.branch-row[data-branch="main"]').length`); n != 0 {
				t.Error("the current branch is offered as a switch")
			}
			if first := p.Text(".remote .file-menu .btn"); first != "Cancel" {
				t.Errorf("first choice %q", first)
			}
			p.TapJS(ButtonByText("Push"))
			p.WaitJS(`document.querySelector(".ask-text")?.textContent === "Push main to its remote?"`)
			p.WaitJS(`document.activeElement === document.querySelector(".ask .choices button")`)
			p.Key(kb.Enter) // Cancel is focused
			p.WaitJS(`!document.querySelector(".ask")`)
			c.Quiet(t, 200*time.Millisecond)

			p.TapJS(`document.querySelector(".head-remote")`)
			reply(t, c, expectOp(t, c, webproto.OpGitBranches, `{}`), webproto.GitBranches{Current: "main", Branches: []string{"feat/web", "main"}})
			p.TapJS(`document.querySelector('.branch-row[data-branch="feat/web"]')`)
			p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
			p.TapJS(ButtonByText("Ask the laptop"))
			reply(t, c, expectOp(t, c, webproto.OpGitCheckout, `{"branch":"feat/web"}`), webproto.AskRef{Card: "card-4"})
			p.WaitJS(`document.querySelector(".waiting")`)
			c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: "card-4", By: "laptop", Choice: "cancel"})})
			p.WaitJS(`!document.querySelector(".waiting") && document.querySelector(".toasts")?.textContent.includes("Cancelled on the laptop.")`)
			p.noOverflow("git remote")
		})
	}
}

// Pop shows when the stash has entries; stash when files changed.
func TestGitStashButtons(t *testing.T) {
	p, c := openRemote(t, Phone)
	p.TapJS(ButtonByText("Pop the stash"))
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	reply(t, c, expectOp(t, c, webproto.OpGitStashPop, `{}`), webproto.AskRef{Card: "card-5"})
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: "card-5", By: "laptop", Choice: "confirm"})})
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Done on the laptop.")`)
	p.TapJS(`document.querySelector(".head-remote")`)
	reply(t, c, expectOp(t, c, webproto.OpGitBranches, `{}`), webproto.GitBranches{Current: "main", Branches: []string{"main"}})
	p.WaitJS(`document.querySelector(".remote .empty")?.textContent === "There is no other branch."`)
	if n := Eval[int](p, `[...document.querySelectorAll(".remote .btn")].filter((b) => b.textContent === "Pop the stash").length`); n != 0 {
		t.Error("pop offered with an empty stash")
	}
	p.TapJS(ButtonByText("Stash changes"))
	p.WaitJS(`document.querySelector(".ask-text")?.textContent === "Stash all changes? Pop brings them back."`)
}

// A viewer gets no ⇅.
func TestGitRemoteNotForViewer(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	m := Meta("c-v")
	m.Role = webproto.RoleViewer
	c := p.LoginMeta(m)
	p.GoTab("git")
	c.WaitSub(t, "git")
	c.Send(t, Snap("git", 1, gitSnap()))
	p.WaitJS(`document.querySelector('` + rowSel("b.go") + `')`)
	if n := Eval[int](p, `document.querySelectorAll(".head-remote").length`); n != 0 {
		t.Fatal("a viewer sees the branch and remote button")
	}
}
