package session

import (
	"strings"
	"testing"
)

// feedSplit feeds s one byte at a time: sequences split across reads.
func feedSplit(m *Modes, s string) {
	for i := 0; i < len(s); i++ {
		m.Feed([]byte{s[i]})
	}
}

// What the app enables at start (Bubble Tea alt screen, all-motion mouse,
// SGR mouse, bracketed paste, hidden cursor) plus its cursor shape and
// background colour.
const appStart = "\x1b]11;#121212\x07\x1b[?1049h\x1b[?25l\x1b[?2004h\x1b[?1003h\x1b[?1006h\x1b[2J\x1b[5 q"

func TestModesReplay(t *testing.T) {
	m := NewModes()
	feedSplit(m, "hello"+appStart+"text\x1b[31mred\x1b[0m")
	got := string(m.Replay())
	if !strings.HasPrefix(got, "\x1b[?1049h") {
		t.Fatalf("alt screen must come first: %q", got)
	}
	for _, want := range []string{"\x1b[?25l", "\x1b[?2004h", "\x1b[?1003h", "\x1b[?1006h", "\x1b[5 q", "\x1b]11;#121212\x07"} {
		if !strings.Contains(got, want) {
			t.Errorf("replay %q is missing %q", got, want)
		}
	}
}

func TestModesFollowRuntimeChanges(t *testing.T) {
	m := NewModes()
	m.Feed([]byte(appStart))
	// A text field takes focus: all-motion off, cell motion on.
	m.Feed([]byte("\x1b[?1003l\x1b[?1002h"))
	got := string(m.Replay())
	if !strings.Contains(got, "\x1b[?1002h") || strings.Contains(got, "\x1b[?1003h") {
		t.Errorf("mouse mode change not followed: %q", got)
	}
	// Cursor shape back to default, colour reset.
	m.Feed([]byte("\x1b[0 q\x1b]111\x07"))
	got = string(m.Replay())
	if strings.Contains(got, " q") || strings.Contains(got, "]11;") {
		t.Errorf("reset cursor / colour still replayed: %q", got)
	}
}

func TestModesSkipStrings(t *testing.T) {
	m := NewModes()
	// A kitty image (APC) and a DCS whose bodies look like mode sets must
	// not count (an ESC would end them, as in a real terminal); a huge OSC
	// is skipped but parsing resumes after it.
	feedSplit(m, "\x1b_Ga=T;[?1049h\x1b\\\x1bP[?2004h\x1b\\\x1b]2;"+strings.Repeat("x", 2000)+"\x07\x1b[?1006h")
	if m.Mode(1049) || m.Mode(2004) {
		t.Errorf("modes inside APC/DCS were recorded: %q", m.Replay())
	}
	if !m.Mode(1006) {
		t.Errorf("mode after a long OSC was lost")
	}
}

func TestModesKittyAndFont(t *testing.T) {
	m := NewModes()
	m.Feed([]byte("\x1b[>1u\x1b[>3u\x1b[<u\x1b]1337;ChangeFontSize=-2\x07"))
	got := string(m.Replay())
	if !strings.Contains(got, "\x1b[>1u") || strings.Contains(got, "\x1b[>3u") {
		t.Errorf("kitty stack wrong: %q", got)
	}
	if !strings.Contains(got, "ChangeFontSize=-2") {
		t.Errorf("font delta not replayed: %q", got)
	}
	reset := string(m.Reset())
	if !strings.Contains(reset, "\x1b[<1u") || !strings.Contains(reset, "ChangeFontSize=2") {
		t.Errorf("reset does not undo kitty / font: %q", reset)
	}
}

func TestModesReset(t *testing.T) {
	m := NewModes()
	m.Feed([]byte(appStart))
	got := string(m.Reset())
	for _, want := range []string{"\x1b[?1003l", "\x1b[?1006l", "\x1b[?2004l", "\x1b[?25h", "\x1b[0 q", "\x1b]111\x07"} {
		if !strings.Contains(got, want) {
			t.Errorf("reset %q is missing %q", got, want)
		}
	}
	if !strings.HasSuffix(got, "\x1b[?1049l") {
		t.Errorf("alt screen must be left last: %q", got)
	}
}
