//go:build browser

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The Git tab (WEB-23, web UX 3.5, 3.9, 5.7) against the fake hub, on the
// phone (touch) and the desktop (mouse).

// gitSnap is a git topic value: a.go changed, b.go staged, new.txt new.
func gitSnap() webproto.GitSnap {
	return webproto.GitSnap{Repo: true, Branch: webproto.GitBranch{Name: "main", Ahead: 1},
		Files: []webproto.GitFile{
			{Path: "src/a.go", X: " ", Y: "M", Unstaged: true},
			{Path: "b.go", X: "M", Y: " ", Staged: true},
			{Path: "new.txt", X: "?", Y: "?", Untracked: true},
		},
		Log: []webproto.GitCommit{{Hash: "1a2b3c4", Subject: "first", Author: "Ann", When: time.Now().Add(-time.Hour).UnixMilli()}}}
}

// openGit logs in, opens the Git tab and sends the git snapshot.
func openGit(t *testing.T, vp Viewport, snap webproto.GitSnap) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-git")
	p.GoTab("git")
	waitGitSub(t, p, c)
	c.Send(t, Snap("git", 1, snap))
	p.WaitJS(`document.querySelector('.git-row-wrap[data-path="b.go"]')`)
	return p, c
}

// expectOp reads the page's messages until a req for op (subs and
// unsubs on the way are fine) and checks its args. It gives up after
// wait: a page that sends subs and unsubs without end must fail the
// test, not hang the suite (macOS CI, 2026-10-30: 15 minutes).
func expectOp(t *testing.T, c *HubConn, op, args string) *webproto.Req {
	t.Helper()
	end := time.Now().Add(wait)
	for {
		if time.Now().After(end) {
			t.Fatalf("no %s request within %s (only subs and unsubs)", op, wait)
		}
		m := c.Next(t)
		switch v := m.(type) {
		case *webproto.Sub, *webproto.Unsub:
			continue
		case *webproto.Req:
			if v.Op != op {
				t.Fatalf("page asked %s %s; want %s", v.Op, v.Args, op)
			}
			if args != "" && canonJSON(t, v.Args) != canonJSON(t, json.RawMessage(args)) {
				t.Fatalf("%s args %s; want %s", op, v.Args, args)
			}
			return v
		default:
			t.Fatalf("page sent %s; want a %s request", show(m), op)
		}
	}
}

func canonJSON(t *testing.T, b json.RawMessage) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

// reply answers a req with data (nil: no data).
func reply(t *testing.T, c *HubConn, r *webproto.Req, data any) {
	t.Helper()
	res := &webproto.Res{T: webproto.KindRes, ID: r.ID}
	if data != nil {
		res.Data = raw(data)
	}
	c.Send(t, res)
}

func rowSel(path string) string {
	return fmt.Sprintf(`.git-row-wrap[data-path=%q]`, path)
}

// The groups show what git says; Stage on a row stages it, the toast
// offers Undo, which unstages it again.
func TestGitGroupsAndStageWithUndo(t *testing.T) {
	for _, vp := range RowViews {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openGit(t, vp, gitSnap())
			got := Eval[[]string](p, `[...document.querySelectorAll(".git-group .group-title")].map((e) => e.textContent.trim())`)
			if strings.Join(got, "|") != "Staged (1)|Changes (1)|Untracked (1)|Recent commits" {
				t.Fatalf("groups %q", got)
			}
			if txt := p.Text(".view-sub"); !strings.Contains(txt, "main") || !strings.Contains(txt, "↑1") {
				t.Errorf("branch line %q", txt)
			}
			p.TapJS(`document.querySelector('` + rowSel("src/a.go") + ` .git-row .btn')`)
			reply(t, c, expectOp(t, c, webproto.OpGitStage, `{"paths":["src/a.go"]}`), nil)
			p.WaitJS(`document.querySelector(".toasts .toast-title")?.textContent === "Staged a.go"`)
			p.TapJS(ButtonByText("Undo"))
			reply(t, c, expectOp(t, c, webproto.OpGitUnstage, `{"paths":["src/a.go"]}`), nil)
			p.WaitJS(`document.querySelector(".toasts .toast-title")?.textContent !== "Staged a.go"`)
		})
	}
}

// The commit box asks inside itself with Cancel first and focused; Enter
// in the subject commits nothing; Commit sends the message and the toast
// names the new commit.
func TestGitCommitBox(t *testing.T) {
	for _, vp := range RowViews {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openGit(t, vp, gitSnap())
			p.Tap(".commit-subject")
			p.Do(chromedp.SendKeys(".commit-subject", "fix: one line"+kb.Enter))
			c.Quiet(t, 300*time.Millisecond) // Enter never commits
			p.TapJS(ButtonByText("Commit 1 file"))
			p.WaitJS(`document.activeElement === document.querySelector(".commit-ask button")`)
			if txt := p.Text(".commit-ask-text"); txt != "Commit 1 staged files to main?" {
				t.Errorf("ask %q", txt)
			}
			p.Key(kb.Enter) // Cancel has the focus
			p.WaitJS(`!document.querySelector(".commit-ask")`)
			c.Quiet(t, 200*time.Millisecond)
			p.TapJS(ButtonByText("Commit 1 file"))
			p.WaitJS(`document.querySelector(".commit-ask")`)
			p.TapJS(`[...document.querySelectorAll(".commit-ask button")].find((b) => b.textContent.trim() === "Commit")`)
			r := expectOp(t, c, webproto.OpGitCommit, `{"message":"fix: one line"}`)
			reply(t, c, r, webproto.GitCommitted{Hash: "9f9f9f9", Files: 1})
			p.WaitJS(`[...document.querySelectorAll(".toasts .toast-title")].some((e) => e.textContent === "Committed 9f9f9f9 · 1 files")`)
			p.WaitJS(`document.querySelector(".commit-subject").value === ""`)
		})
	}
}

// A commit of the log opens with its subject, author and rows.
func TestGitShowCommit(t *testing.T) {
	p, c := openGit(t, Phone, gitSnap())
	p.TapJS(`document.querySelector(".git-commit")`)
	r := expectOp(t, c, webproto.OpGitShow, `{"hash":"1a2b3c4"}`)
	reply(t, c, r, webproto.GitDiff{Commit: &webproto.GitCommitInfo{Hash: "1a2b3c4", Subject: "first", Author: "Ann", When: time.Now().UnixMilli()},
		Added: 1, Rows: []webproto.DiffRow{{K: "file", Text: "a.go"}, {K: "hunk", Text: "@@ -0,0 +1 @@"}, {K: "add", Text: "x", New: 1}}})
	p.WaitJS(`document.querySelector(".show-subject")?.textContent === "first" && document.querySelectorAll(".diff-rows .drow").length === 3`)
	if loc := Eval[string](p, "location.hash"); loc != "#/git?show=1a2b3c4" {
		t.Errorf("route %q", loc)
	}
}

// swipe drags a touch from the row's left (or right) side across most
// of it, in small steps, as a thumb does.
func (p *Page) swipe(sel string, right bool) {
	p.T.Helper()
	b := Eval[struct{ X, Y, W float64 }](p, `(() => { const r = document.querySelector(`+fmt.Sprintf("%q", sel)+`).getBoundingClientRect(); return {X: r.left, Y: r.top + r.height / 2, W: r.width}; })()`)
	from, to := b.X+b.W*0.15, b.X+b.W*0.9
	if !right {
		from, to = to, from
	}
	touch := func(ctx context.Context, tg *chromedp.Target, typ input.DispatchTouchEventType, x float64) error {
		id := 1.0
		pts := []*input.TouchPoint{}
		if typ != input.DispatchTouchEventTypeTouchEnd {
			pts = append(pts, &input.TouchPoint{X: x, Y: b.Y, ID: &id})
		}
		_, err := cdp.Call(ctx, tg, input.DispatchTouchEvent, input.DispatchTouchEventParams{Type: typ, TouchPoints: pts})
		return err
	}
	p.Do(chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		if err := touch(ctx, tg, input.DispatchTouchEventTypeTouchStart, from); err != nil {
			return err
		}
		for i := 1; i <= 8; i++ {
			if err := touch(ctx, tg, input.DispatchTouchEventTypeTouchMove, from+(to-from)*float64(i)/8); err != nil {
				return err
			}
		}
		return touch(ctx, tg, input.DispatchTouchEventTypeTouchEnd, to)
	}))
}

// A swipe right past half the row stages it, a swipe left unstages a
// staged row; a short swipe does nothing; the verb shows under the row.
func TestGitSwipeStages(t *testing.T) {
	p, c := openGit(t, Phone, gitSnap())
	p.swipe(rowSel("new.txt")+" .git-row", true)
	reply(t, c, expectOp(t, c, webproto.OpGitStage, `{"paths":["new.txt"]}`), nil)
	p.WaitJS(`document.querySelector(".toasts .toast-title")?.textContent === "Staged new.txt"`)
	p.swipe(rowSel("b.go")+" .git-row", false)
	reply(t, c, expectOp(t, c, webproto.OpGitUnstage, `{"paths":["b.go"]}`), nil)
	// Staged rows do not move right: nothing is sent.
	p.swipe(rowSel("b.go")+" .git-row", true)
	c.Quiet(t, 400*time.Millisecond)
	if pr := p.Problems(); len(pr) > 0 { // the row moves by CSSOM, never a style attribute
		t.Errorf("problems: %q", pr)
	}
}

// Tapping a row opens its diff: rows with the hunk header; "Stage this
// part" sends the exact header; more rows come page by page. (The
// desktop opens a diff editor tab instead: diffeditor_test.go.)
func TestGitDiffSheet(t *testing.T) {
	for _, vp := range RowViews {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openGit(t, vp, gitSnap())
			p.TapJS(`document.querySelector('` + rowSel("src/a.go") + ` .git-open')`)
			r := expectOp(t, c, webproto.OpGitDiff, `{"path":"src/a.go"}`)
			hunk := "@@ -1,2 +1,2 @@ func main() {"
			reply(t, c, r, webproto.GitDiff{Path: "src/a.go", Added: 1, Removed: 1, Next: 3, Rows: []webproto.DiffRow{
				{K: "hunk", Text: hunk, Old: 1, New: 1}, {K: "del", Text: "old <b>line</b>", Old: 1}, {K: "add", Text: "new line", New: 1}}})
			p.WaitJS(`document.querySelectorAll(".diff-rows .drow").length === 3`)
			if loc := Eval[string](p, "location.hash"); loc != "#/git/src/a.go" {
				t.Errorf("route %q", loc)
			}
			if Eval[int](p, `document.querySelectorAll(".diff-rows b").length`) != 0 {
				t.Fatal("diff text became HTML")
			}
			p.TapJS(ButtonByText("Show more rows"))
			r = expectOp(t, c, webproto.OpGitDiff, `{"path":"src/a.go","from":3}`)
			reply(t, c, r, webproto.GitDiff{Path: "src/a.go", From: 3, Rows: []webproto.DiffRow{{K: "ctx", Text: "tail", Old: 2, New: 2}}})
			p.WaitJS(`document.querySelectorAll(".diff-rows .drow").length === 4`)
			p.TapJS(ButtonByText("Stage this part"))
			reply(t, c, expectOp(t, c, webproto.OpGitStageHunk, `{"path":"src/a.go","hunk":"`+hunk+`"}`), nil)
			expectOp(t, c, webproto.OpGitDiff, `{"path":"src/a.go"}`) // read again
			if pr := p.Problems(); len(pr) > 0 {
				t.Errorf("problems: %q", pr)
			}
		})
	}
}

// Discard asks the laptop: an in-page question first (Cancel first and
// focused, the action arms), then the waiting state until the answered
// event; Cancel there cancels the laptop's card.
func TestDiscardWaitsForLaptop(t *testing.T) {
	for _, vp := range RowViews {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openGit(t, vp, gitSnap())
			p.TapJS(`document.querySelector('` + rowSel("src/a.go") + ` .git-open')`)
			reply(t, c, expectOp(t, c, webproto.OpGitDiff, ""), webproto.GitDiff{Path: "src/a.go", Rows: []webproto.DiffRow{}})
			p.TapJS(ButtonByText("Discard changes…"))
			p.WaitJS(`document.querySelector(".discard") && document.activeElement === document.querySelector(".discard .choices button")`)
			if txt := p.Text(".discard-text"); txt != "Discard changes in src/a.go? This cannot be undone." {
				t.Errorf("question %q", txt)
			}
			p.Key(kb.Enter) // the focused button is Cancel
			p.WaitJS(`!document.querySelector(".discard")`)
			c.Quiet(t, 200*time.Millisecond)

			p.TapJS(ButtonByText("Discard changes…"))
			p.WaitJS(`document.querySelector(".discard .arm.is-armed")`)
			p.TapJS(ButtonByText("Ask the laptop"))
			r := expectOp(t, c, webproto.OpGitDiscard, `{"paths":["src/a.go"]}`)
			reply(t, c, r, webproto.AskRef{Card: "card-7"})
			p.WaitJS(`document.querySelector(".waiting-title")?.textContent === "Waiting for the laptop…"`)
			p.TapJS(`document.querySelector(".waiting button")`)
			cr := expectOp(t, c, webproto.OpCardsAnswer, `{"id":"card-7","choice":"cancel"}`)
			reply(t, c, cr, nil)
			c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: "card-7", By: "phone", Choice: "cancel"})})
			p.WaitJS(`!document.querySelector(".waiting")`)
			p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Cancelled. Nothing changed.")`)

			// Confirmed on the laptop.
			p.TapJS(ButtonByText("Discard changes…"))
			p.WaitJS(`document.querySelector(".discard .arm.is-armed")`)
			p.TapJS(ButtonByText("Ask the laptop"))
			reply(t, c, expectOp(t, c, webproto.OpGitDiscard, ""), webproto.AskRef{Card: "card-8"})
			p.WaitJS(`document.querySelector(".waiting")`)
			c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: "card-8", By: "laptop", Choice: "confirm"})})
			p.WaitJS(`!document.querySelector(".waiting") && document.querySelector(".toasts")?.textContent.includes("Done on the laptop.")`)
		})
	}
}

// A viewer reads only: no stage buttons and no commit box. Offline,
// actions say Offline.
func TestGitViewerReadsOnly(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Tablet)
	p.Open()
	p.Tap(".connect-foot button")
	c := s.Hub.Accept(t)
	Expect[*webproto.Hello](t, c)
	m := Meta("c-v")
	m.Role = webproto.RoleViewer
	c.Send(t, m)
	c.Subs(t, "status", "toasts")
	c.Send(t, Snap("status", 1, statusData("main", 3)))
	p.WaitJS(`document.documentElement.dataset.link === "online"`)
	p.GoTab("git")
	waitGitSub(t, p, c)
	c.Send(t, Snap("git", 1, gitSnap()))
	p.WaitJS(`document.querySelector('` + rowSel("b.go") + `')`)
	if n := Eval[int](p, `document.querySelectorAll(".git-row .btn, .commit-box").length`); n != 0 {
		t.Fatalf("a viewer sees %d actions", n)
	}
}

// Not a repository, and the laptop still reading: say so.
func TestGitEmptyStates(t *testing.T) {
	p, c := openGit(t, Phone, gitSnap())
	c.Send(t, Delta("git", 2, webproto.GitSnap{Repo: false, Files: []webproto.GitFile{}, Log: []webproto.GitCommit{}}))
	p.WaitJS(`document.querySelector(".git .empty")?.textContent === "This workspace is not a git repository."`)
	c.Send(t, Delta("git", 3, webproto.GitSnap{Repo: true, Branch: webproto.GitBranch{Name: "main"}, Files: []webproto.GitFile{},
		Log: []webproto.GitCommit{{Hash: "1a2b3c4", Subject: "first", When: time.Now().UnixMilli()}}}))
	p.WaitJS(`document.querySelector(".git-clean")?.textContent === "Nothing to commit. The tree is clean."`)
	if n := Eval[int](p, `document.querySelectorAll(".git-commit").length`); n != 1 {
		t.Errorf("log rows %d", n)
	}
	p.noOverflow("git tab")
}

// A laptop card (a destructive web request waiting for the laptop) is
// shown as laptop-only in the cards view: no action button, no error.
func TestLaptopCardIsLaptopOnly(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	c.Send(t, Snap("cards", 1, map[string]any{"cards": []any{map[string]any{
		"id": "card-3", "kind": "laptop", "title": "Discard changes?", "since": 1700000000000,
		"body": "Discard changes in a.go? This cannot be undone.\n\nRequested from phone.",
		"choices": []any{
			map[string]any{"id": "cancel", "label": "Cancel", "safe": true},
			map[string]any{"id": "confirm", "label": "Discard", "needs_laptop": true},
		}}}}))
	reopenCard(p)
	if n := Eval[int](p, `[...document.querySelectorAll(".sheet button")].filter((b) => b.textContent.trim() === "Discard").length`); n != 0 {
		t.Fatalf("%d Discard buttons on the web", n)
	}
	if title := p.Text(".sheet .card-title"); title != "This needs the laptop." {
		t.Errorf("title %q", title)
	}
	if pr := p.Problems(); len(pr) > 0 {
		t.Errorf("problems: %q", pr)
	}
}

// waitGitSub waits for the page to ask for the git topic; the desktop
// workbench asks for it at the start (the editor's change marks).
func waitGitSub(t *testing.T, p *Page, c *HubConn) {
	t.Helper()
	if p.VP.W >= 1200 {
		return
	}
	c.WaitSub(t, "git")
}
