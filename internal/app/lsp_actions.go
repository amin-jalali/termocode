package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/picker"
	"termocode/internal/toast"
)

// hoverCmd asks the LSP for hover docs at the cursor and shows them in the
// preview overlay (markdown-rendered when the body looks like markdown).
func (m *Model) hoverCmd() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	c := m.nvim
	return func() tea.Msg {
		body, err := c.Hover()
		if err != nil {
			return ErrMsg{Err: err}
		}
		if strings.TrimSpace(body) == "" {
			return hoverEmptyMsg{}
		}
		// If the response looks like markdown, render it; otherwise pass
		// through plain. Renderer width = full terminal width minus a
		// little padding for the preview chrome.
		rendered := body
		if looksMarkdown(body) {
			if r, err := renderMarkdown(body, 100); err == nil && r != "" {
				rendered = r
			}
		}
		return PreviewMsg{Title: " Hover ", Body: rendered}
	}
}

type hoverEmptyMsg struct{}

func looksMarkdown(s string) bool {
	return strings.Contains(s, "```") || strings.Contains(s, "**") || strings.Contains(s, "##")
}

// ReferencesPickerMsg carries fetched LSP references for the picker overlay.
type ReferencesPickerMsg struct {
	Items []picker.Item
	Index map[string]workspaceSymbolTarget // reuse the file/line/col triple
}

// fetchReferencesCmd asks the LSP for every reference to the symbol at the
// cursor and packages them into a picker.
func (m Model) fetchReferencesCmd() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	c := m.nvim
	cwd, _ := os.Getwd()
	return func() tea.Msg {
		locs, err := c.References()
		if err != nil {
			return ErrMsg{Err: err}
		}
		items := make([]picker.Item, 0, len(locs))
		index := make(map[string]workspaceSymbolTarget, len(locs))
		for i, loc := range locs {
			id := fmt.Sprintf("ref-%d", i)
			rel := loc.File
			if r, err := filepath.Rel(cwd, loc.File); err == nil && !strings.HasPrefix(r, "..") {
				rel = r
			}
			preview := strings.TrimSpace(loc.Preview)
			if len(preview) > 60 {
				preview = preview[:60] + "…"
			}
			items = append(items, picker.Item{
				ID:    id,
				Title: fmt.Sprintf("%s   %s", rel, preview),
				Hint:  fmt.Sprintf("L%d", loc.Line),
			})
			index[id] = workspaceSymbolTarget{File: loc.File, Line: loc.Line, Col: loc.Col}
		}
		return ReferencesPickerMsg{Items: items, Index: index}
	}
}

// gotoTypeDefCmd / gotoImplementationCmd — thin wrappers around the LSP
// goto methods. They run synchronously (the Lua chunk does the navigation)
// and surface a "no result" toast when the server has nothing to offer.
func (m *Model) gotoTypeDefCmd() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	ok, err := m.nvim.GotoTypeDefinition()
	return m.afterGoto(ok, err, "type definition")
}

func (m *Model) gotoImplementationCmd() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	ok, err := m.nvim.GotoImplementation()
	return m.afterGoto(ok, err, "implementation")
}

// afterGoto centralises the toast logic for the two LSP goto methods.
func (m *Model) afterGoto(ok bool, err error, label string) tea.Cmd {
	var toastCmd tea.Cmd
	switch {
	case err != nil:
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, label, err.Error())
	case !ok:
		m.toast, toastCmd = m.toast.Push(toast.Info, "No "+label+" found")
	default:
		m.focus = FocusEditor
	}
	return toastCmd
}

// applyReferencesPickerMsg installs the picker. Empty list ⇒ toast.
func (m *Model) applyReferencesPickerMsg(msg ReferencesPickerMsg) tea.Cmd {
	if len(msg.Items) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No references found")
		return toastCmd
	}
	m.workspaceSymIndex = msg.Index // jumpToWorkspaceSymbol already does the right thing
	m.picker = picker.NewItems(" References ", msg.Items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindWorkspaceSymbols
	return nil
}
