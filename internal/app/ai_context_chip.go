package app

import (
	"math"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// The context chip (CMP-7). After each turn the AI CHAT header shows how
// full the model's context window was on the newest turn of the open
// chat:
//
//	✦ AI CHAT  ✦ default ▾  ctx ▰▰▱▱▱ 42%  usage        + ≡ ⚐ ×
//
// It is green, amber from 70 % and red from 90 % (the chat summarises
// older messages from aiCompactAt). A click opens AI: Usage. The chip
// belongs to its chat: another chat shows none until it has a turn of
// its own. The ⋯ menu's Usage row names the same number, and the web
// chat shows it next to the composer (ChatSnap.CtxPct).

// aiCtxUse is the context use of the newest turn of one chat.
type aiCtxUse struct {
	conv   string // the chat it belongs to
	tokens int    // the biggest prompt of the turn (ai.Usage.PeakContext)
	window int    // the model's window in tokens
}

// Context levels in percent of the window.
const (
	aiContextWarnPct = 70
	aiContextFullPct = 90
)

// noteContext keeps u's prompt size as the open chat's context use. A
// turn with no prompt size or an unknown window changes nothing.
func (s *aiState) noteContext(u ai.Usage, window int) {
	if s == nil || u.PeakContext <= 0 {
		return
	}
	if window <= 0 {
		if s.ctxUse.conv == s.conv.ID {
			s.ctxUse = aiCtxUse{} // a model with no known window: no stale number
		}
		return
	}
	s.ctxUse = aiCtxUse{conv: s.conv.ID, tokens: u.PeakContext, window: window}
}

// noteContextOf is noteContext with the window of u's model (or model
// when u names none) from the settings.
func (s *aiState) noteContextOf(u ai.Usage, model string) {
	if s == nil {
		return
	}
	if u.Model != "" {
		model = u.Model
	}
	w, _ := ai.NewContextWindows(s.settings.ContextWindows).Lookup(model)
	s.noteContext(u, w)
}

// contextUse is the open chat's context use; ok is false when no turn of
// this chat reported one.
func (s *aiState) contextUse() (tokens, window, pct int, ok bool) {
	if s == nil || s.ctxUse.window <= 0 || s.ctxUse.conv != s.conv.ID {
		return 0, 0, 0, false
	}
	c := s.ctxUse
	return c.tokens, c.window, int(math.Round(float64(c.tokens) * 100 / float64(c.window))), true
}

// aiContextBar is pct as five cells, filled in fifths; a used window
// fills at least one.
func aiContextBar(pct int, ascii bool) string {
	n := min(max((pct+10)/20, 0), 5)
	if pct > 0 && n == 0 {
		n = 1
	}
	on, off := "▰", "▱"
	if ascii {
		on, off = "#", "-"
	}
	return strings.Repeat(on, n) + strings.Repeat(off, 5-n)
}

// aiContextTone is the chip's colour for pct.
func aiContextTone(pct int) theme.Color256 {
	switch {
	case pct >= aiContextFullPct:
		return theme.AccentRedCoral
	case pct >= aiContextWarnPct:
		return theme.AccentAmber
	}
	return theme.AccentGreen
}

// aiContextChipLabel is "ctx ▰▰▱▱▱ 42%", or "" when the open chat has no
// known context use.
func (m Model) aiContextChipLabel() string {
	_, _, pct, ok := m.ai.contextUse()
	if !ok {
		return ""
	}
	return "ctx " + aiContextBar(pct, theme.CurrentIconMode == theme.IconModeASCII) + " " + strconv.Itoa(pct) + "%"
}

// aiContextChipTok is the chip's colour token.
func (m Model) aiContextChipTok() theme.Color256 {
	_, _, pct, _ := m.ai.contextUse()
	return aiContextTone(pct)
}

// aiUsageMoreHint is the hint of the ⋯ menu's Usage row.
func aiUsageMoreHint(s *aiState) string {
	if _, _, pct, ok := s.contextUse(); ok {
		return "context " + strconv.Itoa(pct) + "% · tokens and cost"
	}
	return "tokens and cost"
}

// aiContextTooltip is the chip's hover label with the token counts.
func (m Model) aiContextTooltip() string {
	tokens, window, pct, ok := m.ai.contextUse()
	if !ok {
		return ""
	}
	tip := "Context: " + aiGroupDigits(tokens) + " of " + aiGroupDigits(window) + " tokens (" + strconv.Itoa(pct) + "%) · click: AI: Usage"
	if pct >= aiContextWarnPct && m.ai.settings.Compact != "off" {
		tip += " · older messages get summarised from " + strconv.Itoa(aiCompactAt) + "%"
	}
	return tip
}
