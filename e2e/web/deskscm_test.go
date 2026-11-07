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

// The desktop's Source Control view (views/desk/DeskSCM.tsx).

const scm = `document.querySelector('.wb-side [data-view="git"]')`

func scmRow(group, path string) string {
	return scm + `.querySelector('.tr[data-group="` + group + `"][data-path="` + path + `"]')`
}

// openDeskSCM logs in on a desktop and shows the source control view.
func openDeskSCM(t *testing.T, meta *webproto.Meta) (*Page, *HubConn) {
	return openDeskSCMAt(t, Desktop, meta)
}

// openDeskSCMAt is openDeskSCM on a screen of size vp.
func openDeskSCMAt(t *testing.T, vp Viewport, meta *webproto.Meta) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	if meta == nil {
		meta = Meta("c-scm")
	}
	c := p.LoginMeta(meta)
	c.Send(t, Snap("git", 1, gitSnap()))
	Eval[bool](p, `(location.hash = "#/git", true)`)
	p.WaitJS(scmRow("changes", "src/a.go"))
	return p, c
}

// Groups with counts, untracked files in Changes with U, status letters;
// a hover action stages (with Undo); the group's action unstages all.
func TestDeskSCMGroupsAndStage(t *testing.T) {
	p, c := openDeskSCM(t, nil)
	if got := Eval[string](p, `[...`+scm+`.querySelectorAll('.tr[data-group]:not([data-path])')].map((e) => e.querySelector(".tr-label").textContent + " " + e.querySelector(".count").textContent).join(",")`); got != "Staged Changes 1,Changes 2" {
		t.Errorf("groups %q", got)
	}
	if got := Eval[string](p, `[...`+scm+`.querySelectorAll('.tr[data-path]')].map((e) => e.dataset.path + ":" + e.querySelector(".tr-letter").textContent).join(",")`); got != "b.go:M,new.txt:U,src/a.go:M" {
		t.Errorf("rows %q", got)
	}
	if got := Eval[string](p, scmRow("changes", "src/a.go")+`.querySelector(".tr-desc").textContent`); got != "src" {
		t.Errorf("folder %q", got)
	}
	Eval[bool](p, scmRow("changes", "src/a.go")+`.querySelector('[aria-label="Stage Changes"]').click() ?? true`)
	reply(t, c, expectOp(t, c, webproto.OpGitStage, `{"paths":["src/a.go"]}`), nil)
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Staged a.go")`)
	Eval[bool](p, scm+`.querySelector('.tr[data-group="staged"]:not([data-path]) [aria-label="Unstage All Changes"]').click() ?? true`)
	reply(t, c, expectOp(t, c, webproto.OpGitUnstage, `{"paths":["b.go"]}`), nil)
	// The arrow keys move along the rows; Enter opens the change.
	Eval[bool](p, `(`+scmRow("staged", "b.go")+`.focus(), true)`)
	p.Key(kb.ArrowDown)
	p.WaitJS(`document.activeElement?.dataset.group === "changes" && !document.activeElement.dataset.path`)
	p.Key(kb.ArrowDown + kb.ArrowDown + kb.Enter) // new.txt, src/a.go
	p.WaitJS(`location.hash.startsWith("#/files/%40diff%3Awt%3Asrc")`)
	// The change opens beside the view: the side bar keeps source control.
	p.WaitJS(`document.querySelector('[data-view="diff"]')`)
	if !Eval[bool](p, `!!`+scm+` && !!document.querySelector('nav.act-bar .act-item.is-active[data-act="git"]')`) {
		t.Error("opening a change left source control in the side bar")
	}
}

// Commit: Ctrl/Cmd+Enter in the message asks first, Cancel first and
// focused (Enter does not commit); then git.commit with the message.
func TestDeskSCMCommit(t *testing.T) {
	p, c := openDeskSCM(t, nil)
	box := `.wb-side [data-view="git"] textarea`
	p.WaitJS(`document.querySelector('` + box + `').placeholder === "Message (Ctrl+Enter to commit on 'main')" || document.querySelector('` + box + `').placeholder === "Message (⌘+Enter to commit on 'main')"`)
	p.Tap(box)
	p.Do(chromedp.SendKeys(box, "fix the toast"))
	p.Do(chromedp.KeyEvent(kb.Enter, modKey()))
	p.WaitJS(scm + `.querySelector(".pv-ask")?.textContent.includes("Commit 1 staged files to main?") && document.activeElement?.textContent === "Cancel"`)
	p.Key(kb.Enter) // Cancel
	p.WaitJS(`!` + scm + `.querySelector(".pv-ask")`)
	c.Quiet(t, 200*time.Millisecond)
	p.TapJS(scm + `.querySelector('[data-act="commit"]')`)
	p.TapJS(`[...` + scm + `.querySelectorAll(".pv-ask button")].find((b) => b.textContent === "Commit")`)
	reply(t, c, expectOp(t, c, webproto.OpGitCommit, `{"message":"fix the toast"}`), webproto.GitCommitted{Hash: "abc1234", Files: 1})
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Committed abc1234") && document.querySelector('` + box + `').value === ""`)
}

// Discard asks the laptop; the sync item in the status bar offers pull
// and push, which ask the laptop too.
func TestDeskSCMDiscardAndSync(t *testing.T) {
	p, c := openDeskSCM(t, nil)
	Eval[bool](p, scmRow("changes", "src/a.go")+`.querySelector('[aria-label="Discard Changes"]').click() ?? true`)
	p.WaitJS(`document.querySelector(".discard") && document.activeElement === document.querySelector(".discard .choices button")`)
	p.Key(kb.Enter) // Cancel
	p.WaitJS(`!document.querySelector(".discard")`)
	p.TapJS(`document.querySelector(".statusbar .sb-sync")`)
	reply(t, c, expectOp(t, c, webproto.OpGitBranches, `{}`), webproto.GitBranches{Current: "main", Branches: []string{"feat/web", "main"}})
	p.TapJS(`[...document.querySelectorAll('[role="menuitem"]')].find((b) => b.textContent.includes("Push"))`)
	p.WaitJS(`document.querySelector(".ask-text")?.textContent === "Push main to its remote?"`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	reply(t, c, expectOp(t, c, webproto.OpGitPush, `{}`), webproto.AskRef{Card: "card-9"})
	p.WaitJS(`document.querySelector(".waiting")`)
}

// A viewer reads the changes only.
func TestDeskSCMViewer(t *testing.T) {
	m := Meta("c-v")
	m.Role = webproto.RoleViewer
	p, _ := openDeskSCM(t, m)
	if n := Eval[int](p, scm+`.querySelectorAll('textarea, [aria-label="Stage Changes"], [aria-label="Discard Changes"], [data-act="commit"]').length`); n != 0 {
		t.Fatalf("a viewer sees %d actions", n)
	}
}

// scmClick clicks a row with the given modifier keys (ctrlKey, shiftKey).
func scmClick(p *Page, row, mods string) {
	p.T.Helper()
	Eval[bool](p, `(`+row+`.dispatchEvent(new MouseEvent("click", {bubbles: true, cancelable: true, `+mods+`})), true)`)
}

// GITX-20: Ctrl+click marks a row and Shift+click a range; a row action
// on a marked row acts on every marked row of its group; Esc and a plain
// click drop the marks; a discard of the marked rows asks the laptop once.
func TestDeskSCMMultiSelect(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.LoginMeta(Meta("c-scm"))
	snap := gitSnap()
	snap.Files = append(snap.Files, webproto.GitFile{Path: "c.go", X: " ", Y: "M", Unstaged: true}, webproto.GitFile{Path: "d.go", X: "M", Y: " ", Staged: true})
	c.Send(t, Snap("git", 1, snap))
	Eval[bool](p, `(location.hash = "#/git", true)`)
	p.WaitJS(scmRow("changes", "src/a.go"))
	marked := `[...` + scm + `.querySelectorAll('.tr.is-sel[data-path]')].map((e) => e.dataset.group + ":" + e.dataset.path).join(",")`
	// Changes in order: c.go, new.txt, src/a.go.
	scmClick(p, scmRow("changes", "c.go"), "ctrlKey: true")
	scmClick(p, scmRow("changes", "src/a.go"), "shiftKey: true")
	p.WaitJS(marked + ` === "changes:c.go,changes:new.txt,changes:src/a.go"`)
	scmClick(p, scmRow("changes", "new.txt"), "ctrlKey: true")
	p.WaitJS(marked + ` === "changes:c.go,changes:src/a.go"`)
	Eval[bool](p, scmRow("changes", "c.go")+`.querySelector('[aria-label="Stage Changes"]').click() ?? true`)
	reply(t, c, expectOp(t, c, webproto.OpGitStage, `{"paths":["c.go","src/a.go"]}`), nil)
	p.WaitJS(marked + ` === ""`)
	// Staged rows: mark both, unstage them at once from the right-click menu.
	scmClick(p, scmRow("staged", "b.go"), "ctrlKey: true")
	scmClick(p, scmRow("staged", "d.go"), "metaKey: true")
	p.WaitJS(marked + ` === "staged:b.go,staged:d.go"`)
	Eval[bool](p, `(`+scmRow("staged", "d.go")+`.dispatchEvent(new MouseEvent("contextmenu", {bubbles: true, cancelable: true, clientX: 40, clientY: 200})), true)`)
	p.TapJS(`[...document.querySelectorAll('[role="menuitem"]')].find((b) => b.textContent.includes("Unstage Changes"))`)
	reply(t, c, expectOp(t, c, webproto.OpGitUnstage, `{"paths":["b.go","d.go"]}`), nil)
	p.WaitJS(marked + ` === ""`)
	// Esc drops the marks; so does a plain click (which opens the change).
	scmClick(p, scmRow("changes", "c.go"), "ctrlKey: true")
	p.WaitJS(marked + ` === "changes:c.go"`)
	Eval[bool](p, `(`+scmRow("changes", "c.go")+`.focus(), true)`)
	p.Key(kb.Escape)
	p.WaitJS(marked + ` === ""`)
	// Discard of marked rows: one question, Cancel first, for all of them.
	scmClick(p, scmRow("changes", "c.go"), "ctrlKey: true")
	scmClick(p, scmRow("changes", "src/a.go"), "ctrlKey: true")
	Eval[bool](p, scmRow("changes", "src/a.go")+`.querySelector('[aria-label="Discard Changes"]').click() ?? true`)
	p.WaitJS(`document.querySelector(".discard") && document.activeElement === document.querySelector(".discard .choices button")`)
	p.WaitJS(`document.querySelector(".discard .arm.is-armed")`)
	p.TapJS(`[...document.querySelectorAll(".discard button")].find((b) => b.textContent === "Ask the laptop")`)
	reply(t, c, expectOp(t, c, webproto.OpGitDiscard, `{"paths":["c.go","src/a.go"]}`), webproto.AskRef{Card: "card-3"})
	p.WaitJS(`document.querySelector(".waiting")`)
}

// GITX-10: STASHES lists the stash, folded at first with its count; a
// click on an entry offers View Stash Changes (a read) and Drop Stash…,
// which asks here (Cancel first), then the laptop.
func TestDeskSCMStashes(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.LoginMeta(Meta("c-scm"))
	snap := gitSnap()
	snap.Stashes = []webproto.GitStashEntry{{Ref: "stash@{0}", Message: "On main: wip toast"}, {Ref: "stash@{1}", Message: "On main: old try"}}
	c.Send(t, Snap("git", 1, snap))
	Eval[bool](p, `(location.hash = "#/git", true)`)
	sec := scm + `.querySelector('[data-section="stashes"]')`
	p.WaitJS(sec + `?.querySelector(".ps-head")?.getAttribute("aria-expanded") === "false"`)
	if got := Eval[string](p, sec+`.querySelector(".ps-head").textContent`); !strings.Contains(got, "Stashes") || !strings.Contains(got, "2") {
		t.Errorf("head %q", got)
	}
	p.TapJS(sec + `.querySelector(".ps-head")`)
	row := sec + `.querySelector('.tr[data-stash="stash@{1}"]')`
	p.WaitJS(row + `?.querySelector(".tr-label")?.textContent === "On main: old try"`)
	p.TapJS(row)
	p.TapJS(`[...document.querySelectorAll('[role="menuitem"]')].find((b) => b.textContent.includes("View Stash Changes"))`)
	reply(t, c, expectOp(t, c, webproto.OpGitStashShow, `{"ref":"stash@{1}"}`),
		webproto.GitDiff{Rows: []webproto.DiffRow{{K: "file", Text: "a.go"}, {K: "add", Text: "stashed line", New: 1}}, Added: 1})
	p.WaitJS(`[...document.querySelectorAll(".diff-rows *")].some((e) => e.textContent === "stashed line")`)
	p.Key(kb.Escape)
	p.WaitJS(`!document.querySelector(".diff-sheet")`)
	p.TapJS(row)
	p.TapJS(`[...document.querySelectorAll('[role="menuitem"]')].find((b) => b.textContent.includes("Drop Stash"))`)
	p.WaitJS(`document.querySelector(".ask-text")?.textContent.includes("Drop stash@{1}") && document.activeElement === document.querySelector(".ask .choices button")`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	reply(t, c, expectOp(t, c, webproto.OpGitStashAct, `{"ref":"stash@{1}","do":"drop"}`), webproto.AskRef{Card: "card-8"})
	p.WaitJS(`document.querySelector(".waiting")`)
}

// GITX-10: a viewer can view a stash entry but has no actions on it.
func TestDeskSCMStashesViewer(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	m := Meta("c-v")
	m.Role = webproto.RoleViewer
	c := p.LoginMeta(m)
	snap := gitSnap()
	snap.Stashes = []webproto.GitStashEntry{{Ref: "stash@{0}", Message: "wip"}}
	c.Send(t, Snap("git", 1, snap))
	Eval[bool](p, `(location.hash = "#/git", true)`)
	sec := scm + `.querySelector('[data-section="stashes"]')`
	p.TapJS(sec + `?.querySelector(".ps-head")`)
	p.TapJS(sec + `.querySelector('.tr[data-stash="stash@{0}"]')`)
	p.WaitJS(`document.querySelectorAll('[role="menuitem"]').length > 0`)
	if got := Eval[string](p, `[...document.querySelectorAll('[role="menuitem"]')].map((b) => b.textContent).join(",")`); got != "View Stash Changes" {
		t.Errorf("viewer menu %q", got)
	}
}
