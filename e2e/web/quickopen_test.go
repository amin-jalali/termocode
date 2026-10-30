//go:build browser

package web

import (
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The workbench's quick input: Go to file (Ctrl/Cmd+P) ranks the
// workspace's files as the user types, the command list (Ctrl/Cmd+Shift+P)
// runs a command by name, Go to line (":") moves the editor's cursor.

// wbKey sends one workbench key to the page (as the keyboard would).
func wbKey(p *Page, key string, ctrl, shift bool) {
	Eval[bool](p, `(dispatchEvent(new KeyboardEvent("keydown", {key: `+jsq(key)+`, ctrlKey: `+boolJS(ctrl)+`, shiftKey: `+boolJS(shift)+`, bubbles: true, cancelable: true})), true)`)
}

func boolJS(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestQuickOpenFindsAFileByItsName(t *testing.T) {
	p, c := openFiles(t, Desktop)
	wbKey(p, "p", true, false)
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	r := expectReq(t, c, "files.list")
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"paths": []string{"README.md", "cmd/covreport/filter.go", "cmd/covreport/main.go", "internal/app/filters.go", "docs/file.md"}})})
	p.Key("filgo")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "filter.go"`)
	if got := p.Text(".quick-row.is-on .quick-detail"); got != "cmd/covreport" {
		t.Errorf("the first row's folder is %q", got)
	}
	if got := Eval[string](p, `[...document.querySelectorAll(".quick-row.is-on .quick-label mark")].map((m) => m.textContent).join("")`); got != "filgo" {
		t.Errorf("marked letters %q, want the typed ones", got)
	}
	p.Key("\r")
	p.WaitJS(`!document.querySelector(".quick") && location.hash === "#/files/cmd/covreport/filter.go"`)
	expectReq(t, c, "buf.open")
	// Esc closes without doing anything.
	wbKey(p, "p", true, false)
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key(kb.Escape)
	p.WaitJS(`!document.querySelector(".quick")`)
}

func TestCommandListRunsACommand(t *testing.T) {
	p, _ := openFiles(t, Desktop)
	wbKey(p, "P", true, true)
	p.WaitJS(`document.querySelector(".quick .quick-box")?.value === ">" && document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key("toggle side")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "View: Toggle Side Bar"`)
	if got := p.Text(".quick-row.is-on .quick-keys"); got != "Ctrl+B" && got != "⌘+B" {
		t.Errorf("the command shows the keys %q", got)
	}
	p.Do(chromedp.KeyEvent("\r"))
	p.WaitJS(`!document.querySelector(".quick") && !document.querySelector(".wb-side")`)
}

func TestGoToLineMovesTheCursor(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.txt", "one", "two", "three", "four")
	wbKey(p, "g", true, false)
	p.WaitJS(`document.querySelector(".quick .quick-box")?.value === ":" && document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key("3")
	p.WaitJS(`document.querySelector(".quick-row.is-on")?.textContent === "Go to line 3"`)
	p.Key("\r")
	p.WaitJS(`document.querySelector(".statusbar .sb-pos")?.textContent === "Ln 3, Col 1"`)
}

// Ctrl+Tab goes to the next editor tab, Ctrl/Cmd+W closes the active one.
func TestTabKeys(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openBufAs(t, p, c, 2, "a.txt", nil, "a")
	openBufAs(t, p, c, 4, "b.txt", nil, "b")
	wbKey(p, "Tab", true, false)
	p.WaitJS(`location.hash === "#/files/a.txt"`)
	nextOpen(t, c)
	wbKey(p, "w", true, false)
	p.WaitJS(`document.querySelectorAll(".ed-tab").length === 1 && location.hash === "#/files/b.txt"`)
}
