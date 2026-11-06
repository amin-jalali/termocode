//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The small web parity leftovers (WEBP-9): laptop commands the desktop
// workbench's command list runs too.

// runFirstRow opens the command list, answers the laptop's list with none,
// types q and runs the first row, which must be label (a command may
// open the quick input again: the caller waits for what it does).
func runFirstRow(t *testing.T, p *Page, c *HubConn, q, label string) {
	t.Helper()
	wbKey(p, "P", true, true)
	reply(t, c, expectOp(t, c, "palette.list", `{}`), webproto.PaletteList{})
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key(q)
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === ` + jsq(label))
	p.Key("\r")
}

// Debug: Open launch.json asks the laptop for the file (it writes the
// template when there is none) and opens it in the editor.
func TestCommandOpensLaunchJSON(t *testing.T) {
	p, c := openFiles(t, Desktop)
	runFirstRow(t, p, c, "open launch", "Debug: Open launch.json")
	reply(t, c, expectOp(t, c, "debug.open_launch", `{}`), map[string]any{"path": ".termocode/launch.json"})
	p.WaitJS(`location.hash === "#/files/.termocode/launch.json"`)
	expectReq(t, c, "buf.open")
}

// Testing: Collapse All folds the TESTING tree, as its head button does.
func TestCommandCollapsesTheTestTree(t *testing.T) {
	p, c := openRunView(t, Desktop, "testing", "testing", "tests")
	c.Send(t, Snap("tests", 1, testsTree("idle", 1)))
	p.WaitJS(`document.querySelectorAll(".tx-row").length === 4`)
	runFirstRow(t, p, c, "testing collapse", "Testing: Collapse All")
	p.WaitJS(`document.querySelectorAll(".tx-row").length === 1 && !!` + txRow("p:pkg"))
}

// Output: Clear empties the OUTPUT panel's lines; the channel stays.
func TestCommandClearsTheOutput(t *testing.T) {
	p, c := deskLogin(t, nil)
	Eval[bool](p, `(location.hash = "#/more/containers", true)`)
	reply(t, c, expectOp(t, c, webproto.OpContainersList, `{}`), ctrList())
	reply(t, c, expectOp(t, c, webproto.OpContainersHost, `{}`), ctrHost())
	const side = `document.querySelector('.wb-side [data-view="containers"]')`
	p.WaitJS(side + `?.querySelectorAll(".tr[data-id]").length === 2`)
	p.TapJS(side + `.querySelector('.tr[data-id="9f3c2e1a1234"]')`)
	reply(t, c, expectOp(t, c, webproto.OpContainersLogs, `{"id":"9f3c2e1a1234"}`), webproto.ContainerLogs{ID: "9f3c2e1a1234", Lines: []string{"start", "ready"}})
	p.WaitJS(`document.querySelectorAll('.wb-panel [data-view="output"] .outv-line').length === 2`)
	runFirstRow(t, p, c, "output clear", "Output: Clear")
	p.WaitJS(`document.querySelectorAll('.wb-panel [data-view="output"] .outv-line').length === 0 && !!document.querySelector(".outv-channel")`)
}

// AI: Side Question… asks the quick model beside the main chat: the
// question box, then the answer in a sheet (Close first and focused);
// Ask another asks again, and the sheet keeps the side questions.
func TestCommandAsksASideQuestion(t *testing.T) {
	p, c := openFiles(t, Desktop)
	runFirstRow(t, p, c, "side question", "AI: Side Question…")
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box") && document.querySelector(".quick .quick-box").placeholder === "Ask (not added to the main chat)"`)
	p.Key("how do I compare errors?")
	p.Key("\r")
	reply(t, c, expectOp(t, c, "chat.side", `{"text":"how do I compare errors?"}`), map[string]any{"answer": "Use <b>errors.Is</b>."})
	p.WaitJS(`document.querySelector(".side-chat .side-a")?.textContent === "Use <b>errors.Is</b>." && document.activeElement?.textContent === "Close"`)
	if got := p.Text(".side-chat .side-q"); got != "how do I compare errors?" {
		t.Errorf("the question shows as %q", got)
	}
	p.TapJS(ButtonByText("Ask another"))
	p.WaitJS(`!document.querySelector(".side-chat") && document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key("and wrapping?")
	p.Key("\r")
	reply(t, c, expectOp(t, c, "chat.side", `{"text":"and wrapping?"}`), map[string]any{"answer": "Yes."})
	p.WaitJS(`document.querySelectorAll(".side-chat .side-a").length === 2`)
	p.TapJS(ButtonByText("Close"))
	p.WaitJS(`!document.querySelector(".side-chat")`)
}

// AI: MCP Prompts and Resources… lists the laptop's MCP prompts and
// resources; a pick goes into the chat draft (after what was typed),
// and nothing is sent.
func TestCommandPutsAnMCPItemInTheChat(t *testing.T) {
	p, c := openChat(t, Desktop, chatWith(false, userMsg(0, "hi")))
	typeDraft(p, "look at")
	runFirstRow(t, p, c, "mcp prompts", "AI: MCP Prompts and Resources…")
	reply(t, c, expectOp(t, c, "ai.mcp_items", `{}`), map[string]any{"items": []map[string]any{
		{"text": "/gh.review ", "title": "/gh.review — Review a PR", "hint": "prompt · pr"},
		{"text": "@gh:issue://12 ", "title": "@gh: Issue 12", "hint": "resource · issue://12"},
	}})
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box") && document.querySelectorAll(".quick-row").length === 2`)
	p.Key("issue")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "@gh: Issue 12"`)
	p.Key("\r")
	p.WaitJS(`!document.querySelector(".quick") && ` + composer + `.querySelector("textarea").value === "look at @gh:issue://12 "`)
}
