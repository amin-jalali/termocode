package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/picker"
	"termocode/internal/prompt"
	"termocode/internal/theme"
	"termocode/internal/toast"
)

// openThemeEditor pops a picker listing every palette key with its
// current hex value. Selecting a row opens a prompt for the new hex.
// Persistence is to ~/.config/termocode/user_theme.json.
func (m *Model) openThemeEditor() tea.Cmd {
	overrides := theme.LoadUserOverrides()
	items := make([]picker.Item, 0, len(theme.PaletteKeys))
	for _, k := range theme.PaletteKeys {
		hint := overrides[k]
		if hint == "" {
			// No user override yet — show the live default so the user
			// sees what they'd be replacing.
			hint = theme.CurrentPaletteHex(k)
		}
		items = append(items, picker.Item{
			ID:    "theme-key-" + k,
			Title: k,
			Hint:  hint,
		})
	}
	m.picker = picker.NewItems(" Custom Theme ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindThemeEditor
	return m.picker.Init()
}

// onThemeEditorRowSelected opens a prompt for a new hex value for the
// selected palette key.
func (m *Model) onThemeEditorRowSelected(id string) tea.Cmd {
	const prefix = "theme-key-"
	if len(id) <= len(prefix) || id[:len(prefix)] != prefix {
		return nil
	}
	field := id[len(prefix):]
	overrides := theme.LoadUserOverrides()
	current := overrides[field]
	if current == "" {
		current = theme.CurrentPaletteHex(field)
	}
	m.prompt = prompt.New("Custom Theme", field+" (#rrggbb):", current)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindThemeColor
	m.themePromptKey = field
	return nil
}

// applyThemeColor is invoked from handlePromptSubmit when the user
// confirms a new hex for a palette key. Validates and persists, then
// re-applies the user theme so the change is visible immediately.
func (m *Model) applyThemeColor(value string) tea.Cmd {
	if m.themePromptKey == "" {
		return nil
	}
	var toastCmd tea.Cmd
	if !theme.IsHexColor(value) {
		m.toast, toastCmd = m.toast.Push(toast.Errr, "expected #rgb or #rrggbb")
		return toastCmd
	}
	entries := theme.LoadUserOverrides()
	if entries == nil {
		entries = map[string]string{}
	}
	entries[m.themePromptKey] = value
	if err := theme.SaveUserOverrides(entries); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Theme error", err.Error())
		return toastCmd
	}
	// Apply right away so the user sees the change without re-launching.
	m.applyTheme(theme.UserThemeID)
	m.toast, toastCmd = m.toast.PushDetail(toast.Info, m.themePromptKey, value)
	m.themePromptKey = ""
	return toastCmd
}
