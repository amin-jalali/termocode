//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The small web parity leftovers (WEBP-9): laptop commands the desktop
// workbench's command list runs too.

// runCmd opens the command list, answers the laptop's list with none,
// types q and runs the first row, which must be label.
func runCmd(t *testing.T, p *Page, c *HubConn, q, label string) {
	t.Helper()
	wbKey(p, "P", true, true)
	reply(t, c, expectOp(t, c, "palette.list", `{}`), webproto.PaletteList{})
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key(q)
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === ` + jsq(label))
	p.Key("\r")
	p.WaitJS(`!document.querySelector(".quick")`)
}

// Debug: Open launch.json asks the laptop for the file (it writes the
// template when there is none) and opens it in the editor.
func TestCommandOpensLaunchJSON(t *testing.T) {
	p, c := openFiles(t, Desktop)
	runCmd(t, p, c, "open launch", "Debug: Open launch.json")
	reply(t, c, expectOp(t, c, "debug.open_launch", `{}`), map[string]any{"path": ".termocode/launch.json"})
	p.WaitJS(`location.hash === "#/files/.termocode/launch.json"`)
	expectReq(t, c, "buf.open")
}
