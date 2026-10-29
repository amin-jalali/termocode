package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/layout"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Layout: Delete… (G-LAY): only saved layouts are listed; the built-in
// ones stay. The card asks first, with Cancel first and focused (AH-1).

const (
	pickerKindLayoutDelete  pickerKindEnum  = 3301
	confirmKindLayoutDelete confirmKindEnum = 3303
)

// openLayoutDelete is Layout: Delete…: the saved layouts, or a toast.
func (m *Model) openLayoutDelete() tea.Cmd {
	items, broken := m.layoutPickerItems(func(e layout.Entry) bool { return e.Source != layout.Builtin })
	if len(items) == 0 {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Info, "No saved layouts to delete (the built-in ones stay)")
		return tea.Batch(c, m.layoutBrokenToast(broken))
	}
	m.picker = picker.NewItems(" Layout: Delete ", items)
	m.picker.SetWidth(84)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindLayoutDelete
	return m.layoutBrokenToast(broken)
}

// onLayoutDeletePicked asks before deleting the picked layout.
func (m *Model) onLayoutDeletePicked(name string) tea.Cmd {
	e, ok := m.layoutStore().Find(name)
	if !ok || e.Source == layout.Builtin {
		return nil
	}
	body := "The file is removed and cannot be restored here."
	if e.Source == layout.Project {
		body = "It is the project's file .termocode/layouts/" + e.Name + layout.Ext + ". " +
			"Others who share it lose it too once you commit the removal."
	}
	m.confirm = confirm.Risky("Delete layout "+e.Name+"?", body,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "delete", Title: "Delete", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindLayoutDelete
	m.layoutCardName = name
	return nil
}

// answerLayoutDelete is the delete card (Cancel and Esc keep the file).
func (m *Model) answerLayoutDelete(id string) tea.Cmd {
	name := m.layoutCardName
	m.layoutCardName = ""
	if id != "delete" {
		return nil
	}
	var c tea.Cmd
	e, ok := m.layoutStore().Find(name)
	if !ok {
		return nil
	}
	if err := m.layoutStore().Delete(e); err != nil {
		m.toast, c = m.toast.Push(toast.Warn, "Layout not deleted: "+strings.TrimSpace(err.Error()))
		return c
	}
	m.toast, c = m.toast.Push(toast.Info, "Layout "+name+" deleted")
	return c
}
