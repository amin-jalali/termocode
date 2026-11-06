package app

import (
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The Itermoc view of the web (WEBP-ITERMOC): the laptop's Itermoc
// sidebar view for the page. itermoc.state is the view's own snapshot
// (itermocBuild), which holds no secrets: a model slot carries only
// whether its key is there. itermoc.ask opens one of the laptop's setup
// forms (Add AI, a model slot, the profiles) after a laptop card; the
// user fills it on the laptop, so a key is never typed on the web.

func init() {
	webOps[webproto.OpItermoc] = (*Model).webItermoc
	webAskOps[webproto.OpItermocAsk] = (*Model).webItermocAsk
}

// webItermocDispatch runs a laptop palette command (a seam for tests).
var webItermocDispatch = (*Model).dispatchPaletteAction

// webItermocKeyState says what a slot's key state is on the page.
func webItermocKeyState(k imKeyState) string {
	switch k {
	case imKeyOK:
		return "ok"
	case imKeyNone:
		return "none"
	case imKeyUnset:
		return "unset"
	}
	return "missing"
}

// webItermocText caps a line of the view.
func webItermocText(s string) string {
	s = webName(s)
	if len(s) > webproto.MaxItermocText {
		s = s[:webproto.MaxItermocText]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1] // not inside a character
		}
	}
	return s
}

func (m *Model) webItermoc(call *webCall) tea.Cmd {
	sn := itermocBuild(*m, false)
	out := webproto.Itermoc{Profiles: []webproto.ItermocProfile{}, Slots: []webproto.ItermocSlot{}, Providers: []webproto.ItermocProvider{},
		Services: []webproto.ItermocService{}, Agents: []webproto.ItermocAgent{}, Listening: sn.listening, Problems: len(sn.problems)}
	for _, p := range sn.profiles {
		if len(out.Profiles) == webproto.MaxItermocRows {
			break
		}
		src := p.p.Source
		if src != ai.ProfileSourceUser && src != ai.ProfileSourceProject {
			src = ai.ProfileSourceBuiltin
		}
		slot := ""
		if len(p.p.Steps) > 0 {
			slot = p.p.Steps[0].Slot
		}
		out.Profiles = append(out.Profiles, webproto.ItermocProfile{Name: webItermocText(p.p.Name), Title: webItermocText(p.p.Title), Source: src,
			Trust: p.trust, Active: p.active, Steps: len(p.p.Steps), Model: webItermocText(slot)})
	}
	for _, s := range sn.slots {
		if len(out.Slots) == webproto.MaxItermocRows {
			break
		}
		out.Slots = append(out.Slots, webproto.ItermocSlot{Name: webItermocText(s.name), Provider: webItermocText(s.provider), Model: webItermocText(s.model),
			HasKey: webItermocKeyState(s.key), User: s.user})
	}
	for _, p := range sn.providers {
		if len(out.Providers) == webproto.MaxItermocRows {
			break
		}
		out.Providers = append(out.Providers, webproto.ItermocProvider{Kind: webItermocText(p.kind), Name: webItermocText(p.name), Label: webItermocText(p.label),
			Cost: webItermocText(p.cost), Models: len(p.models), SignedOut: p.signedOut})
	}
	for _, s := range sn.services {
		if len(out.Services) == webproto.MaxItermocRows {
			break
		}
		out.Services = append(out.Services, webproto.ItermocService{Name: webItermocText(s.Name), State: webItermocText(s.State), Tools: s.Tools})
	}
	for _, a := range sn.agents {
		if len(out.Agents) == webproto.MaxItermocRows {
			break
		}
		out.Agents = append(out.Agents, webproto.ItermocAgent{Name: webItermocText(a.name), Since: webItermocText(a.since), Calls: a.calls})
	}
	call.reply(out)
	return nil
}

// webItermocAsk asks the laptop to open one of its setup forms.
func (m *Model) webItermocAsk(call *webCall) *webAsk {
	id := call.args.(*webproto.ItermocAskArgs).Action
	title := ""
	for _, it := range paletteItems() {
		if it.ID == id {
			title = it.Title
			break
		}
	}
	if title == "" {
		call.fail(webproto.CodeNotFound, "The laptop has no such command.")
		return nil
	}
	return &webAsk{
		summary: id,
		title:   "Open " + title + "?",
		body:    "The web asks to open “" + title + "” on the laptop. You fill it in here; keys are typed only on the laptop.",
		action:  "Open",
		run:     func(m *Model) tea.Cmd { return webItermocDispatch(m, id) },
	}
}
