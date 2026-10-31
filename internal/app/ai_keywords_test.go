package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
)

func TestAIDetectKeyword(t *testing.T) {
	for text, want := range map[string]string{
		"ulw fix the tests":                 "ulw",
		"please fix it, ULW.":               "ulw",
		"ultrawork: fix it":                 "ulw",
		"ulw plan the new cache":            "ulw plan",
		"Mass ULW: port every handler":      "mass ulw",
		"mulw port them":                    "mass ulw",
		"fix ulwx":                          "",
		"look at src/ulw/main.go":           "",
		"run `ulw` in the shell":            "",
		"```\nulw plan\n```\nwhat is this?": "",
		"plan ulw":                          "ulw",
		"":                                  "",
	} {
		kw, _ := aiDetectKeyword(text)
		if kw.word != want {
			t.Errorf("%q: got %q, want %q", text, kw.word, want)
		}
	}
}

func TestAIKeywordChipShowsAndCanBeRemoved(t *testing.T) {
	m := aiTestModel(t)
	m.ai.provider = fakeAIProvider{}
	m.ai.focused = true
	m.actionsOpen, m.actionsPinned = true, true
	m.ai.input = []rune("ulw fix the tests")
	l := m.aiDividerLine(70)
	if got := ansi.Strip(l.styled); !strings.Contains(got, "ulw → Work until done") || !strings.Contains(got, "Alt+Q") {
		t.Fatalf("chip %q", got)
	}
	if _, ok := m.routeAIPanelKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}, Alt: true}); !ok {
		t.Fatal("Alt+Q not handled")
	}
	if got := ansi.Strip(m.aiDividerLine(70).styled); strings.Contains(got, "Work until done") {
		t.Fatalf("chip still shown: %q", got)
	}
	m.ai.kwOff = false
	for _, b := range m.aiDividerLine(70).buttons {
		if b.id == "rm-keyword" {
			m.onAIButton(b.id)
		}
	}
	if !m.ai.kwOff {
		t.Fatal("the × of the chip does not remove it")
	}
}

func TestAIKeywordRunsItsProfileOnlyWhenKept(t *testing.T) {
	m := aiTestModel(t)
	s := m.ai
	s.provider = fakeAIProvider{reply: "ok"}
	s.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
	s.input = []rune("fix it, ulw")
	s.kwOff = true
	driveAITurn(t, &m, m.submitAIInput())
	if s.conv.Messages[len(s.conv.Messages)-1].Run != nil {
		t.Fatal("a removed chip still switched the profile")
	}
	if s.kwOff {
		t.Fatal("the removal stays for the next message")
	}
	if p, ok := s.profiles.Get(ai.ProfileUntilDone); !ok || p.Name != ai.ProfileUntilDone {
		t.Skip("no until-done profile")
	}
	kw, _ := aiDetectKeyword("fix it, ulw")
	if kw.profile != ai.ProfileUntilDone {
		t.Fatalf("ulw runs %q", kw.profile)
	}
	if kw, _ := aiDetectKeyword("mass ulw"); kw.profile != ai.ProfilePlan {
		t.Fatalf("mass ulw runs %q", kw.profile)
	}
}

func TestAIKeywordKeptStartsTheRun(t *testing.T) {
	m := aiTestModel(t)
	s := m.ai
	s.provider = fakeAIProvider{reply: "ok"}
	s.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
	s.slotFake = map[string]aiSlot{ai.SlotMain: {p: fakeAIProvider{reply: "ok"}, cfg: s.cfg}, ai.SlotQuick: {p: fakeAIProvider{reply: "ok"}, cfg: s.cfg}}
	s.input = []rune("fix it, ulw")
	m.submitAIInput()
	defer m.startNewAIChat()
	last := s.conv.Messages[len(s.conv.Messages)-1]
	if last.Run == nil || last.Run.Profile != ai.ProfileUntilDone {
		t.Fatalf("the kept ulw chip did not start until-done: %+v", last)
	}
	if s.activeProfile().Name == ai.ProfileUntilDone {
		t.Fatal("the keyword changed the chat's profile")
	}
}
