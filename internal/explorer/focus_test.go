package explorer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// TestHeaderLineFollowsFocus: the hairline under EXPLORER lights up in the
// focus accent while the explorer has focus, and is a plain border when
// it does not. The text is the same either way.
func TestHeaderLineFollowsFocus(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	dir := t.TempDir()
	mustWrite(t, dir+"/a.go", "")
	root, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := Model{root: root}
	m.SetSize(30, 20)
	on := m.View(true, "", false, nil)
	off := m.View(false, "", false, nil)
	if ansi.Strip(on) != ansi.Strip(off) {
		t.Fatalf("text changed with focus")
	}
	hair := func(s string) string { return strings.Split(s, "\n")[1] }
	if hair(on) == hair(off) {
		t.Error("header hairline looks the same with and without focus")
	}
}
