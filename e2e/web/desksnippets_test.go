//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Snippets: Manage… (WEBP-9, Editor and files): the user's own snippets
// in a picker; + New Snippet… asks a name and a body (\n is a new line)
// and saves it; a snippet picked is deleted after a question here
// (Cancel first).
func TestDeskManageSnippets(t *testing.T) {
	p, c := openFiles(t, Desktop)
	runCmd(t, p, c, true, "snippets manage", "Snippets: Manage…")
	r := waitReq(t, c, webproto.OpSnippetsUser)
	reply(t, c, r, webproto.UserSnippetList{Items: []webproto.UserSnippet{{Scope: "*", Trigger: "todo", Body: "// TODO"}}})
	p.WaitJS(`[...document.querySelectorAll(".quick-row .quick-label")].map((e) => e.textContent).join() === "+ New Snippet…,todo"`)
	pickRow(p, "+ New Snippet…")
	p.WaitJS(`document.querySelector(".quick-box")?.placeholder === "Name: scope:trigger (go:iferr), or a trigger for every file type" && document.activeElement === document.querySelector(".quick-box")`)
	p.Key("go:iferr")
	p.Key("\r")
	p.WaitJS(`document.querySelector(".quick-box")?.placeholder === "Body of go:iferr (\\n is a new line, \\t a tab, $1 a stop)" && document.activeElement === document.querySelector(".quick-box")`)
	p.Key(`if err != nil {\n\treturn $1\n}`)
	p.Key("\r")
	r = waitArgs(t, c, webproto.OpSnippetsSave, `{"scope":"go","trigger":"iferr","body":"if err != nil {\n\treturn $1\n}"}`)
	reply(t, c, r, webproto.SnippetSaved{})
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Snippet go:iferr saved.")`)

	runCmd(t, p, c, false, "snippets manage", "Snippets: Manage…")
	r = waitReq(t, c, webproto.OpSnippetsUser)
	reply(t, c, r, webproto.UserSnippetList{Items: []webproto.UserSnippet{{Scope: "*", Trigger: "todo", Body: "// TODO"}}})
	p.WaitJS(`document.querySelector(".quick-row .quick-label")`)
	p.Key("todo")
	pickRow(p, "todo")
	p.WaitJS(`document.activeElement === document.querySelector(".ask-here .choices button")`)
	p.WaitJS(`document.querySelector(".ask-here .btn-danger.is-armed")`)
	p.TapJS(`document.querySelector(".ask-here .btn-danger")`)
	r = waitArgs(t, c, webproto.OpSnippetsDelete, `{"scope":"*","trigger":"todo"}`)
	reply(t, c, r, nil)
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Snippet todo deleted.")`)
}
