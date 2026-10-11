// Package toast shows short notices in the corner of the screen.
package toast

import (
	"strings"
	"time"
)

// Kind is the toast severity.
type Kind int

const (
	Info Kind = iota
	Warn
	Error
)

// Toast is one card.
type Toast struct {
	Kind      Kind
	Title     string
	Detail    string
	ExpiresAt time.Time
	leaving   bool
}

// Model holds the visible toasts, newest last.
type Model struct {
	toasts []Toast
	width  int
}

// Push adds a toast that expires after four seconds.
func (m Model) Push(k Kind, title string, now time.Time) Model {
	m.toasts = append(m.toasts, Toast{Kind: k, Title: title, ExpiresAt: now.Add(4 * time.Second)})
	return m
}

// Tick marks expired toasts as leaving and drops the ones that left.
func (m Model) Tick(now time.Time) Model {
	out := m.toasts[:0]
	for _, t := range m.toasts {
		if !t.ExpiresAt.After(now) && !t.leaving {
			t.leaving = true // slide out once
		}
		out = append(out, t)
	}
	m.toasts = out
	return m
}

// View renders the cards, one per line.
func (m Model) View() string {
	var b strings.Builder
	for _, t := range m.toasts {
		b.WriteString(icon(t.Kind) + " " + t.Title + "\n")
	}
	return b.String()
}

func icon(k Kind) string {
	switch k {
	case Warn:
		return "!"
	case Error:
		return "x"
	}
	return "i"
}
