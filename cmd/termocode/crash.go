package main

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/crash"
)

// crashLeave is written to the terminal after a crash, on top of Bubble
// Tea's own restore (alt screen off, kitty keyboard flags popped): every
// mouse mode, bracketed paste and focus reports off, the cursor shown in
// its default shape, then the host changes undone (background, font,
// bidi). Each part is harmless when it is already off.
func crashLeave(hostLeave, fontRestore, rtlLeave string) string {
	return "\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l" + // mouse
		"\x1b[?2004l\x1b[?1004l" + // bracketed paste, focus reports
		"\x1b[?25h\x1b[0 q" + // cursor on, default shape
		hostLeave + fontRestore + rtlLeave
}

// envTestCrash is a hidden switch for the crash tests (e2e): F12 panics
// in Update ("update"), in the next View ("view"), in a command ("cmd")
// or in a background goroutine ("go"). Never set it yourself.
const envTestCrash = "TERMOCODE_TEST_CRASH"

// testCrashModel wraps m for envTestCrash; mode "" returns m as it is.
func testCrashModel(m tea.Model, mode string) tea.Model {
	if mode == "" {
		return m
	}
	return testCrash{m: m, mode: mode, view: new(bool)}
}

type testCrash struct {
	m    tea.Model
	mode string
	view *bool
}

func (t testCrash) Init() tea.Cmd { return t.m.Init() }

func (t testCrash) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.Type == tea.KeyF12 {
		switch t.mode {
		case "update":
			panic("test crash in update")
		case "view":
			*t.view = true
		case "cmd":
			return t, func() tea.Msg { panic("test crash in a command") }
		case "go":
			crash.Go("the test worker", func() { panic("test crash in a goroutine") })
		}
		return t, nil
	}
	m, cmd := t.m.Update(msg)
	t.m = m
	return t, cmd
}

func (t testCrash) View() string {
	if *t.view {
		panic("test crash in view")
	}
	return t.m.View()
}
