package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Session recap on reconnect (CMP-29). The session keeps running when the
// terminal goes away; this says what happened meanwhile. Every notice that
// may become a desktop notification (an agent or AI run ended or waits, a
// workflow finished) is kept here too, focused or not. When a client
// attaches again after at least recapMinAway without a key or click, and
// something happened since the last one, a toast says how much; a click on
// it (or Session: Show Recap) opens a read-only card with the list.
//
// The log lives in memory only, holds at most recapMax short lines and
// never leaves the UI process: no file, no AI, no web panel.

const (
	recapMax       = 100
	recapShow      = 30
	recapTextRunes = 120
	recapMinAway   = 30 * time.Second
)

const confirmKindSessionRecap confirmKindEnum = 8291

// recapNow is the recap clock (a seam for tests).
var recapNow = time.Now

type recapEvent struct {
	At   time.Time
	Text string
}

type sessionRecap struct {
	events []recapEvent
	// lastInput is the last key or click of the user.
	lastInput time.Time
	// awayFrom and awayTo are the away window of the last reconnect that
	// had news (zero: none yet).
	awayFrom, awayTo time.Time
}

// sessionRecapClickMsg is a click on the "while you were away" toast.
type sessionRecapClickMsg struct{}

func (m *Model) recapS() *sessionRecap {
	if m.recap == nil {
		m.recap = &sessionRecap{}
	}
	return m.recap
}

// recapClean makes one safe, short line: no control characters, at most
// recapTextRunes runes.
func recapClean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > recapTextRunes {
		s = string(r[:recapTextRunes-1]) + "…"
	}
	return s
}

// recapAdd keeps one event; the oldest go once there are recapMax.
func (m *Model) recapAdd(text string) {
	text = recapClean(text)
	if text == "" {
		return
	}
	r := m.recapS()
	r.events = append(r.events, recapEvent{At: recapNow(), Text: text})
	if n := len(r.events) - recapMax; n > 0 {
		r.events = append([]recapEvent(nil), r.events[n:]...)
	}
}

// noteRecapInput marks the user as present on a key or a mouse press
// (not on motion, a resize or a redraw).
func (m *Model) noteRecapInput(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
	case tea.MouseMsg:
		if msg.Action != tea.MouseActionPress {
			return
		}
	default:
		return
	}
	m.recapS().lastInput = recapNow()
}

// since returns the events after from, oldest first.
func (r *sessionRecap) since(from time.Time) []recapEvent {
	i := len(r.events)
	for i > 0 && r.events[i-1].At.After(from) {
		i--
	}
	return r.events[i:]
}

// recapOnAttach runs when a terminal attached to the session again
// (TerminalChangedMsg). With news since the user's last key or click,
// and an absence of at least recapMinAway, it pushes a clickable toast.
func (m *Model) recapOnAttach() tea.Cmd {
	r := m.recapS()
	now := recapNow()
	from := r.lastInput
	if !from.IsZero() && now.Sub(from) < recapMinAway {
		return nil
	}
	n := len(r.since(from))
	if n == 0 {
		return nil
	}
	r.awayFrom, r.awayTo = from, now
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushClick(toast.Info, strconv.Itoa(n)+" update"+plural(n)+" while you were away",
		"Click to see them (Session: Show Recap).", sessionRecapClickMsg{})
	return cmd
}

// recapTimeOfDay is a time of day, with the date when it is not today.
func recapTimeOfDay(t, now time.Time) string {
	if y, mo, d := t.Date(); y != now.Year() || mo != now.Month() || d != now.Day() {
		return t.Format("Jan 2 15:04")
	}
	return t.Format("15:04")
}

// recapSpan is a short length of time: "less than a minute", "13 min",
// "2 h 5 min".
func recapSpan(d time.Duration) string {
	mins := int(d / time.Minute)
	switch {
	case mins < 1:
		return "less than a minute"
	case mins < 60:
		return strconv.Itoa(mins) + " min"
	case mins%60 == 0:
		return strconv.Itoa(mins/60) + " h"
	}
	return fmt.Sprintf("%d h %d min", mins/60, mins%60)
}

// recapCard is the card's title and text.
func recapCard(r *sessionRecap, now time.Time) (title, body string) {
	var b strings.Builder
	var evs []recapEvent
	if r.awayTo.IsZero() {
		title = "Session recap"
		evs = r.events
		b.WriteString("No reconnect with news yet. The latest events of this session:\n")
	} else {
		title = "While you were away"
		evs = r.since(r.awayFrom)
		if r.awayFrom.IsZero() {
			b.WriteString("Since the session started (back at " + recapTimeOfDay(r.awayTo, now) + "):\n")
		} else {
			b.WriteString("Away from " + recapTimeOfDay(r.awayFrom, now) + " to " + recapTimeOfDay(r.awayTo, now) +
				" (" + recapSpan(r.awayTo.Sub(r.awayFrom)) + "):\n")
		}
	}
	if len(evs) == 0 {
		b.WriteString("\nNothing to show yet.\n")
	} else {
		if extra := len(evs) - recapShow; extra > 0 {
			b.WriteString("\n… and " + strconv.Itoa(extra) + " older\n")
			evs = evs[extra:]
		} else {
			b.WriteString("\n")
		}
		for _, e := range evs {
			b.WriteString(recapTimeOfDay(e.At, now) + "  " + e.Text + "\n")
		}
	}
	b.WriteString("\nKept: agent and AI runs that end or wait for you, and finished workflows. In memory only, never sent anywhere.")
	return title, b.String()
}

// openSessionRecap opens the read-only recap card (Close only). It never
// replaces another open card.
func (m *Model) openSessionRecap() tea.Cmd {
	if m.confirmOpen && m.confirmKind != confirmKindSessionRecap {
		return m.pushToast(toast.Info, "Close the open card first to see the recap")
	}
	title, body := recapCard(m.recapS(), recapNow())
	m.confirm = confirm.New(title, body, []confirm.Button{{ID: "close", Title: "Close"}}).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmKind, m.confirmOpen = confirmKindSessionRecap, true
	return nil
}
