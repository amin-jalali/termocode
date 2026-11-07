package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
)

// Auto model pick in the chat (CMP-25, ai/router.go): with ai_router on,
// a plain chat turn of the default profile runs on the slot the router
// picks, and the chat shows a line "⇢ Auto model: quick (…) — a short,
// simple message" (for the user only, not sent to the model). A slot
// that can not run leaves the turn on main and the line says so.

// aiRouteMark starts the chat line of an auto model pick.
const aiRouteMark = "⇢ Auto model: "

// confirmKindAIRouterOn is the card before the auto model pick is
// turned on.
const confirmKindAIRouterOn confirmKindEnum = 5125

// aiRouteLine reports whether a stored tool line is an auto pick line.
func aiRouteLine(text string) bool { return strings.HasPrefix(text, aiRouteMark) }

// aiRoutable reports whether the router may pick the model of a turn:
// the default profile on the main slot (a profile or step that names its
// own model keeps it).
func aiRoutable(prof ai.Profile, slot string) bool {
	return prof.Name == ai.ProfileDefault && (slot == "" || slot == ai.SlotMain)
}

// aiRouteTurn picks the slot of a plain chat turn. It returns the slot
// name to use (slot itself when the router is off or has nothing to
// say) and the chat line ("" when nothing was picked).
func (s *aiState) aiRouteTurn(prof ai.Profile, slot, text string) (string, string) {
	if !aiRoutable(prof, slot) || !s.settings.Router.On() {
		return slot, ""
	}
	in := ai.RouteInput{Text: text, Think: s.turnThink}
	for _, a := range s.attachments {
		if a.Image != "" {
			in.Images++
		} else {
			in.Attachments++
		}
	}
	c := s.settings.Router.Pick(in)
	if c.Slot == "" {
		return slot, ""
	}
	sl, err := s.resolveSlot(c.Slot)
	if err != nil {
		main := ai.SlotMain
		if m, e := s.resolveSlot(main); e == nil {
			main = aiSlotLabel(ai.SlotMain, m.cfg)
		}
		return slot, aiRouteMark + main + " — " + c.Slot + " can not run (" + ai.Redact(err.Error()) + ")"
	}
	return c.Slot, aiRouteMark + aiSlotLabel(c.Slot, sl.cfg) + " — " + c.Reason
}

// aiRouterSlotLine names a router slot for the card: "quick (gpt-5-mini)"
// or "quick (not set up: main is used)".
func (s *aiState) aiRouterSlotLine(name string) string {
	sl, err := s.resolveSlot(name)
	if err != nil {
		return name + " (not set up: main is used)"
	}
	return aiSlotLabel(name, sl.cfg)
}

// toggleAIRouter turns the auto model pick off at once, or shows the
// card that turns it on (Cancel first).
func (m *Model) toggleAIRouter() tea.Cmd {
	s := m.aiS()
	if s.settings.Router.On() {
		return m.setAIRouterMode(ai.RouterOff)
	}
	if err := ai.CheckRouter(s.settings.Router); err != nil {
		return m.aiToastErr("ai_router can not be used", err)
	}
	r := s.settings.Router
	msg := "Each plain chat turn of the Default profile picks its model on this machine (no extra request):\n" +
		"• short, simple messages: " + s.aiRouterSlotLine(r.QuickSlot()) + "\n" +
		"• hard ones (code, files, design, debugging, think): " + s.aiRouterSlotLine(r.DeepSlot()) + "\n"
	if n := len(r.Rules); n > 0 {
		msg += "• your " + itoa(n) + " ai_router rule" + plural(n) + " come first\n"
	}
	msg += "The chat shows each pick and why. Other profiles and runs keep their models."
	m.confirm = confirm.Risky("Turn on auto model pick?", msg,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "on", Title: "Turn On"})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIRouterOn
	return nil
}

// answerAIRouterOn is the answer of the turn-on card.
func (m *Model) answerAIRouterOn(id string) tea.Cmd {
	if id != "on" {
		return nil
	}
	return m.setAIRouterMode(ai.RouterAuto)
}

// setAIRouterMode saves ai_router.mode and uses it now.
func (m *Model) setAIRouterMode(mode string) tea.Cmd {
	if path, err := ai.ConfigPath(); err == nil {
		if err := ai.SaveRouterMode(path, mode); err != nil {
			return m.aiToastErr("Could not save the setting", err)
		}
	}
	m.aiS().settings.Router.Mode = mode
	if mode == ai.RouterAuto {
		return m.aiInfo("Auto model pick on: each chat turn shows the model it picked")
	}
	return m.aiInfo("Auto model pick off: the chat uses its own model")
}
