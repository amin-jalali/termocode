package connectcard

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
func runes(s string) tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func run(c tea.Cmd) tea.Msg {
	if c == nil {
		return nil
	}
	return c()
}

func ghCard() Card {
	return Card{Title: "Connect GitHub", Rows: []Row{
		{Kind: Text, Text: "You are signed in to GitHub with the gh command."},
		{Kind: Choice, ID: "account", Text: "Account", Options: []string{"amin-jalali (active)", "perkino-deals", "huisstart"}, Vertical: true},
		{Kind: Choice, ID: "access", Text: "Access", Options: []string{"Read only", "Read and write"}},
		{Kind: Dim, Text: "termocode asks gh for the token when it connects; it keeps no copy."},
	}, Buttons: []Button{{ID: "gh", Title: "Use gh", Primary: true}, {ID: "device", Title: "Sign in on github.com instead"}, {ID: "cancel", Title: "Cancel"}},
		Keys: []string{"c", "copy link"}}
}

// Spec (CX-5): the card is at most 76 cells wide and every row has the
// same width at 80 and 160 columns; long text wraps inside it.
func TestCardFits(t *testing.T) {
	for _, w := range []int{80, 160, 60} {
		m := New(ghCard())
		m.SetSize(w, 24)
		box := m.Box()
		lines := strings.Split(box, "\n")
		bw := ansi.StringWidth(lines[0])
		if bw > MaxWidth || bw > w-4 {
			t.Errorf("w=%d: box %d wide", w, bw)
		}
		for i, l := range lines {
			if ansi.StringWidth(l) != bw {
				t.Errorf("w=%d line %d: %d cells, want %d: %q", w, i, ansi.StringWidth(l), bw, ansi.Strip(l))
			}
		}
		if len(lines) > 22 {
			t.Errorf("w=%d: %d rows", w, len(lines))
		}
	}
}

// Spec: the focus starts on the first choice; ↓/Tab move through the
// choices to the buttons; ←/→ change the option or the button; Enter on
// a choice presses the primary button.
func TestCardKeys(t *testing.T) {
	m := New(ghCard())
	m.SetSize(100, 30)
	m, _ = m.Update(key(tea.KeyRight))
	if m.Choice("account") != 1 {
		t.Errorf("account %d", m.Choice("account"))
	}
	m, _ = m.Update(key(tea.KeyTab))
	m, _ = m.Update(key(tea.KeyRight))
	if m.Choice("access") != 1 {
		t.Errorf("access %d", m.Choice("access"))
	}
	m, c := m.Update(key(tea.KeyEnter))
	if got := run(c); got != (ButtonMsg{ID: "gh"}) {
		t.Errorf("enter on a choice: %v", got)
	}
	m, _ = m.Update(key(tea.KeyTab)) // to the buttons
	m, _ = m.Update(key(tea.KeyRight))
	_, c = m.Update(key(tea.KeyEnter))
	if got := run(c); got != (ButtonMsg{ID: "device"}) {
		t.Errorf("second button: %v", got)
	}
	if got := run(func() tea.Cmd { _, c := m.Update(runes("c")); return c }()); got != (KeyMsg{Key: "c"}) {
		t.Errorf("letter key: %v", got)
	}
	if got := run(func() tea.Cmd { _, c := m.Update(key(tea.KeyEsc)); return c }()); got != (CloseMsg{}) {
		t.Errorf("esc: %v", got)
	}
}

// Spec: a masked input takes the focus first, never shows its value and
// submits on Enter; letter keys go into the input there.
func TestCardMaskedInput(t *testing.T) {
	c := Card{Title: "Connect Sentry with a token", Rows: []Row{{Kind: Input, ID: "token", Text: "3. Paste it here:", Masked: true}},
		Buttons: []Button{{ID: "connect", Title: "Connect", Primary: true}, {ID: "cancel", Title: "Cancel"}}, Keys: []string{"s", "use sentry-cli"}}
	m := New(c)
	m.SetSize(80, 24)
	m, _ = m.Update(runes("sntrys_secret_value"))
	if m.Value() != "sntrys_secret_value" {
		t.Errorf("value %q", m.Value())
	}
	if strings.Contains(ansi.Strip(m.Box()), "sntrys") || !strings.Contains(ansi.Strip(m.Box()), "•••") {
		t.Errorf("masked box:\n%s", ansi.Strip(m.Box()))
	}
	_, cmd := m.Update(key(tea.KeyEnter))
	if got := run(cmd); got != (SubmitMsg{ID: "token", Value: "sntrys_secret_value"}) {
		t.Errorf("submit %v", got)
	}
	// A countdown tick (same card) keeps the typed value.
	m.SetCard(c)
	if m.Value() != "sntrys_secret_value" {
		t.Error("SetCard dropped the value")
	}
}

// Spec: clicks press buttons and pick options.
func TestCardMouse(t *testing.T) {
	m := New(ghCard())
	m.SetSize(120, 40)
	rs := m.ButtonRects()
	if len(rs) != 3 {
		t.Fatalf("rects %v", rs)
	}
	_, c := m.HandleMouse(rs[2].X+1, rs[2].Y, tea.MouseActionPress, tea.MouseButtonLeft)
	if got := run(c); got != (ButtonMsg{ID: "cancel"}) {
		t.Errorf("click: %v", got)
	}
	plain := strings.Split(ansi.Strip(m.Box()), "\n")
	_, oy := m.Origin()
	ox, _ := m.Origin()
	for y, l := range plain {
		if i := strings.Index(l, "huisstart"); i >= 0 {
			x := ox + len([]rune(l[:i]))
			m, _ = m.HandleMouse(x, oy+y, tea.MouseActionPress, tea.MouseButtonLeft)
		}
	}
	if m.Choice("account") != 2 {
		t.Errorf("clicked option: %d", m.Choice("account"))
	}
}

// Spec: a long link is cut in the middle, the device code is framed.
func TestCardCodeAndLink(t *testing.T) {
	c := Card{Title: "Connect GitHub", Rows: []Row{
		{Kind: Link, Text: "https://example.com/authorize?client_id=" + strings.Repeat("x", 120) + "&state=END"},
		{Kind: Code, Text: "ABCD-1234"},
		{Kind: Status, Text: "◷ Waiting for GitHub …  14:32 left"},
	}}
	m := New(c)
	m.SetSize(80, 24)
	s := ansi.Strip(m.Box())
	if !strings.Contains(s, "…") || !strings.Contains(s, "END") || !strings.Contains(s, "│ ABCD-1234 │") {
		t.Errorf("box:\n%s", s)
	}
}
