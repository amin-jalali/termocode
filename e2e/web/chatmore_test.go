//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The chat's More Actions (web parity, AI chat): rename, fork, export,
// delete and clear the chat history; the laptop's own AI commands (a
// setting or a paid turn asks here first, Cancel first, then the laptop);
// and the laptop's AI screens, read only. A desktop shows a menu at the
// button, a phone a sheet.

const chatMoreBtn = `document.querySelector('[data-view="chat"] .chat-more')`

// moreItem is the menu entry (desktop menu or phone sheet) with label.
func moreItem(label string) string {
	return `[...document.querySelectorAll(".ctx-item, .row-menu-item")].find((b) => (b.querySelector(".ctx-label")?.textContent ?? b.textContent).trim() === ` + jsq(label) + `)`
}

// pickMore opens More Actions and picks label; the first open asks the
// laptop for its commands.
func pickMore(t *testing.T, p *Page, c *HubConn, label string, first bool) {
	t.Helper()
	p.TapJS(chatMoreBtn)
	if first {
		reply(t, c, expectOp(t, c, "palette.list", `{}`), aiLaptopCmds())
	}
	p.WaitJS(moreItem(label))
	p.TapJS(moreItem(label))
}

func aiLaptopCmds() webproto.PaletteList {
	return webproto.PaletteList{Items: []webproto.PaletteItem{
		{ID: "save-all", Title: "File: Save All Files"},
		{ID: "ai-plan-approval", Title: "AI: Toggle Plan Approval", Ask: true},
		{ID: "ai-mcp-reconnect-all", Title: "AI: MCP: Reconnect All Servers"},
	}}
}

func TestChatMoreHistoryActions(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openChat(t, vp, chatWith(false, userMsg(0, "first question"), aiMsg(1, md("doc", mdPara("an answer"))), userMsg(2, "second question")))

			pickMore(t, p, c, "Rename Chat…", true)
			p.WaitJS(`document.activeElement === document.querySelector(".name-input") && document.querySelector(".name-input").value === "Fix the build"`)
			p.Key("Better title\r")
			reply(t, c, expectOp(t, c, "chat.rename", `{"id":"c-1","title":"Better title"}`), nil)
			p.WaitJS(`(` + toastsJS + `).includes("Chat renamed")`)

			pickMore(t, p, c, "Fork Chat from Message…", false)
			p.WaitJS(moreItem("second question") + ` && ` + moreItem("first question"))
			p.TapJS(moreItem("second question"))
			reply(t, c, expectOp(t, c, "chat.fork", `{"i":2}`), map[string]any{"id": "c-2"})

			pickMore(t, p, c, "Export Chat", false)
			reply(t, c, expectOp(t, c, "chat.export", `{"id":"c-1"}`), webproto.ChatExport{Name: "ai-chat-2026-11-06.md", Text: "# Fix the build\n"})
			p.WaitJS(`(` + toastsJS + `).includes("Saved ai-chat-2026-11-06.md")`)

			// Delete asks here first (Cancel first and focused), then the laptop.
			pickMore(t, p, c, "Delete Chat…", false)
			p.WaitJS(`document.querySelector(".ask") && document.activeElement === document.querySelector(".ask .choices button")`)
			p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
			p.TapJS(ButtonByText("Ask the laptop"))
			reply(t, c, expectOp(t, c, "chat.delete", `{"id":"c-1"}`), webproto.AskRef{Card: "card-1"})
			p.WaitJS(`document.querySelector(".waiting")`)
		})
	}
}

func TestChatMoreLaptopCommandsAndScreens(t *testing.T) {
	p, c := openChat(t, Desktop, chatWith(false, userMsg(0, "hi")))

	// A chat setting asks first; the harmless MCP command runs at once.
	pickMore(t, p, c, "AI: Toggle Plan Approval", true)
	p.WaitJS(`document.querySelector(".ask") && document.activeElement === document.querySelector(".ask .choices button")`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	reply(t, c, expectOp(t, c, "palette.ask", `{"id":"ai-plan-approval"}`), webproto.AskRef{Card: "card-2"})
	p.WaitJS(`document.querySelector(".waiting")`)
	p.TapJS(`document.querySelector(".waiting button")`)
	reply(t, c, expectOp(t, c, "cards.answer", ""), nil)

	pickMore(t, p, c, "AI: MCP: Reconnect All Servers", false)
	reply(t, c, expectOp(t, c, "palette.run", `{"id":"ai-mcp-reconnect-all"}`), nil)

	// A laptop screen reads here: rows, text, and Copy when it offers it.
	pickMore(t, p, c, "AI: Usage", false)
	reply(t, c, expectOp(t, c, "ai.info", `{"kind":"usage"}`), webproto.AIInfo{Title: "AI Usage", Note: "Tokens and cost", Rows: []webproto.AIInfoRow{{Title: "By model", Header: true}, {Title: "claude-test", Hint: "$0.12"}},
		Text: "model   cost\nclaude  0.12"})
	p.WaitJS(`document.querySelector(".ai-info pre")?.textContent.includes("claude  0.12") && document.querySelector(".ai-info .ai-info-row")?.textContent.includes("claude-test")`)
	p.TapJS(ButtonByText("Close"))
	p.WaitJS(`!document.querySelector(".ai-info")`)

	pickMore(t, p, c, "MCP: termocode Server Config", false)
	reply(t, c, expectOp(t, c, "ai.info", `{"kind":"serve-config"}`), webproto.AIInfo{Title: "termocode Server Config", Rows: []webproto.AIInfoRow{}, Text: `{"mcpServers":{}}`, Copy: true})
	p.WaitJS(ButtonByText("Copy"))
	p.TapJS(ButtonByText("Copy"))
	p.WaitJS(`(` + toastsJS + `).includes("Copied")`)
}
