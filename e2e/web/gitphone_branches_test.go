//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The branch pill opens the Branches sheet (page design 07): one field
// finds a branch or names a new one; Local, Remote and Tags; the current
// branch first with ✓ and what waits to push; "+ New branch from main";
// Pull, Push and Stash under the list. Every action asks the laptop.
func TestGitPhoneBranches(t *testing.T) {
	p, c := openGit(t, Phone, gitSnap())
	open := func() {
		p.TapJS(`document.querySelector('.gp-pill')`)
		reply(t, c, expectOp(t, c, webproto.OpGitBranches, `{}`), webproto.GitBranches{Current: "main", Branches: []string{"feat/web", "main"}, Remotes: []string{"origin/feat/far"}, Stashes: 1, Upstream: true})
		p.WaitJS(`document.querySelector('.gp-br-row[data-branch="feat/web"]')`)
	}
	open()
	if got := p.Text(".gp-br .gp-cs-t"); got != "Branches" {
		t.Errorf("title %q", got)
	}
	if got := texts(p, ".gp-br-seg button"); got != "Local|Remote|Tags" {
		t.Errorf("segments %q", got)
	}
	if got := Eval[string](p, `[...document.querySelectorAll(".gp-br-row")].map((r) => r.dataset.branch).join(",")`); got != "main,feat/web" {
		t.Errorf("rows %q", got)
	}
	if got := p.Text(`.gp-br-row[aria-current="true"] .gp-dir`); got != "current · 1 to push" {
		t.Errorf("current %q", got)
	}
	if got := p.Text(`[data-act="new-branch"]`); got != "New branch from main" {
		t.Errorf("new row %q", got)
	}
	if got := texts(p, ".gp-br-acts button"); got != "Pull|Push|Stash" {
		t.Errorf("buttons %q", got)
	}
	if got := p.Text(".gp-br-note"); got != "Git on the laptop signs in. No password goes through this page." {
		t.Errorf("note %q", got)
	}
	// The field finds, then names a new branch.
	p.Tap(".gp-find input")
	p.Key("feat")
	p.WaitJS(`[...document.querySelectorAll(".gp-br-row")].map((r) => r.dataset.branch).join(",") === "feat/web"`)
	p.Key("ure/checkout")
	p.WaitJS(`document.querySelector('[data-act="new-branch"]')?.textContent.trim() === "Create feature/checkout from main"`)
	p.TapJS(`document.querySelector('[data-act="new-branch"]')`)
	p.WaitJS(`document.querySelector(".ask-text")?.textContent === "Make the branch feature/checkout from main and switch to it? Changed files go with you."`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	reply(t, c, expectOp(t, c, webproto.OpGitBranchCreate, `{"name":"feature/checkout"}`), webproto.AskRef{Card: "card-9"})
	p.WaitJS(`document.querySelector(".waiting")`)
	c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: "card-9", By: "laptop", Choice: "cancel"})})
	p.WaitJS(`!document.querySelector(".waiting")`)
	// A remote branch is tracked; a tag is checked out; Push asks.
	open()
	p.TapJS(`document.querySelector('.gp-br-seg [data-tab="remote"]')`)
	p.TapJS(`document.querySelector('.gp-br-row[data-branch="origin/feat/far"]')`)
	p.WaitJS(`document.querySelector(".ask-text")?.textContent === "Switch to feat/far? A new local branch tracks origin/feat/far."`)
	p.TapJS(`document.querySelector(".ask .choices button")`) // Cancel
	p.WaitJS(`!document.querySelector(".ask")`)
	open()
	p.TapJS(`document.querySelector('.gp-br-seg [data-tab="tags"]')`)
	reply(t, c, expectOp(t, c, webproto.OpGitTags, `{}`), webproto.GitTags{Tags: []string{"v1.0"}})
	p.TapJS(`document.querySelector('.gp-br-row[data-branch="v1.0"]')`)
	p.WaitJS(`document.querySelector(".ask-text")?.textContent === "Check out the tag v1.0? New commits there belong to no branch."`)
	p.TapJS(`document.querySelector(".ask .choices button")`)
	p.WaitJS(`!document.querySelector(".ask")`)
	open()
	p.TapJS(`document.querySelector('.gp-br-acts [data-act="push"]')`)
	p.WaitJS(`document.querySelector(".ask-text")?.textContent === "Push main to its remote?"`)
	p.noOverflow("branches")
}

// History (page design 07): a dot, the subject, the mono hash · author ·
// time and "not pushed"; a long press gives Ask AI about this, Add to
// chat, Show changes and Copy hash; a git.log view the AI saved ("not
// pushed + last 5, by date") shapes it with its banner.
func TestGitPhoneHistory(t *testing.T) {
	snap := gitSnap()
	now := time.Now()
	snap.Log = []webproto.GitCommit{
		{Hash: "a41c9e2", Subject: "Add Clear and checkout", Author: "Amin Jalali", When: now.Add(-time.Minute).UnixMilli(), Unpushed: true},
		{Hash: "5d0e1f2", Subject: "Start the cart", Author: "Amin Jalali", When: now.Add(-2 * time.Hour).UnixMilli()},
	}
	p, c := openGit(t, Phone, snap)
	p.TapJS(`document.querySelector('[data-act="history"]')`)
	reply(t, c, expectOp(t, c, webproto.OpViewList, ""), noViews)
	p.WaitJS(`document.querySelectorAll(".gp-commit-row").length === 2`)
	if loc := Eval[string](p, "location.hash"); loc != "#/git?view=history" {
		t.Errorf("route %q", loc)
	}
	if got := texts(p, ".gp-commit-row"); got != "Add Clear and checkouta41c9e2 · Amin Jalali · 1mnot pushed|Start the cart5d0e1f2 · Amin Jalali · 2h" {
		t.Errorf("rows %q", got)
	}
	const menu = `document.querySelector(".gp-menu")`
	p.press(`document.querySelector('.gp-commit-row[data-hash="a41c9e2"]')`, menu)
	if got := texts(p, ".gp-menu .gp-mi"); got != "Ask AI about this|Add to chat ↩|Show changes|Copy hash" {
		t.Errorf("menu %q", got)
	}
	p.TapJS(ButtonByText("Show changes"))
	reply(t, c, expectOp(t, c, webproto.OpGitShow, `{"hash":"a41c9e2"}`), webproto.GitDiff{Commit: &webproto.GitCommitInfo{Hash: "a41c9e2", Subject: "Add Clear and checkout", Author: "Amin Jalali", When: now.UnixMilli()}, Rows: []webproto.DiffRow{}})
	p.WaitJS(`document.querySelector(".show-subject")?.textContent === "Add Clear and checkout"`)

	// The AI's view of the log.
	views := map[string]any{"max": 32, "max_pinned": 8, "items": []any{map[string]any{
		"id": "v2", "name": "Not pushed + last 5", "by": "ai", "saved": 1700000000000,
		"spec": map[string]any{"view": "git.log", "limit": 5, "sort": []any{map[string]any{"field": "when", "desc": true}},
			"filter": []any{map[string]any{"field": "unpushed", "op": "eq", "value": "true"}}},
	}}}
	p2, c2 := openGit(t, Phone, snap)
	p2.TapJS(`document.querySelector('[data-act="history"]')`)
	reply(t, c2, expectOp(t, c2, webproto.OpViewList, ""), views)
	p2.WaitJS(`document.querySelector(".gp-aiv.is-ai") && document.querySelectorAll(".gp-commit-row").length === 1`)
	if txt := p2.Text(".gp-aiv"); !strings.Contains(txt, "Made by AI: Not pushed + last 5 · not pushed · by date · max 5") {
		t.Errorf("banner %q", txt)
	}
	p2.TapJS(`document.querySelector('[data-act="view-reset"]')`)
	p2.WaitJS(`!document.querySelector(".gp-aiv") && document.querySelectorAll(".gp-commit-row").length === 2`)
}
