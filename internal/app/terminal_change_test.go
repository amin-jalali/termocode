package app

import (
	"os"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/anim"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/termimg"
	"github.com/amin-jalali/termocode/internal/theme"
)

// TestTerminalChangeReappliesCaps: a session attached from a different
// terminal (TerminalChangedMsg, after the environment was updated) is drawn
// with that terminal's colour profile and graphics protocol, not the ones
// of the first terminal.
func TestTerminalChangeReappliesCaps(t *testing.T) {
	withConfigDir(t)
	oldProfile, oldCaps, oldIcons := lipgloss.ColorProfile(), termCaps, theme.CurrentIconMode
	t.Cleanup(func() {
		lipgloss.SetColorProfile(oldProfile)
		termCaps = oldCaps
		theme.CurrentIconMode = oldIcons
	})
	truecolor := regexp.MustCompile(`\x1b\[[0-9;]*[34]8;2;`)
	color256 := regexp.MustCompile(`\x1b\[[0-9;]*[34]8;5;`)

	// The first terminal: truecolor, kitty graphics.
	t.Setenv("TERMOCODE_COLORS", "truecolor")
	lipgloss.SetColorProfile(theme.DetectProfile(os.Getenv))
	termCaps = termimg.Caps{Protocol: termimg.Kitty}
	m0, _ := New().Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	out := m0.View()
	if !truecolor.MatchString(out) {
		t.Fatalf("first terminal: no truecolor SGR")
	}
	if !strings.Contains(out, "\x1b_G") {
		t.Fatalf("first terminal: no kitty image (welcome logo)")
	}

	// A 256-colour terminal without graphics attaches.
	t.Setenv("TERMOCODE_COLORS", "256")
	m1, _ := m0.Update(TerminalChangedMsg{Graphics: termimg.Caps{Protocol: termimg.Blocks}, Gen: 5})
	out = m1.View()
	if !strings.HasPrefix(out, session.AttachedSeq(5)) {
		t.Errorf("frame not marked for attach 5: %q", out[:min(len(out), 40)])
	}
	if truecolor.MatchString(out) {
		t.Errorf("24-bit SGR after a 256-colour terminal attached: %q", truecolor.FindString(out))
	}
	if !color256.MatchString(out) {
		t.Errorf("no 256-colour SGR after a 256-colour terminal attached")
	}
	if strings.Contains(out, "\x1b_G") {
		t.Errorf("kitty graphics sent to a terminal without them")
	}

	// And back to kitty: the image is drawn again.
	t.Setenv("TERMOCODE_COLORS", "truecolor")
	m2, _ := m1.Update(TerminalChangedMsg{Graphics: termimg.Caps{Protocol: termimg.Kitty, CellW: 9, CellH: 18}})
	out = m2.View()
	if !truecolor.MatchString(out) || !strings.Contains(out, "\x1b_G") {
		t.Errorf("back on kitty: truecolor=%v kitty=%v", truecolor.MatchString(out), strings.Contains(out, "\x1b_G"))
	}
	if w, h := termCaps.CellSize(); w != 9 || h != 18 {
		t.Errorf("cell size %dx%d, want the new terminal's 9x18", w, h)
	}
}

// TestTerminalChangeNoteToast: a client that came back by itself says so
// in the attach; the UI shows it as a toast. A plain attach shows none.
func TestTerminalChangeNoteToast(t *testing.T) {
	withConfigDir(t)
	oldProfile, oldCaps, oldIcons, oldAnim := lipgloss.ColorProfile(), termCaps, theme.CurrentIconMode, anim.Enabled
	t.Cleanup(func() {
		lipgloss.SetColorProfile(oldProfile)
		termCaps = oldCaps
		theme.CurrentIconMode = oldIcons
		anim.Enabled = oldAnim
	})
	m0, _ := New().Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m1, _ := m0.Update(TerminalChangedMsg{Graphics: termimg.Caps{Protocol: termimg.Blocks}, Gen: 2})
	if strings.Contains(toastText(m1.(Model)), "Reconnected") {
		t.Fatalf("a plain attach shows a reconnect toast")
	}
	m2, _ := m1.Update(TerminalChangedMsg{Graphics: termimg.Caps{Protocol: termimg.Blocks}, Gen: 3, Note: "Reconnected after 12 s"})
	if got := toastText(m2.(Model)); !strings.Contains(got, "Reconnected after 12 s") {
		t.Errorf("no reconnect toast: %q", got)
	}
	// Keys were dropped: said in full, below the title.
	m3, _ := m2.Update(TerminalChangedMsg{Gen: 4, Note: "Reconnected after 3 s · keys typed while offline were dropped"})
	// As wide as the toast column of view.go (at most 52).
	var got strings.Builder
	for _, c := range m3.(Model).toast.Cards(52) {
		got.WriteString(strings.Join(c.Lines, " "))
	}
	if !strings.Contains(got.String(), "keys typed while offline were dropped") {
		t.Errorf("dropped keys not said in full: %q", got.String())
	}
}
