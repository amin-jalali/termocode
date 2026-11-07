package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/picker"
)

// CTX-39: "Kubernetes: New Manifest from Snippet…" opens a new YAML
// buffer with a manifest (name my-app, the view's namespace). Nothing is
// applied; the user edits and saves it.

// kubeSnippetItems are the picker rows ("k8s:snippet:<id>").
func kubeSnippetItems() []picker.Item {
	var out []picker.Item
	for _, s := range kube.Snippets() {
		out = append(out, picker.Item{ID: "k8s:snippet:" + s.ID, Title: s.Title, Hint: s.Kind})
	}
	return out
}

// kubeSnippetLines is the snippet's text as buffer lines.
func kubeSnippetLines(id, namespace string) ([]string, bool) {
	text, ok := kube.Render(id, "", namespace)
	if !ok {
		return nil, false
	}
	return strings.Split(strings.TrimRight(text, "\n"), "\n"), true
}

// kubeOpenSnippet opens the snippet in a new unnamed yaml buffer.
func (m *Model) kubeOpenSnippet(id string) tea.Cmd {
	ns := ""
	if m.kube != nil {
		ns = m.kube.namespace
	}
	lines, ok := kubeSnippetLines(id, ns)
	if !ok || m.nvim == nil {
		return nil
	}
	m.ensureEditorWindowCurrent()
	_ = m.nvim.ExecLuaArgs(`local l=...
vim.cmd('enew')
vim.api.nvim_buf_set_lines(0,0,-1,false,l)
vim.bo.filetype='yaml'
pcall(vim.api.nvim_win_set_cursor,0,{1,0})
vim.cmd('stopinsert')`, lines)
	m.focus = FocusEditor
	return m.fetchStateCmd()
}
