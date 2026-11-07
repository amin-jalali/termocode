package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// recapClock sets the recap clock for one test.
func recapClock(t *testing.T, at *time.Time) {
	t.Helper()
	old := recapNow
	recapNow = func() time.Time { return *at }
	t.Cleanup(func() { recapNow = old })
}

func TestSessionRecapKeepsNoticesEvenWhenFocused(t *testing.T) {
	withConfigDir(t)
	now := time.Date(2026, 11, 7, 14, 0, 0, 0, time.Local)
	recapClock(t, &now)
	m := &Model{}
	_ = m.notifyOS("AI run ended") // focused: no OS notice, but the recap keeps it
	m.notifyOSFor("Agent run ended", "Claude\x1b[31m Code")
	evs := m.recapS().events
	if len(evs) != 2 || evs[0].Text != "AI run ended" {
		t.Fatalf("events = %+v", evs)
	}
	if strings.ContainsRune(evs[1].Text, '\x1b') || !strings.Contains(evs[1].Text, "Agent run ended · Claude") {
		t.Fatalf("detail not cleaned: %q", evs[1].Text)
	}
}

func TestSessionRecapCapsTheLog(t *testing.T) {
	now := time.Now()
	recapClock(t, &now)
	m := &Model{}
	for i := 0; i < recapMax+10; i++ {
		m.recapAdd("event")
	}
	if n := len(m.recapS().events); n != recapMax {
		t.Fatalf("kept %d, want %d", n, recapMax)
	}
	m.recapAdd("   ")
	if n := len(m.recapS().events); n != recapMax {
		t.Fatal("blank event kept")
	}
	if got := recapClean(strings.Repeat("x", 500)); len([]rune(got)) > recapTextRunes {
		t.Fatalf("not cut: %d runes", len([]rune(got)))
	}
}

func TestSessionRecapInputIsKeysAndClicks(t *testing.T) {
	now := time.Date(2026, 11, 7, 14, 0, 0, 0, time.Local)
	recapClock(t, &now)
	m := &Model{}
	m.noteRecapInput(tea.MouseMsg{Action: tea.MouseActionMotion})
	m.noteRecapInput(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !m.recapS().lastInput.IsZero() {
		t.Fatal("motion or resize counted as input")
	}
	m.noteRecapInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if !m.recapS().lastInput.Equal(now) {
		t.Fatal("key not counted")
	}
	now = now.Add(time.Minute)
	m.noteRecapInput(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.recapS().lastInput.Equal(now) {
		t.Fatal("click not counted")
	}
}

func TestSessionRecapToastOnReconnect(t *testing.T) {
	now := time.Date(2026, 11, 7, 14, 0, 0, 0, time.Local)
	recapClock(t, &now)
	m := &Model{}
	m.noteRecapInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m.recapAdd("before you left") // same instant as the last key: not "away"
	now = now.Add(5 * time.Minute)
	m.recapAdd("Agent run ended")
	now = now.Add(7 * time.Minute)
	m.recapAdd("Workflow nightly done")
	now = now.Add(time.Minute)
	if m.recapOnAttach() == nil {
		t.Fatal("no toast after a reconnect with news")
	}
	live := m.toast.Live()
	if len(live) != 1 || !strings.Contains(live[0].Title, "2 updates while you were away") {
		t.Fatalf("toast = %+v", live)
	}
	title, body := recapCard(m.recapS(), now)
	if title != "While you were away" {
		t.Fatalf("title %q", title)
	}
	for _, want := range []string{"14:00", "14:13", "13 min", "14:05  Agent run ended", "14:12  Workflow nightly done"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body misses %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "before you left") {
		t.Fatalf("old event shown:\n%s", body)
	}
}

func TestSessionRecapQuietWhenNothingHappenedOrShortAway(t *testing.T) {
	now := time.Date(2026, 11, 7, 14, 0, 0, 0, time.Local)
	recapClock(t, &now)
	m := &Model{}
	m.noteRecapInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	now = now.Add(10 * time.Minute)
	if m.recapOnAttach() != nil {
		t.Fatal("toast with no news")
	}
	m.recapAdd("AI run ended")
	now = now.Add(5 * time.Second)
	m.noteRecapInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	m.recapAdd("AI run ended")
	now = now.Add(10 * time.Second)
	if m.recapOnAttach() != nil {
		t.Fatal("toast after a short blip")
	}
}

func TestSessionRecapCardOpensReadOnly(t *testing.T) {
	now := time.Date(2026, 11, 7, 14, 0, 0, 0, time.Local)
	recapClock(t, &now)
	m := &Model{w: 100, h: 40}
	_ = m.openSessionRecap()
	if !m.confirmOpen || m.confirmKind != confirmKindSessionRecap {
		t.Fatal("card not open")
	}
	if b := m.confirm.Buttons(); len(b) != 1 || b[0].ID != "close" {
		t.Fatalf("buttons = %+v", b)
	}
	if _, ok := m.handleAIConfirm("close"); !ok {
		t.Fatal("answer not handled")
	}
	title, body := recapCard(m.recapS(), now)
	if title != "Session recap" || !strings.Contains(body, "Nothing to show yet") {
		t.Fatalf("empty card: %q %q", title, body)
	}
}

func TestSessionRecapCardKeepsAnOpenCard(t *testing.T) {
	m := &Model{w: 100, h: 40}
	m.confirmOpen, m.confirmKind = true, confirmKindAgentTranscripts
	_ = m.openSessionRecap()
	if m.confirmKind != confirmKindAgentTranscripts {
		t.Fatal("recap replaced an open card")
	}
}

func TestSessionRecapPaletteAndPolicy(t *testing.T) {
	found := false
	for _, it := range paletteItems() {
		if it.ID == "session-recap" {
			found = true
		}
	}
	if !found {
		t.Fatal("no palette command")
	}
	if p, ok := aiActionPolicyFor("session-recap"); !ok || p.Gate != aiGateAuto {
		t.Fatalf("policy = %+v", p)
	}
}
