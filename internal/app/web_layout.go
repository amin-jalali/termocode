package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/layout"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The laptop's saved layouts from the web (WEBP-7): Layout: Open…, Save
// As…, Delete… and Reset to Default on the page, through the laptop's
// own layout store and code. Deleting a saved file waits for a laptop
// card, as the laptop's own Delete card asks first.

func init() {
	webOps[webproto.OpLayoutList] = (*Model).webLayoutList
	webOps[webproto.OpLayoutOpen] = (*Model).webLayoutOpen
	webOps[webproto.OpLayoutSave] = (*Model).webLayoutSave
	webOps[webproto.OpLayoutReset] = (*Model).webLayoutReset
	webAskOps[webproto.OpLayoutDelete] = (*Model).webLayoutDelete
}

func (m *Model) webLayoutList(call *webCall) tea.Cmd {
	entries, _ := m.layoutStore().List()
	out := webproto.LayoutList{Items: []webproto.LayoutEntry{}}
	for _, e := range entries {
		preview := e.Preview()
		if preview == nil {
			preview = []string{}
		}
		out.Items = append(out.Items, webproto.LayoutEntry{Name: e.Name, Source: e.Source.String(), Description: webText(e.Description), Preview: preview})
	}
	call.reply(out)
	return nil
}

func (m *Model) webLayoutOpen(call *webCall) tea.Cmd {
	name := call.args.(*webproto.LayoutNameArgs).Name
	e, ok := m.layoutStore().Find(name)
	if !ok {
		call.fail(webproto.CodeNotFound, "The laptop has no layout of that name.")
		return nil
	}
	cmd := m.openLayout(e.Layout)
	call.reply(webproto.LayoutOpened{Name: e.Name})
	return cmd
}

func (m *Model) webLayoutReset(call *webCall) tea.Cmd {
	cmd := m.resetLayout()
	call.reply(webproto.LayoutOpened{Name: "default"})
	return cmd
}

func (m *Model) webLayoutSave(call *webCall) tea.Cmd {
	a := call.args.(*webproto.LayoutSaveArgs)
	if err := layout.ValidName(a.Name); err != nil {
		call.fail(webproto.CodeBadRequest, "A layout name "+err.Error()+".")
		return nil
	}
	if layout.IsBuiltin(a.Name) {
		call.fail(webproto.CodeBadRequest, a.Name+" is a built-in layout; pick another name.")
		return nil
	}
	src := layout.User
	if a.Scope == "project" {
		src = layout.Project
	}
	st := m.layoutStore()
	if st.Has(src, a.Name) && !a.Replace {
		call.reply(webproto.LayoutSaved{Exists: true})
		return nil
	}
	if _, err := st.Save(src, m.captureLayout(a.Name)); err != nil {
		call.fail(webproto.CodeInternal, "The layout was not saved.")
		return nil
	}
	call.reply(webproto.LayoutSaved{Saved: true})
	return nil
}

func (m *Model) webLayoutDelete(call *webCall) *webAsk {
	name := call.args.(*webproto.LayoutNameArgs).Name
	e, ok := m.layoutStore().Find(name)
	if !ok {
		call.fail(webproto.CodeNotFound, "The laptop has no layout of that name.")
		return nil
	}
	if e.Source == layout.Builtin {
		call.fail(webproto.CodeForbidden, "A built-in layout cannot be deleted.")
		return nil
	}
	body := "Delete the " + e.Source.String() + " layout " + e.Name + "? The file is removed and cannot be restored here."
	if e.Source == layout.Project {
		body += " It is the project's file .termocode/layouts/" + e.Name + layout.Ext + "; others who share it lose it once the removal is committed."
	}
	return &webAsk{
		summary: e.Name,
		title:   "Delete layout " + e.Name + "?",
		body:    body,
		action:  "Delete",
		run: func(m *Model) tea.Cmd {
			m.layoutCardName = e.Name
			return m.answerLayoutDelete("delete")
		},
	}
}
