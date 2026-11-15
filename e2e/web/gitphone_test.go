//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The phone's Git page (page design 05, gitphone.js) against the fake
// hub at 390×844 with touch: the head with the branch pill, History, ⇅
// and ⋯; filter chips with counts; the files as grouped cards with ⚙ on
// each group and a round tick that stages; the commit field and a split
// Commit button docked at the bottom; a long press on a row opens its
// menu; a git.status view the AI saved reshapes the groups.

const gpRow = `document.querySelector('.gp-row[data-path="src/a.go"]')`

func texts(p *Page, sel string) string {
	return Eval[string](p, `[...document.querySelectorAll(`+jsq(sel)+`)].map((e) => e.textContent.replace(/\s+/g, " ").trim()).join("|")`)
}

func TestGitPhonePage(t *testing.T) {
	p, c := openGit(t, Phone, gitSnap())
	if got := texts(p, ".gp-chip"); got != "All 3|Staged 1|Changes 1|Untracked 1" {
		t.Errorf("chips %q", got)
	}
	if got := texts(p, ".gp-sec-t"); got != "Staged|Changes|Untracked" {
		t.Errorf("groups %q", got)
	}
	if n := Eval[int](p, `document.querySelectorAll(".gp-sec .gp-gear").length`); n != 3 {
		t.Errorf("%d ⚙ on 3 groups", n)
	}
	if n := Eval[int](p, `document.querySelectorAll(".git-log, .git-commit").length`); n != 0 {
		t.Error("recent commits are on the changes page; they belong in History")
	}
	if pill := p.Text(".gp-pill"); !strings.Contains(pill, "main") || !strings.Contains(pill, "↑1") {
		t.Errorf("branch pill %q", pill)
	}
	if got := Eval[string](p, `[...document.querySelectorAll('[data-view="git"] .view-actions [data-act]')].map((b) => b.dataset.act).join(",")`); got != "history,sync,more" {
		t.Errorf("head buttons %q", got)
	}
	if got := Eval[string](p, `document.querySelector(".gp-dock .gp-msg")?.placeholder`); got != "Commit message" {
		t.Errorf("commit field %q", got)
	}
	if got := p.Text(`.gp-dock [data-act="commit"]`); got != "Commit 1" {
		t.Errorf("commit button %q", got)
	}
	p.WaitJS(`document.querySelector('.gp-dock [data-act="commit-more"][aria-haspopup="menu"]')`)
	if got := Eval[string](p, `[...document.querySelectorAll(".gp-row")].map((r) => r.dataset.path + ":" + (r.querySelector(".gp-circ")?.getAttribute("aria-checked") ?? "-")).join(",")`); got != "b.go:true,src/a.go:false,new.txt:false" {
		t.Errorf("ticks %q", got)
	}
	// A chip shows its group alone.
	p.TapJS(`document.querySelector('.gp-chip[data-chip="staged"]')`)
	p.WaitJS(`[...document.querySelectorAll(".gp-sec-t")].map((e) => e.textContent).join("|") === "Staged"`)
	p.TapJS(`document.querySelector('.gp-chip[data-chip="all"]')`)
	p.WaitJS(`document.querySelectorAll(".gp-sec").length === 3`)
	// The round tick stages; the toast's Undo unstages.
	p.TapJS(gpRow + `.querySelector(".gp-circ")`)
	reply(t, c, expectOp(t, c, webproto.OpGitStage, `{"paths":["src/a.go"]}`), nil)
	p.WaitJS(`document.querySelector(".toasts .toast-title")?.textContent === "Staged a.go"`)
	p.TapJS(ButtonByText("Undo"))
	reply(t, c, expectOp(t, c, webproto.OpGitUnstage, `{"paths":["src/a.go"]}`), nil)
	// A tap on the row opens its diff.
	p.TapJS(gpRow + `.querySelector(".gp-open")`)
	reply(t, c, expectOp(t, c, webproto.OpGitDiff, `{"path":"src/a.go"}`), webproto.GitDiff{Path: "src/a.go", Rows: []webproto.DiffRow{}})
	if loc := Eval[string](p, "location.hash"); loc != "#/git/src/a.go" {
		t.Errorf("route %q", loc)
	}
	p.noOverflow("phone git")
	if pr := p.Problems(); len(pr) > 0 {
		t.Errorf("problems: %q", pr)
	}
}

// A long press on a row opens its menu: Ask AI about this and Add to
// chat first, then Open diff, Stage and Discard….
func TestGitPhoneRowMenu(t *testing.T) {
	p, c := openGit(t, Phone, gitSnap())
	const menu = `document.querySelector(".gp-menu")`
	p.press(gpRow, menu)
	if got := texts(p, ".gp-menu .gp-mi"); got != "Ask AI about this|Add to chat ↩|Open diff|Stage|Discard…" {
		t.Errorf("menu %q", got)
	}
	if got := p.Text(".gp-menu-t"); got != "src/a.go" {
		t.Errorf("menu title %q", got)
	}
	p.Holds(menu, 400*time.Millisecond) // the lifted finger does not close it
	p.TapJS(ButtonByText("Add to chat ↩"))
	p.WaitJS(`!` + menu + ` && document.querySelector(".toasts")?.textContent.includes("Added to the chat.")`)
	p.press(gpRow, menu)
	p.TapJS(ButtonByText("Discard…"))
	p.WaitJS(`document.querySelector(".discard")`)
	if txt := p.Text(".discard-text"); txt != "Discard changes in src/a.go? This cannot be undone." {
		t.Errorf("question %q", txt)
	}
	c.Quiet(t, 200*time.Millisecond)
	// The staged row offers Unstage, no Discard.
	p.TapJS(`document.querySelector(".discard .choices button")`)
	p.press(`document.querySelector('.gp-row[data-path="b.go"]')`, menu)
	if got := texts(p, ".gp-menu .gp-mi"); got != "Ask AI about this|Add to chat ↩|Open diff|Unstage" {
		t.Errorf("staged row's menu %q", got)
	}
}

// A git.status view the AI saved reshapes the page: only *.go, grouped
// by folder, with a banner that says so; Reset brings the page's own
// groups back.
func TestGitPhoneAIView(t *testing.T) {
	views := map[string]any{"max": 32, "max_pinned": 8, "items": []any{map[string]any{
		"id": "v1", "name": "Go files", "by": "ai", "saved": 1700000000000,
		"spec": map[string]any{"view": "git.status", "group": "path", "filter": []any{map[string]any{"field": "path", "op": "contains", "value": ".go"}}},
	}}}
	p, _ := openGitViews(t, Phone, gitSnap(), views)
	p.WaitJS(`document.querySelector(".gp-aiv.is-ai")`)
	if txt := p.Text(".gp-aiv"); !strings.Contains(txt, "Made by AI:") || !strings.Contains(txt, "Go files · only *.go · by folder") {
		t.Errorf("banner %q", txt)
	}
	if got := texts(p, ".gp-sec-t"); got != "src|Top folder" {
		t.Errorf("groups %q", got)
	}
	if n := Eval[int](p, `document.querySelectorAll('.gp-row[data-path="new.txt"]').length`); n != 0 {
		t.Error("new.txt shows through an only-*.go view")
	}
	if n := Eval[int](p, `document.querySelectorAll(".gp-card.is-cfg").length`); n != 2 {
		t.Errorf("%d cards marked as reshaped", n)
	}
	p.TapJS(`document.querySelector('[data-act="view-reset"]')`)
	p.WaitJS(`!document.querySelector(".gp-aiv") && [...document.querySelectorAll(".gp-sec-t")].map((e) => e.textContent).join("|") === "Staged|Changes|Untracked"`)
}

// Commit asks first (Cancel first), then commits the message; an empty
// message says so instead.
func TestGitPhoneCommit(t *testing.T) {
	p, c := openGit(t, Phone, gitSnap())
	p.TapJS(`document.querySelector('.gp-dock [data-act="commit"]')`)
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Write a commit message first.")`)
	c.Quiet(t, 200*time.Millisecond)
	p.Tap(".gp-msg")
	p.Key("fix: one line")
	p.TapJS(`document.querySelector('.gp-dock [data-act="commit"]')`)
	p.WaitJS(`document.querySelector(".commit-ask-text")?.textContent === "Commit 1 staged file to main?"`)
	if first := p.Text(".commit-ask .choices button"); first != "Cancel" {
		t.Errorf("first choice %q", first)
	}
	p.TapJS(`[...document.querySelectorAll(".commit-ask button")].find((b) => b.textContent.trim() === "Commit")`)
	reply(t, c, expectOp(t, c, webproto.OpGitCommit, `{"message":"fix: one line"}`), webproto.GitCommitted{Hash: "9f9f9f9", Files: 1})
	p.WaitJS(`[...document.querySelectorAll(".toasts .toast-title")].some((e) => e.textContent === "Committed 9f9f9f9 · 1 files")`)
	p.WaitJS(`document.querySelector(".gp-msg").value === "" && !document.querySelector(".commit-ask")`)
}

// A viewer reads only: no ticks, no commit bar, no ⇅.
func TestGitPhoneViewer(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	m := Meta("c-v")
	m.Role = webproto.RoleViewer
	c := p.LoginMeta(m)
	p.GoTab("git")
	waitGitSub(t, p, c)
	c.Send(t, Snap("git", 1, gitSnap()))
	p.WaitJS(`document.querySelector('.gp-row[data-path="b.go"]')`)
	if n := Eval[int](p, `document.querySelectorAll('.gp-circ, .gp-dock, [data-act="sync"]').length`); n != 0 {
		t.Fatalf("a viewer sees %d actions", n)
	}
}

// Not a repository, and a clean tree: say so.
func TestGitPhoneEmptyStates(t *testing.T) {
	p, c := openGit(t, Phone, gitSnap())
	c.Send(t, Delta("git", 2, webproto.GitSnap{Repo: false, Files: []webproto.GitFile{}, Log: []webproto.GitCommit{}}))
	p.WaitJS(`document.querySelector(".gp .empty")?.textContent === "This workspace is not a git repository."`)
	c.Send(t, Delta("git", 3, webproto.GitSnap{Repo: true, Branch: webproto.GitBranch{Name: "main"}, Files: []webproto.GitFile{},
		Log: []webproto.GitCommit{{Hash: "1a2b3c4", Subject: "first", When: time.Now().UnixMilli()}}}))
	p.WaitJS(`document.querySelector(".gp .git-clean")?.textContent === "Nothing to commit. The tree is clean." && !document.querySelector(".gp-dock")`)
	p.noOverflow("phone git clean")
}
