package app

import (
	"fmt"
	"reflect"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ext"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The laptop's Lua extension panels and status items on the web
// (WEBP-7). The status topic carries the panels and the status items'
// text (the heartbeat keeps them fresh); ext.panel renders one panel for
// the page's side bar, off the UI goroutine with the same time limit as
// the laptop's poll. A row picked and a status item clicked run the
// extension's own handler, exactly as a click on the laptop does.

// webExtWidth and webExtRows are the size a panel renders at for the
// page's side bar.
const (
	webExtWidth = 60
	webExtRows  = 200
)

func init() {
	webOps[webproto.OpExtPanel] = (*Model).webExtPanel
	webOps[webproto.OpExtSelect] = (*Model).webExtSelect
	webOps[webproto.OpExtStatusRun] = (*Model).webExtStatusRun
}

// webExtStatus is the status topic's ext part (nil: no panels, no items).
func (m *Model) webExtStatus() *webproto.StatusExt {
	if m.ext == nil || len(m.ext.reg.Panels) == 0 && len(m.ext.reg.Status) == 0 {
		return nil
	}
	out := &webproto.StatusExt{Panels: []webproto.StatusExtPanel{}, Items: []webproto.StatusExtItem{}}
	for _, p := range m.ext.reg.Panels {
		out.Panels = append(out.Panels, webproto.StatusExtPanel{ID: p.ID, Title: webText(p.Title), Ext: p.Ext})
	}
	for _, s := range m.ext.reg.Status {
		out.Items = append(out.Items, webproto.StatusExtItem{ID: s.ID, Text: webText(ext.PlainText(m.ext.status[s.ID])), Run: s.Command != ""})
	}
	return out
}

// webExtSame compares the ext parts of two status values.
func webExtSame(a, b *webproto.StatusExt) bool { return reflect.DeepEqual(a, b) }

// webExtFind is the panel of id.
func (m *Model) webExtFind(id string) (ext.Panel, bool) {
	if m.ext == nil {
		return ext.Panel{}, false
	}
	for _, p := range m.ext.reg.Panels {
		if p.ID == id {
			return p, true
		}
	}
	return ext.Panel{}, false
}

// webExtSpans turns markup lines into styled spans.
func webExtSpans(lines []string) [][]webproto.ExtSpan {
	out := make([][]webproto.ExtSpan, 0, len(lines))
	for _, l := range lines {
		row := []webproto.ExtSpan{}
		for _, s := range ext.ParseMarkup(l) {
			row = append(row, webproto.ExtSpan{T: webText(s.Text), S: strings.Join(s.Styles, " ")})
		}
		out = append(out, row)
	}
	return out
}

func (m *Model) webExtPanel(call *webCall) tea.Cmd {
	id := call.args.(*webproto.ExtIDArgs).ID
	p, ok := m.webExtFind(id)
	if !ok {
		call.fail(webproto.CodeNotFound, "The laptop has no extension panel of that name.")
		return nil
	}
	if m.ext.procPanel[p.ID] != "" { // a process extension sends its lines itself
		m.ext.webLines[p.ID] = m.ext.lines[p.ID]
		call.reply(webproto.ExtPanelView{Lines: webExtSpans(m.ext.lines[p.ID])})
		return nil
	}
	if m.nvim == nil || !m.ext.loaded {
		call.fail(webproto.CodeBusy, "The laptop's extensions are not loaded yet.")
		return nil
	}
	c := m.nvim
	code := fmt.Sprintf("local H = _G._termocode_ext; if not H then return '' end; return H.poll(%s, %d, %d)", luaQuote(p.ID), webExtWidth, webExtRows)
	return func() tea.Msg {
		out, err := extEval(c, code, extCallTimeout)
		var poll ext.Poll
		if err == nil {
			poll, err = ext.ParsePoll(out)
		}
		if err != nil {
			recordError("[ext] web panel " + p.ID + ": " + err.Error())
			call.fail(webproto.CodeInternal, "The extension panel did not render.")
			return nil
		}
		return applyMsg(func(m *Model) tea.Cmd {
			m.ext.webLines[p.ID] = poll.Lines
			call.reply(webproto.ExtPanelView{Lines: webExtSpans(poll.Lines)})
			return nil
		})
	}
}

func (m *Model) webExtSelect(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ExtSelectArgs)
	p, ok := m.webExtFind(a.ID)
	if !ok {
		call.fail(webproto.CodeNotFound, "The laptop has no extension panel of that name.")
		return nil
	}
	lines := m.ext.webLines[p.ID]
	if a.Row >= len(lines) {
		call.fail(webproto.CodeNotFound, "That row is not in the panel any more.")
		return nil
	}
	text := ext.PlainText(lines[a.Row])
	call.reply(nil)
	if name := m.ext.procPanel[p.ID]; name != "" {
		m.extProcSelect(name, p.ID, a.Row+1, text)
		return nil
	}
	return m.extCall("select "+p.ID, fmt.Sprintf("H.select(%s, %d, %s)", luaQuote(p.ID), a.Row+1, luaQuote(text)))
}

func (m *Model) webExtStatusRun(call *webCall) tea.Cmd {
	id := call.args.(*webproto.ExtIDArgs).ID
	if m.ext != nil {
		for _, s := range m.ext.reg.Status {
			if s.ID != id {
				continue
			}
			if s.Command == "" {
				call.fail(webproto.CodeConflict, "This status item runs nothing.")
				return nil
			}
			call.reply(nil)
			return m.runExtCommand(s.Command)
		}
	}
	call.fail(webproto.CodeNotFound, "The laptop has no status item of that name.")
	return nil
}
