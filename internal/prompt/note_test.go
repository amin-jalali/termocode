package prompt

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A note is shown in full, wrapped to the card, between the label and
// the input (sign-in instructions, an ssh -L command).
func TestPromptNoteWraps(t *testing.T) {
	m := New("Sign in", "Paste the address:", "")
	m.SetSize(100, 40)
	long := "Open the address in your browser. When it says it can not connect, copy the whole address and paste it here."
	m.SetNote(long + "\nssh -L 8765:127.0.0.1:8765 host")
	v := ansi.Strip(m.View())
	flat := strings.Join(strings.Fields(strings.ReplaceAll(v, "│", " ")), " ")
	if !strings.Contains(flat, long) || !strings.Contains(v, "ssh -L 8765:127.0.0.1:8765 host") {
		t.Errorf("note missing or cut:\n%s", v)
	}
	for _, l := range strings.Split(v, "\n") {
		if w := ansi.StringWidth(l); w > 64 {
			t.Errorf("row wider than the card (%d): %q", w, l)
		}
	}
}
