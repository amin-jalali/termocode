package app

import (
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/layout"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Layout: Open… / Save As… / Delete… / Reset to Default (G-LAY). The
// commands only the user runs: the AI changes the layout with layout_set.

const (
	pickerKindLayoutOpen  pickerKindEnum  = 3300
	confirmKindLayoutOpen confirmKindEnum = 3300
)

// layoutNoAI is why the AI has no palette access to the layout commands.
const layoutNoAI = "the AI changes the layout with the layout_set tool"

// layoutPaletteItems are the palette's layout commands.
func layoutPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "layout-open", Title: "Layout: Open…"},
	}
}

// dispatchLayoutPalette runs the layout palette ids.
func (m *Model) dispatchLayoutPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "layout-open":
		return m.openLayoutPicker(), true
	}
	return nil, false
}

// layoutStore is where this workspace reads and writes layout files.
func (m *Model) layoutStore() layout.Store {
	var s layout.Store
	if dir, err := configDir(); err == nil {
		s.UserDir = filepath.Join(dir, "layouts")
	}
	if root, err := m.rootDir(); err == nil && root != "" {
		s.ProjectDir = filepath.Join(root, ".termocode", "layouts")
	}
	return s
}

// layoutRowHint is the hint of a picker row: where the layout comes from
// and what it is for.
func layoutRowHint(e layout.Entry) string {
	what := e.Description
	if what == "" {
		what = strings.Join(e.Preview(), " · ")
	}
	return e.Source.String() + " · " + what
}

// layoutPickerItems lists the layouts for a picker; broken files are
// reported by the toast the caller pushes.
func (m *Model) layoutPickerItems(keep func(layout.Entry) bool) ([]picker.Item, []layout.Broken) {
	entries, broken := m.layoutStore().List()
	var items []picker.Item
	for _, e := range entries {
		if keep != nil && !keep(e) {
			continue
		}
		items = append(items, picker.Item{ID: e.Name, Title: e.Name, Hint: layoutRowHint(e)})
	}
	return items, broken
}

// layoutBrokenToast says that layout files could not be read (one toast).
func (m *Model) layoutBrokenToast(broken []layout.Broken) tea.Cmd {
	if len(broken) == 0 {
		return nil
	}
	b := broken[0]
	msg := filepath.Base(b.Path) + " is not a valid layout: " + b.Err.Error()
	if len(broken) > 1 {
		msg = filepath.Base(b.Path) + " and " + strconv.Itoa(len(broken)-1) + " more layout files are not valid"
	}
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Warn, msg)
	return c
}

// openLayoutPicker is Layout: Open…: every layout, built-in ones too.
// Enter opens it; → shows what it would change first.
func (m *Model) openLayoutPicker() tea.Cmd {
	items, broken := m.layoutPickerItems(nil)
	m.picker = picker.NewItems(" Layout: Open ", items)
	m.picker.SetWidth(84)
	m.picker.SetRowAction("→", "preview")
	m.picker.SetNote([]string{"Enter opens a layout. → shows what it changes."})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindLayoutOpen
	return m.layoutBrokenToast(broken)
}

// onLayoutOpenPicked opens the layout the user picked.
func (m *Model) onLayoutOpenPicked(name string) tea.Cmd {
	e, ok := m.layoutStore().Find(name)
	if !ok {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Warn, "Layout "+name+" is gone")
		return c
	}
	return m.openLayout(e.Layout)
}

// onLayoutPickerAction is → on a row of the Open picker: a card with the
// lines of what the layout does, then Open or Close.
func (m *Model) onLayoutPickerAction(name string) tea.Cmd {
	e, ok := m.layoutStore().Find(name)
	if !ok {
		return nil
	}
	body := strings.Join(e.Preview(), "\n")
	if e.Description != "" {
		body = e.Description + "\n\n" + body
	}
	m.confirm = confirm.New("Layout "+e.Name+" ("+e.Source.String()+")", body, []confirm.Button{
		{ID: "close", Title: "Close"},
		{ID: "open", Title: "Open"},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindLayoutOpen
	m.layoutCardName = name
	return nil
}

// answerLayoutOpenCard handles the preview card. Close (and Esc, "") goes
// back to the list with the row still selected.
func (m *Model) answerLayoutOpenCard(id string) tea.Cmd {
	name := m.layoutCardName
	m.layoutCardName = ""
	if id == "open" {
		return m.onLayoutOpenPicked(name)
	}
	cmd := m.openLayoutPicker()
	m.picker.SetCursorID(name)
	return cmd
}

// handleLayoutConfirm routes the layout cards. ok=false: not ours.
func (m *Model) handleLayoutConfirm(id string) (tea.Cmd, bool) {
	switch m.confirmKind {
	case confirmKindLayoutOpen:
		return m.answerLayoutOpenCard(id), true
	}
	return nil, false
}

// layoutConfirmClosed is Esc on a layout card; ok=false: not ours.
func (m *Model) layoutConfirmClosed() (tea.Cmd, bool) {
	switch m.confirmKind {
	case confirmKindLayoutOpen:
		return m.answerLayoutOpenCard(""), true
	}
	return nil, false
}
