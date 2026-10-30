//go:build browser

package web

import (
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
