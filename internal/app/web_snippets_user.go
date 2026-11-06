package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The user's own snippets from the web (WEBP-9; the laptop's Snippets:
// Manage…): list, save, delete, in the laptop's snippets.json, which the
// laptop's editor reads again after each change.

func init() {
	webOps[webproto.OpSnippetsUser] = webOpSnippetsUser
	webOps[webproto.OpSnippetsSave] = webOpSnippetsSave
	webOps[webproto.OpSnippetsDelete] = webOpSnippetsDelete
}

func webOpSnippetsUser(m *Model, call *webCall) tea.Cmd {
	snips, err := loadUserSnippets()
	if err != nil {
		call.fail(webproto.CodeInternal, "snippets.json on the laptop is not valid JSON.")
		return nil
	}
	out := webproto.UserSnippetList{Items: []webproto.UserSnippet{}}
	for _, s := range snips.list("") {
		out.Items = append(out.Items, webproto.UserSnippet{Scope: s.Scope, Trigger: s.Trigger, Body: s.Body})
	}
	call.reply(out)
	return nil
}

func webOpSnippetsSave(m *Model, call *webCall) tea.Cmd {
	a := call.args.(*webproto.SnippetSaveArgs)
	if !validSnippetTrigger(a.Trigger) {
		call.fail(webproto.CodeBadRequest, "A trigger is letters, digits or _ only.")
		return nil
	}
	snips, err := loadUserSnippets()
	if err != nil {
		call.fail(webproto.CodeInternal, "snippets.json on the laptop is not valid JSON.")
		return nil
	}
	replaced := snips.set(a.Scope, a.Trigger, a.Body)
	if err := saveUserSnippets(snips); err != nil {
		call.fail(webproto.CodeInternal, "The snippet was not saved.")
		return nil
	}
	m.reloadUserSnippets()
	call.reply(webproto.SnippetSaved{Replaced: replaced})
	return nil
}

func webOpSnippetsDelete(m *Model, call *webCall) tea.Cmd {
	a := call.args.(*webproto.SnippetRefArgs)
	snips, err := loadUserSnippets()
	if err != nil {
		call.fail(webproto.CodeInternal, "snippets.json on the laptop is not valid JSON.")
		return nil
	}
	if !snips.remove(snippetRef{Scope: a.Scope, Trigger: a.Trigger}) {
		call.fail(webproto.CodeNotFound, "That snippet is not there any more.")
		return nil
	}
	if err := saveUserSnippets(snips); err != nil {
		call.fail(webproto.CodeInternal, "The snippet was not deleted.")
		return nil
	}
	m.reloadUserSnippets()
	call.reply(webproto.SnippetSaved{})
	return nil
}
