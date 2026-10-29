package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Drawing of the setup card (setup_card.go) inside the Welcome page: a
// checklist of welcome rows (welcome_row.go).
//
//	SET UP
//	   ● Icons  Unicode icons · GNOME Terminal: Ubuntu Mono 11     Fix…
//	   ✓ AI     Claude · claude-opus-5-5
//	   ○ Theme  VSCode Dark+                                    Change…
//	   Hide setup
//
// Below welcomeNarrowMin cells a row whose status would be cut by its
// action word puts the status on the next line, under the title.

// welcomeSetup is the card the next Welcome render draws (zero = none).
// Model.publishSetupCard sets it before every render / hit test.
var welcomeSetup setupCardView

// welcomeSetupHit is one checklist row on screen.
type welcomeSetupHit struct {
	Row      int
	ColStart int
	ColEnd   int
	ID       string // step id, or "hide"
}

// setupLine is one line of the checklist: a row, or a plain line (the
// header, a note under a row).
type setupLine struct {
	row    *welcomeRow
	header bool
	done   bool // header: every required step is done
	note   string
	indent int
}

// text draws a plain line W cells wide.
func (l setupLine) text(W int) string {
	if l.header {
		h := sectionHeader("SET UP")
		if l.done {
			dim := lipgloss.NewStyle().Background(theme.LG(theme.BgEditor)).Foreground(theme.Readable(theme.TextDim, theme.BgEditor, 3.0))
			h += dim.Render(" · done")
		}
		return padRowToWidth(h, W, theme.Bg(theme.BgEditor))
	}
	return renderWelcomeNote(l.note, l.indent, W)
}

// setupStatusCol is where a step's status (and its notes) start; the
// title column is setupTitleW wide (a 6-letter title such as "Import"
// widens both by one for every row, so the statuses stay aligned).
const (
	setupStatusCol = 12
	setupTitleW    = 7
)

// setupCardLines lays the checklist out at width W.
func setupCardLines(v setupCardView, W int) []setupLine {
	narrow := W < welcomeNarrowMin
	titleW, statusCol := setupTitleW, setupStatusCol
	for _, s := range v.steps {
		if runewidth.StringWidth(s.title) > setupTitleW-2 {
			titleW, statusCol = setupTitleW+1, setupStatusCol+1
		}
	}
	noteIndent := statusCol
	if narrow {
		noteIndent = 5
	}
	out := []setupLine{{header: true, done: v.allDone}}
	for _, s := range v.steps {
		mark, tok := setupMark(s)
		r := &welcomeRow{id: "setup:" + s.id, glyph: mark, glyphTok: tok, title: s.title, titleW: titleW,
			status: s.status, right: s.action, interactive: s.action != "", muted: s.done}
		var under []string
		fits := statusCol+runewidth.StringWidth(s.status)+2+runewidth.StringWidth(s.action)+1 <= W
		if narrow && s.action != "" && !fits {
			r.status = ""
			under = append(under, s.status)
		}
		out = append(out, setupLine{row: r})
		for _, u := range under {
			out = append(out, setupLine{note: u, indent: noteIndent})
		}
		for _, n := range s.notes {
			for _, l := range wrapWords(n, max(W-1-noteIndent, 8)) {
				out = append(out, setupLine{note: l, indent: noteIndent})
			}
		}
	}
	out = append(out, setupLine{row: &welcomeRow{id: "setup:hide", title: "Hide setup", titleMuted: true, interactive: true}})
	return out
}

// setupMark is the step's state glyph: done, optional, to do. To do is
// the accent (the one thing to look at); done and optional are grey.
func setupMark(s setupStep) (string, theme.Color256) {
	ascii := theme.CurrentIconMode == theme.IconModeASCII
	switch {
	case s.done:
		if ascii {
			return "+", theme.TextMuted
		}
		return "✓", theme.TextMuted
	case s.optional:
		if ascii {
			return "-", theme.TextDim
		}
		return "○", theme.TextDim
	}
	if ascii {
		return "*", theme.BorderFocus
	}
	return "●", theme.BorderFocus
}

// wrapWords word-wraps plain text to w cells (a longer word is cut).
func wrapWords(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	lead := len(s) - len(strings.TrimLeft(s, " "))
	var out []string
	line := strings.Repeat(" ", lead)
	for _, word := range strings.Fields(s) {
		if runewidth.StringWidth(word) > w {
			word = runewidth.Truncate(word, w, "…")
		}
		switch {
		case strings.TrimSpace(line) == "":
			line += word
		case runewidth.StringWidth(line)+1+runewidth.StringWidth(word) > w:
			out = append(out, line)
			line = strings.Repeat(" ", lead) + word
		default:
			line += " " + word
		}
	}
	if strings.TrimSpace(line) != "" {
		out = append(out, line)
	}
	return out
}
