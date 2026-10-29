package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestCrashLeaveRestoresTerminal: after a crash the terminal gets mouse,
// paste and focus reports off, the cursor back, and the host changes
// (background, font, bidi) undone.
func TestCrashLeaveRestoresTerminal(t *testing.T) {
	got := crashLeave("<host>", "<font>", "<rtl>")
	for _, want := range []string{
		"\x1b[?1000l", "\x1b[?1002l", "\x1b[?1003l", "\x1b[?1006l", // mouse
		"\x1b[?2004l", "\x1b[?1004l", // paste, focus
		"\x1b[?25h", "\x1b[0 q", // cursor on, default shape
		"<host>", "<font>", "<rtl>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("restore misses %q: %q", want, got)
		}
	}
}

type plain struct{}

func (plain) Init() tea.Cmd                       { return nil }
func (plain) Update(tea.Msg) (tea.Model, tea.Cmd) { return plain{}, nil }
func (plain) View() string                        { return "ok" }

var f12 = tea.KeyMsg{Type: tea.KeyF12}

func panics(f func()) (did bool) {
	defer func() { did = recover() != nil }()
	f()
	return false
}

// TestTestCrashHook: TERMOCODE_TEST_CRASH makes F12 panic where asked;
// without it F12 is a plain key.
func TestTestCrashHook(t *testing.T) {
	if m := testCrashModel(plain{}, ""); panics(func() { m.Update(f12) }) {
		t.Fatal("no hook set, but F12 panicked")
	}
	if m := testCrashModel(plain{}, "update"); !panics(func() { m.Update(f12) }) {
		t.Fatal("update: F12 did not panic")
	}
	m := testCrashModel(plain{}, "view")
	m2, _ := m.Update(f12)
	if !panics(func() { m2.View() }) {
		t.Fatal("view: View did not panic after F12")
	}
	m = testCrashModel(plain{}, "cmd")
	_, cmd := m.Update(f12)
	if cmd == nil || !panics(func() { cmd() }) {
		t.Fatal("cmd: no panicking command after F12")
	}
	m = testCrashModel(plain{}, "update")
	if panics(func() { m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}) }) {
		t.Fatal("a key other than F12 panicked")
	}
}
