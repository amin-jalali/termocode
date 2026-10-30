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

// The desktop editor (workbench): the vendored editor in a frame of its
// own, always live on the laptop's buffer. Typing sends buf.set_text with
// the tick the page knows, the laptop's changes come in at once, undo
// takes back the user's own typing only, the colours come from buf.hl.

const deskFrame = `document.querySelector(".desk-editor")`
const deskDoc = deskFrame + `?.contentDocument`

// deskText is the text the desktop editor shows, line by line.
func deskText(p *Page) string {
	return Eval[string](p, `[...`+deskDoc+`.querySelectorAll(".view-lines .view-line")].sort((a, b) => a.offsetTop - b.offsetTop).map((l) => l.textContent.replace(/ /g, " ")).join("\n")`)
}

// openDesk opens path as buffer buf on the desktop and waits for its text.
func openDesk(t *testing.T, p *Page, c *HubConn, buf int, path string, lines ...string) {
	t.Helper()
	openBufAs(t, p, c, buf, path, nil, lines...)
	p.WaitJS(deskDoc + `?.querySelector(".view-lines .view-line")`)
	if got := deskText(p); got != strings.Join(lines, "\n") {
		t.Fatalf("the editor shows %q", got)
	}
}

// deskFocus gives the editor the keyboard (its input element, in the frame).
func deskFocus(p *Page) {
	p.T.Helper()
	Eval[bool](p, `(`+deskFrame+`.focus(), `+deskDoc+`.querySelector(".native-edit-context, textarea.inputarea, textarea").focus(), true)`)
}

// deskType puts the caret at the text's start (or end) and types s.
func deskType(p *Page, end bool, s string) {
	p.T.Helper()
	deskFocus(p)
	k := kb.Home
	if end {
		k = kb.End
	}
	p.Do(chromedp.KeyEvent(k, modKey()))
	p.Do(chromedp.KeyEvent(s))
}

func TestDeskEditorTwoWaySyncAndUndo(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "notes.txt", "alpha", "beta é")
	if slicesHas(p.Requests(), "/assets/editor.js") {
		t.Error("the desktop loaded the phone editor")
	}
	// One pause, one set_text: the change at the tick the page knows.
	deskType(p, false, "X")
	r, a := expectSetText(t, c)
	if a.Buf != 2 || a.Tick != 3 || a.Start != (webproto.Pos{}) || a.End != (webproto.Pos{}) || strings.Join(a.Lines, "|") != "X" {
		t.Fatalf("set_text %+v", a)
	}
	okEdit(t, c, r, 4)
	c.Send(t, Delta("buf:2", 2, map[string]any{"kind": "lines", "first": 0, "last": 1, "lines": []string{"Xalpha"}, "tick": 4}))
	// The laptop types: it shows at once; nothing goes back.
	c.Send(t, Delta("buf:2", 3, map[string]any{"kind": "lines", "first": 1, "last": 2, "lines": []string{"beta é laptop"}, "tick": 5}))
	p.WaitJS(`(() => { const t = [...` + deskDoc + `.querySelectorAll(".view-line")].map((l) => l.textContent.replace(/ /g, " ")); return t.includes("beta é laptop") })()`)
	noReq(t, c, 300*time.Millisecond)
	// Typing after a multi-byte row counts bytes for nvim.
	deskType(p, true, "!")
	r, a = expectSetText(t, c)
	if a.Tick != 5 || a.Start != (webproto.Pos{Row: 1, Col: 14}) || a.End != a.Start || strings.Join(a.Lines, "|") != "!" {
		t.Fatalf("set_text at the end %+v", a)
	}
	okEdit(t, c, r, 6)
	// Ctrl/Cmd+Z takes back the user's own change, never the laptop's.
	p.Do(chromedp.KeyEvent("z", modKey()))
	r, a = expectSetText(t, c)
	if a.Tick != 6 || a.Start != (webproto.Pos{Row: 1, Col: 14}) || a.End != (webproto.Pos{Row: 1, Col: 15}) || strings.Join(a.Lines, "|") != "" {
		t.Fatalf("undo sent %+v", a)
	}
	okEdit(t, c, r, 7)
	if got := deskText(p); got != "Xalpha\nbeta é laptop" {
		t.Errorf("after undo the editor has %q", got)
	}
	// Ctrl/Cmd+S in the editor saves through the laptop.
	p.Do(chromedp.KeyEvent("s", modKey()))
	r = expectReq(t, c, "buf.save")
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"tick": 8, "dirty": false})})
	p.WaitJS(`(` + toastsJS + `).includes("Saved. The laptop's on-save hooks ran.")`)
	// The cursor's place is in the status bar.
	p.WaitJS(`/^Ln 2, Col \d+$/.test(document.querySelector(".statusbar .sb-pos")?.textContent ?? "")`)
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}

// The colours of buf.hl paint inside the editor.
func TestDeskEditorColours(t *testing.T) {
	p, c := openFiles(t, Desktop)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce", "prefers-color-scheme": "dark"})
	openDesk(t, p, c, 2, "a.go", `if x { return f(1) }`)
	r, _ := expectHl(t, c)
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"tick": 3, "first": 0, "last": 1,
		"groups": []string{"keyword.conditional", "function.call"}, "spans": [][]int{{0, 0, 2, 0}, {0, 14, 15, 1}}})})
	p.WaitJS(deskDoc + `.querySelector(".view-lines .hl-kc")?.textContent === "if"`)
	if got := Eval[string](p, `getComputedStyle(`+deskDoc+`.querySelector(".view-lines .hl-kc")).color`); got != "rgb(197, 134, 192)" {
		t.Errorf("a control keyword is %s, want the default theme's #c586c0", got)
	}
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}

// The workbench keys work while the editor has the focus.
func TestDeskEditorPassesWorkbenchKeys(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.txt", "one")
	deskFocus(p)
	p.Do(chromedp.KeyEvent("b", modKey()))
	p.WaitJS(`!document.querySelector(".wb-side")`)
	p.Do(chromedp.KeyEvent("b", modKey()))
	p.WaitJS(`document.querySelector(".wb-side")`)
}

// A viewer's editor reads only: typing changes nothing and sends nothing.
func TestDeskEditorViewerReadsOnly(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	m := Meta("c-v")
	m.Role = webproto.RoleViewer
	c := p.LoginMeta(m)
	openDesk(t, p, c, 2, "a.txt", "one")
	p.WaitJS(`document.querySelector(".ed-tabbar .ed-actions .view-actions .chip")?.textContent === "View only"`)
	deskType(p, false, "X")
	noReq(t, c, 400*time.Millisecond)
	if got := deskText(p); got != "one" {
		t.Errorf("a viewer typed into the file: %q", got)
	}
}

// The open file's git changes show in the gutter: changed, added and
// deleted rows, read from its unstaged diff once the file is saved.
func TestDeskEditorGitGutter(t *testing.T) {
	p, c := openFiles(t, Desktop)
	c.Send(t, Snap("git", 1, webproto.GitSnap{Repo: true, Branch: webproto.GitBranch{Name: "main"},
		Files: []webproto.GitFile{{Path: "a.txt", X: " ", Y: "M", Unstaged: true}}}))
	openDesk(t, p, c, 2, "a.txt", "one", "TWO", "three", "new", "five")
	r := expectReq(t, c, "git.diff")
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(webproto.GitDiff{Path: "a.txt", Rows: []webproto.DiffRow{
		{K: "hunk", Text: "@@ -1,5 +1,5 @@"},
		{K: "ctx", Text: "one", Old: 1, New: 1},
		{K: "del", Text: "two", Old: 2},
		{K: "add", Text: "TWO", New: 2},
		{K: "ctx", Text: "three", Old: 3, New: 3},
		{K: "add", Text: "new", New: 4},
		{K: "ctx", Text: "five", Old: 4, New: 5},
	}})})
	p.WaitJS(deskDoc + `.querySelector(".tc-gm-mod") && ` + deskDoc + `.querySelector(".tc-gm-add")`)
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems: %v", probs)
	}
}

// The editor has its features (the vendored editor's contributions):
// Ctrl/Cmd+F opens its find box, and nothing breaks the frame's policy
// (it runs no worker).
func TestDeskEditorFind(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.txt", "one two", "two three")
	deskFocus(p)
	p.Do(chromedp.KeyEvent("f", modKey()))
	p.WaitJS(deskDoc + `.querySelector(".find-widget.visible")`)
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}
