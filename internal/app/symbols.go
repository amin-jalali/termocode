package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/nvim"
	"termocode/internal/picker"
	"termocode/internal/toast"
)

// SymbolPickerMsg carries the result of a fetchSymbolsCmd run. The picker is
// rebuilt from this each time the user opens "Go to Symbol in File" so the
// list always reflects the current LSP state of the active buffer.
type SymbolPickerMsg struct {
	Items []picker.Item
	// Index maps picker IDs back to (line, col) so handlePickerSelect can
	// jump without re-traversing the symbol tree.
	Index map[string][2]int
}

// fetchSymbolsCmd asks the active buffer's LSP for document symbols and
// flattens the hierarchical response into a single picker list. Children are
// indented in the title with "·" so the user can read scope at a glance
// without losing fuzzy-match coverage on names.
func (m Model) fetchSymbolsCmd() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	c := m.nvim
	return func() tea.Msg {
		syms, err := c.DocumentSymbols()
		if err != nil {
			if nvim.IsNoLSP(err) {
				return SymbolPickerMsg{}
			}
			return ErrMsg{Err: err}
		}
		items := make([]picker.Item, 0, 32)
		index := make(map[string][2]int, 32)
		var walk func(syms []nvim.DocSymbol, depth int)
		walk = func(syms []nvim.DocSymbol, depth int) {
			for i, s := range syms {
				id := fmt.Sprintf("s-%d-%d", depth, len(items)+i)
				prefix := strings.Repeat("·  ", depth)
				items = append(items, picker.Item{
					ID:    id,
					Title: prefix + symbolKindGlyph(s.Kind) + "  " + s.Name,
					Hint:  fmt.Sprintf("%s  L%d", symbolKindLabel(s.Kind), s.Line),
				})
				index[id] = [2]int{s.Line, s.Col}
				if len(s.Children) > 0 {
					walk(s.Children, depth+1)
				}
			}
		}
		walk(syms, 0)
		return SymbolPickerMsg{Items: items, Index: index}
	}
}

// applySymbolPickerMsg installs the freshly-built picker. Empty list ⇒ toast.
func (m *Model) applySymbolPickerMsg(msg SymbolPickerMsg) tea.Cmd {
	if len(msg.Items) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No symbols (LSP not attached, or empty file)")
		return toastCmd
	}
	m.symbolsIndex = msg.Index
	m.picker = picker.NewItems(" Go to Symbol in File ", msg.Items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindSymbols
	return nil
}

// jumpToSymbol moves the editor cursor to the line/col captured when the
// picker was opened. Closes any sidebar focus so the editor is ready for
// typing.
func (m *Model) jumpToSymbol(id string) {
	pos, ok := m.symbolsIndex[id]
	if !ok || m.nvim == nil {
		return
	}
	_ = m.nvim.Command(fmt.Sprintf("call cursor(%d, %d)", pos[0], pos[1]))
	m.focus = FocusEditor
}

// WorkspaceSymbolPickerMsg carries the result of a fetchWorkspaceSymbolsCmd run.
type WorkspaceSymbolPickerMsg struct {
	Items []picker.Item
	// Index maps picker IDs back to the absolute file path + (line, col)
	// triple so handlePickerSelect can :edit + :call cursor in one go.
	Index map[string]workspaceSymbolTarget
}

type workspaceSymbolTarget struct {
	File string
	Line int
	Col  int
}

// fetchWorkspaceSymbolsCmd queries LSP workspace/symbol with an empty query.
// The LSP servers return their best initial set of project-wide symbols,
// which the picker's fuzzy search then filters as the user types. We don't
// re-query on each keystroke because that's both slow (round-trip per
// keystroke) and unnecessary (fuzzy filtering on the client is fast enough
// for typical project sizes).
func (m Model) fetchWorkspaceSymbolsCmd() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	c := m.nvim
	return func() tea.Msg {
		syms, err := c.WorkspaceSymbols("")
		if err != nil {
			return ErrMsg{Err: err}
		}
		items := make([]picker.Item, 0, len(syms))
		index := make(map[string]workspaceSymbolTarget, len(syms))
		for i, s := range syms {
			id := fmt.Sprintf("ws-%d", i)
			title := symbolKindGlyph(s.Kind) + "  " + s.Name
			if s.Container != "" {
				title += "  ·  " + s.Container
			}
			hint := s.File
			if s.Line > 0 {
				hint = fmt.Sprintf("%s:%d", s.File, s.Line)
			}
			items = append(items, picker.Item{
				ID:    id,
				Title: title,
				Hint:  hint,
			})
			index[id] = workspaceSymbolTarget{
				File: s.File,
				Line: s.Line,
				Col:  s.Col,
			}
		}
		return WorkspaceSymbolPickerMsg{Items: items, Index: index}
	}
}

// applyWorkspaceSymbolPickerMsg installs the fetched workspace-symbol picker.
// Empty result ⇒ toast (LSP either has no project index yet, or the server
// doesn't implement workspace/symbol).
func (m *Model) applyWorkspaceSymbolPickerMsg(msg WorkspaceSymbolPickerMsg) tea.Cmd {
	if len(msg.Items) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No workspace symbols (LSP not ready, or unsupported)")
		return toastCmd
	}
	m.workspaceSymIndex = msg.Index
	m.picker = picker.NewItems(" Go to Symbol in Workspace ", msg.Items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindWorkspaceSymbols
	return nil
}

// jumpToWorkspaceSymbol opens the file at the symbol's location.
func (m *Model) jumpToWorkspaceSymbol(id string) {
	t, ok := m.workspaceSymIndex[id]
	if !ok || m.nvim == nil || t.File == "" {
		return
	}
	m.ensureEditorWindowCurrent()
	_ = m.nvim.Command(fmt.Sprintf("edit +%d %s", t.Line, t.File))
	if t.Col > 0 {
		_ = m.nvim.Command(fmt.Sprintf("call cursor(%d, %d)", t.Line, t.Col))
	}
	m.focus = FocusEditor
}

// symbolKindGlyph returns a single glyph for an LSP SymbolKind. Picked to
// visually echo VSCode's outline icons while staying terminal-safe (no
// material/codicon glyphs that need a Nerd Font).
func symbolKindGlyph(k int) string {
	switch k {
	case 5: // Class
		return "C"
	case 6, 9: // Method, Constructor
		return "M"
	case 12: // Function
		return "ƒ"
	case 11: // Interface
		return "I"
	case 23: // Struct
		return "S"
	case 10: // Enum
		return "E"
	case 13, 14: // Variable, Constant
		return "v"
	case 7, 8: // Property, Field
		return "•"
	case 2: // Module
		return "□"
	case 3: // Namespace
		return "□"
	case 22: // EnumMember
		return "·"
	}
	return "?"
}

func symbolKindLabel(k int) string {
	switch k {
	case 1:
		return "file"
	case 2:
		return "module"
	case 3:
		return "namespace"
	case 5:
		return "class"
	case 6:
		return "method"
	case 7:
		return "property"
	case 8:
		return "field"
	case 9:
		return "constructor"
	case 10:
		return "enum"
	case 11:
		return "interface"
	case 12:
		return "function"
	case 13:
		return "variable"
	case 14:
		return "constant"
	case 22:
		return "enumMember"
	case 23:
		return "struct"
	}
	return ""
}
