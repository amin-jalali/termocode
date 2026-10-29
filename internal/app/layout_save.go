package app

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/layout"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Layout: Save As… (G-LAY): a name, then where (only me, or this project),
// then a warning when that file exists. The layout is read when the file is
// written, so what is saved is what is on screen at the end.

const (
	promptKindLayoutSave     promptKindEnum  = 3300
	confirmKindLayoutScope   confirmKindEnum = 3301
	confirmKindLayoutReplace confirmKindEnum = 3302
)

// openLayoutSave is Layout: Save As…: the name prompt.
func (m *Model) openLayoutSave() tea.Cmd {
	m.prompt = prompt.New("Layout: Save As", "Name for this layout:", "")
	m.prompt.SetNote("Saves the open panels, sizes, splits, terminals and files.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindLayoutSave
	return nil
}

// handleLayoutPrompt routes the layout prompts. ok=false: not ours.
func (m *Model) handleLayoutPrompt(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindLayoutSave {
		return nil, false
	}
	return m.onLayoutSaveName(strings.TrimSpace(value)), true
}

// layoutRefuse keeps the name prompt open with a message under the input.
func (m *Model) layoutRefuse(name, why string) {
	m.prompt = prompt.New("Layout: Save As", "Name for this layout:", name)
	m.prompt.SetError(why)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindLayoutSave
}

// onLayoutSaveName checks the name and asks where to save.
func (m *Model) onLayoutSaveName(name string) tea.Cmd {
	if err := layout.ValidName(name); err != nil {
		m.layoutRefuse(name, err.Error())
		return nil
	}
	if layout.IsBuiltin(name) {
		m.layoutRefuse(name, name+" is a built-in layout; pick another name")
		return nil
	}
	m.layoutCardName = name
	m.confirm = confirm.New("Save layout "+name, "Who gets this layout?\n\n"+
		"Only me: every project of yours.\nThis project: the file .termocode/layouts/"+name+layout.Ext+", to share with the team.",
		[]confirm.Button{
			{ID: "user", Title: "Only me"},
			{ID: "project", Title: "This project"},
			{ID: "cancel", Title: "Cancel"},
		})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindLayoutScope
	return nil
}

var layoutScopes = map[string]layout.Source{"user": layout.User, "project": layout.Project}

// answerLayoutScope is the "who gets it" card ("" or cancel: nothing saved).
func (m *Model) answerLayoutScope(id string) tea.Cmd {
	src, ok := layoutScopes[id]
	name := m.layoutCardName
	if !ok {
		m.layoutCardName = ""
		return nil
	}
	m.layoutSaveSrc = src
	if m.layoutStore().Has(src, name) {
		m.confirm = confirm.Risky("Replace layout "+name+"?", "A "+src.String()+" layout with this name exists. It is replaced.",
			confirm.Button{ID: "cancel", Title: "Cancel"},
			confirm.Button{ID: "replace", Title: "Replace", Style: confirm.StyleDestructive})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindLayoutReplace
		return nil
	}
	return m.writeLayout(name, src)
}

// answerLayoutReplace is the replace card (Cancel and Esc keep the file).
func (m *Model) answerLayoutReplace(id string) tea.Cmd {
	name := m.layoutCardName
	if id != "replace" {
		m.layoutCardName = ""
		return nil
	}
	return m.writeLayout(name, m.layoutSaveSrc)
}

// writeLayout saves the live layout as name.
func (m *Model) writeLayout(name string, src layout.Source) tea.Cmd {
	m.layoutCardName = ""
	l := m.captureLayout(name)
	_, err := m.layoutStore().Save(src, l)
	var c tea.Cmd
	if err != nil {
		m.toast, c = m.toast.Push(toast.Warn, "Layout not saved: "+err.Error())
		return c
	}
	where := "for all your projects"
	if src == layout.Project {
		where = "in " + filepath.ToSlash(filepath.Join(".termocode", "layouts", name+layout.Ext))
	}
	m.toast, c = m.toast.Push(toast.Info, "Layout "+name+" saved "+where)
	return c
}
