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

// Testing: Collapse All folds the TESTING tree, as its head button does.
func TestCommandCollapsesTheTestTree(t *testing.T) {
	p, c := openRunView(t, Desktop, "testing", "testing", "tests")
	c.Send(t, Snap("tests", 1, testsTree("idle", 1)))
	p.WaitJS(`document.querySelectorAll(".tx-row").length === 4`)
	runCmd(t, p, c, "testing collapse", "Testing: Collapse All")
	p.WaitJS(`document.querySelectorAll(".tx-row").length === 1 && !!` + txRow("p:pkg"))
}

// Output: Clear empties the OUTPUT panel's lines; the channel stays.
func TestCommandClearsTheOutput(t *testing.T) {
	p, c := deskLogin(t, nil)
	Eval[bool](p, `(location.hash = "#/more/containers", true)`)
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	const side = `document.querySelector('.wb-side [data-view="containers"]')`
	p.WaitJS(side + `?.querySelectorAll(".tr[data-id]").length === 2`)
	p.TapJS(side + `.querySelector('.tr[data-id="9f3c2e1a1234"]')`)
	reply(t, c, expectOp(t, c, webproto.OpContainersLogs, `{"id":"9f3c2e1a1234"}`), webproto.ContainerLogs{ID: "9f3c2e1a1234", Lines: []string{"start", "ready"}})
	p.WaitJS(`document.querySelectorAll('.wb-panel [data-view="output"] .outv-line').length === 2`)
	runCmd(t, p, c, "output clear", "Output: Clear")
	p.WaitJS(`document.querySelectorAll('.wb-panel [data-view="output"] .outv-line').length === 0 && !!document.querySelector(".outv-channel")`)
}
