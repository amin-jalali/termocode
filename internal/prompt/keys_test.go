package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

func press(m Model, keys ...tea.KeyMsg) Model {
	for _, k := range keys {
		m, _ = m.Update(k)
	}
	return m
}

func k(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

// multi is a names editor holding text with the caret at pos.
func multi(text string, pos int) Model {
	m := New("Rename files", "", text)
	m.SetSize(100, 30)
	m.SetMultiline()
	m.caret = pos
	return m
}

func TestMultilineCaretMoves(t *testing.T) {
	text := "alpha\nbe\ngamma" // lines start at 0, 6, 9
	cases := []struct {
		name string
		pos  int
		key  tea.KeyMsg
		want int
	}{
		{"left at start stays", 0, k(tea.KeyLeft), 0},
		{"left", 3, k(tea.KeyLeft), 2},
		{"right at end stays", 14, k(tea.KeyRight), 14},
		{"right", 3, k(tea.KeyRight), 4},
		{"home goes to line start", 8, k(tea.KeyHome), 6},
		{"ctrl+a goes to line start", 12, k(tea.KeyCtrlA), 9},
		{"end goes to line end", 6, k(tea.KeyEnd), 8},
		{"ctrl+e goes to line end", 0, k(tea.KeyCtrlE), 5},
		{"up keeps the column", 11, k(tea.KeyUp), 8}, // gamma col 2 → "be" is short: its end
		{"up from a short line", 7, k(tea.KeyUp), 1}, // be col 1 → alpha col 1
		{"up on the first line stays", 2, k(tea.KeyUp), 2},
		{"down keeps the column", 1, k(tea.KeyDown), 7},
		{"down to a shorter line", 4, k(tea.KeyDown), 8},
		{"down on the last line stays", 11, k(tea.KeyDown), 11},
	}
	for _, c := range cases {
		m := press(multi(text, c.pos), c.key)
		if m.caret != c.want {
			t.Errorf("%s: caret %d, want %d", c.name, m.caret, c.want)
		}
		if m.Value() != text {
			t.Errorf("%s: moving changed the text to %q", c.name, m.Value())
		}
	}
}

func TestMultilineEditing(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		pos       int
		key       tea.KeyMsg
		want      string
		wantCaret int
	}{
		{"backspace", "ab\ncd", 3, k(tea.KeyBackspace), "abcd", 2},
		{"backspace at start", "ab", 0, k(tea.KeyBackspace), "ab", 0},
		{"delete", "ab\ncd", 2, k(tea.KeyDelete), "abcd", 2},
		{"delete at end", "ab", 2, k(tea.KeyDelete), "ab", 2},
		{"ctrl+k cuts to line end", "abc\ndef", 1, k(tea.KeyCtrlK), "a\ndef", 1},
		{"enter adds a line", "ab", 1, k(tea.KeyEnter), "a\nb", 2},
		{"space", "ab", 1, k(tea.KeySpace), "a b", 2},
		{"typing RTL", "x", 1, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("سلام")}, "xسلام", 5},
		{"pasted CRLF lines", "", 0, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\r\nb\x1b[31m")}, "a\nb", 3},
		{"other keys do nothing", "ab", 1, k(tea.KeyF5), "ab", 1},
	}
	for _, c := range cases {
		m := press(multi(c.text, c.pos), c.key)
		if m.Value() != c.want || m.caret != c.wantCaret {
			t.Errorf("%s: %q caret %d, want %q caret %d", c.name, m.Value(), m.caret, c.want, c.wantCaret)
		}
	}
}

func TestMultilineSubmitAndCancel(t *testing.T) {
	m := multi("a\nb", 0)
	_, cmd := m.Update(k(tea.KeyCtrlS))
	if cmd == nil {
		t.Fatal("Ctrl+S did nothing")
	}
	if s, ok := cmd().(SubmitMsg); !ok || s.Value != "a\nb" {
		t.Errorf("Ctrl+S sent %#v", cmd())
	}
	for _, key := range []tea.KeyMsg{k(tea.KeyEsc), k(tea.KeyCtrlC)} {
		_, cmd := m.Update(key)
		if cmd == nil {
			t.Fatalf("%v did nothing", key)
		}
		if _, ok := cmd().(CloseMsg); !ok {
			t.Errorf("%v sent %T", key, cmd())
		}
	}
	// The mouse is not used by the names editor.
	if _, cmd := m.HandleMouse(10, 10, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil {
		t.Error("click in the names editor did something")
	}
}

func TestMultilineBoxScrollsToTheCaret(t *testing.T) {
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, "name-"+string(rune('A'+i%26))+strings.Repeat("x", i))
	}
	text := strings.Join(lines, "\n")
	m := multi(text, len([]rune(text))) // caret on the last line
	m.SetSize(50, 20)
	box := m.Box()
	plain := ansi.Strip(box)
	if !strings.Contains(plain, "name-N") || strings.Contains(plain, "name-A\n") {
		t.Errorf("the caret's line is not on screen:\n%s", plain)
	}
	for i, l := range strings.Split(box, "\n") {
		if w := ansi.StringWidth(l); w > 50-4 {
			t.Errorf("line %d is %d wide in a 50-wide window", i, w)
		}
	}
	if !strings.Contains(plain, "…") {
		t.Error("long lines are not cut with …")
	}
	if !strings.Contains(plain, "Ctrl+S preview") {
		t.Error("footer missing")
	}
}

func TestPromptBoxFitsNarrowAndWideWindows(t *testing.T) {
	for _, w := range []int{40, 50, 64, 120} {
		m := New("Rename", strings.Repeat("A long label ", 10), strings.Repeat("value", 30))
		m.SetSize(w, 30)
		box := m.Box()
		want := ansi.StringWidth(strings.Split(box, "\n")[0])
		if want > w {
			t.Errorf("window %d: box is %d wide", w, want)
		}
		for i, l := range strings.Split(box, "\n") {
			if lw := ansi.StringWidth(l); lw != want {
				t.Errorf("window %d: line %d is %d wide, box %d", w, i, lw, want)
			}
		}
		plain := ansi.Strip(box)
		if !strings.Contains(plain, "value▏") || strings.Count(plain, "value") >= 30 {
			t.Errorf("window %d: a long value does not show its end", w)
		}
		if !strings.Contains(plain, "A long label") || !strings.Contains(plain, "…") {
			t.Errorf("window %d: long label not cut", w)
		}
	}
}

func TestPromptWithoutALabel(t *testing.T) {
	m := New("Rename", "  ", "x")
	m.SetSize(100, 30)
	lines := strings.Split(ansi.Strip(m.Box()), "\n")
	if strings.TrimSpace(strings.Trim(lines[3], "│")) != "" {
		t.Errorf("row under the hairline = %q, want blank", lines[3])
	}
}

func TestPromptIconFollowsTheIconMode(t *testing.T) {
	saved := theme.CurrentIconMode
	t.Cleanup(func() { theme.CurrentIconMode = saved })
	m := New("Rename", "", "x")
	m.SetSize(100, 30)
	for mode, want := range map[theme.IconMode]string{
		theme.IconModeNerdFont: " Rename",
		theme.IconModeUnicode:  "❯ Rename",
		theme.IconModeASCII:    "> Rename",
	} {
		theme.CurrentIconMode = mode
		if !strings.Contains(ansi.Strip(m.View()), want) {
			t.Errorf("mode %v: header lacks %q", mode, want)
		}
	}
}

func TestWrapWordsCutsLongWords(t *testing.T) {
	got := wrapWords("short "+strings.Repeat("x", 25)+" end\n\nnext", 10)
	want := []string{"short", "xxxxxxxxxx", "xxxxxxxxxx", "xxxxx end", "", "next"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("wrapWords = %q, want %q", got, want)
	}
}

func TestPromptMouseIgnoresOtherClicks(t *testing.T) {
	m := New("Rename", "Name:", "x")
	if _, cmd := m.HandleMouse(1, 1, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil {
		t.Error("click before SetSize did something")
	}
	m.SetSize(120, 40)
	if _, cmd := m.HandleMouse(60, 20, tea.MouseActionRelease, tea.MouseButtonLeft); cmd != nil {
		t.Error("release did something")
	}
	if _, cmd := m.HandleMouse(60, 20, tea.MouseActionPress, tea.MouseButtonRight); cmd != nil {
		t.Error("right click did something")
	}
	// The footer row, but right of the hints.
	bx, by := m.Origin()
	lines := strings.Split(m.Box(), "\n")
	y := by + len(lines) - 2
	if _, cmd := m.HandleMouse(bx+ansi.StringWidth(lines[0])-3, y, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil {
		t.Error("click on the empty end of the footer did something")
	}
	// Enter hint with an empty value does nothing.
	e := New("Rename", "Name:", "  ")
	e.SetSize(120, 40)
	bx, by = e.Origin()
	lines = strings.Split(e.Box(), "\n")
	foot := ansi.Strip(lines[len(lines)-2])
	i := strings.Index(foot, "Enter")
	if _, cmd := e.HandleMouse(bx+ansi.StringWidth(foot[:i]), by+len(lines)-2, tea.MouseActionPress, tea.MouseButtonLeft); cmd != nil {
		t.Errorf("Enter hint on a blank value sent %#v", cmd())
	}
}

func TestPromptAltBracketAndEscapesAreDropped(t *testing.T) {
	m := New("Find", "", "")
	m = press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("["), Alt: true})
	if m.Value() != "" {
		t.Errorf("Alt+[ reached the field: %q", m.Value())
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\x1b[1;2Hb[<1;2;3mc\x00")})
	if m.Value() != "abc" {
		t.Errorf("value = %q, want abc", m.Value())
	}
	if NewWithDefault("T", "L", "v").Value() != "v" {
		t.Error("NewWithDefault lost the value")
	}
	if New("T", "L", "").Init() != nil {
		t.Error("Init returned a command")
	}
	if got := sanitizeInput("[<1;x"); got != "[<1;x" {
		t.Errorf("a typed [< that is not a mouse code = %q", got)
	}
	if got := sanitizeInput("z\x1b[9"); got != "z" {
		t.Errorf("cut-off escape = %q", got)
	}
}

func TestCompletePathFollowsLinksAndHome(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "target"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "linkdir")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "linkfile-target"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "linkfile-target"), filepath.Join(dir, "lfile")); err != nil {
		t.Fatal(err)
	}
	if got := CompletePath(filepath.Join(dir, "linkd")); got != filepath.Join(dir, "linkdir")+"/" {
		t.Errorf("link to a folder = %q, want a trailing /", got)
	}
	if got := CompletePath(filepath.Join(dir, "lfi")); got != filepath.Join(dir, "lfile") {
		t.Errorf("link to a file = %q, want no /", got)
	}

	t.Setenv("HOME", dir)
	if got := CompletePath("~/tar"); got != "~/target/" {
		t.Errorf("~/tar = %q, want ~/target/", got)
	}
	if got := CompletePath("~/nothing-here"); got != "~/nothing-here" {
		t.Errorf("no match = %q", got)
	}
	if got := CompletePath(filepath.Join(dir, "missing-dir", "x")); got != filepath.Join(dir, "missing-dir", "x") {
		t.Errorf("missing folder = %q", got)
	}
	t.Setenv("HOME", "")
	if got := CompletePath("~/tar"); got != "~/tar" {
		t.Errorf("no home: %q, want it unchanged", got)
	}
}

func TestCompletePathInTheWorkingFolder(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, n := range []string{"report-a.txt", "report-b.txt"} {
		if err := os.WriteFile(n, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := CompletePath("rep"); got != "report-" {
		t.Errorf("two matches = %q, want their shared start report-", got)
	}
}
