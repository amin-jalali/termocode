package preview_test

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/mdview"
	"github.com/amin-jalali/termocode/internal/preview"
	"github.com/amin-jalali/termocode/internal/testkit"
	"github.com/amin-jalali/termocode/internal/theme"
)

func readerFor(t *testing.T, renders *int) preview.Model {
	t.Helper()
	b, err := os.ReadFile("../devtools/shotkit/testdata/preview.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := preview.NewDoc(func(w int) []string {
		*renders++
		return mdview.Render(string(b), mdview.Options{Width: w})
	})
	return preview.NewReader("preview.md", "Preview", doc)
}

// The reader view renders once per width / theme: scrolling and redraws
// reuse the cached rows.
func TestReaderRendersOncePerWidthAndTheme(t *testing.T) {
	defer theme.ApplyTheme("vscode-dark-plus")
	n := 0
	m := readerFor(t, &n)
	m.SetSize(120, 30)
	_ = m.View()
	if n != 1 {
		t.Fatalf("renders after open = %d, want 1", n)
	}
	for _, k := range []string{"j", "j", "G", "k", "g"} {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		_ = m.View()
	}
	m, _ = m.Update(tea.MouseMsg{Type: tea.MouseWheelDown})
	_ = m.View()
	if n != 1 {
		t.Errorf("scrolling re-rendered the markdown: %d renders", n)
	}
	m.SetSize(80, 30)
	_ = m.View()
	if n != 2 {
		t.Errorf("resize: %d renders, want 2", n)
	}
	theme.ApplyTheme("catppuccin-latte")
	_ = m.View()
	_ = m.View()
	if n != 3 {
		t.Errorf("theme switch: %d renders, want 3", n)
	}
}

func TestReaderFrameInvariants(t *testing.T) {
	defer theme.ApplyTheme("vscode-dark-plus")
	old := theme.CurrentIconMode
	defer func() { theme.CurrentIconMode = old }()
	for _, th := range []string{"vscode-dark-plus", "catppuccin-latte"} {
		theme.ApplyTheme(th)
		for _, mode := range []theme.IconMode{theme.IconModeNerdFont, theme.IconModeUnicode, theme.IconModeASCII} {
			theme.CurrentIconMode = mode
			for _, sz := range [][2]int{{240, 62}, {100, 30}, {40, 12}, {12, 5}} {
				n := 0
				m := readerFor(t, &n)
				m.SetSize(sz[0], sz[1])
				for _, k := range []string{"", "G"} {
					if k != "" {
						m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
					}
					frame := m.View()
					if !testkit.CheckFrame(t, frame, sz[0], sz[1]) {
						t.Fatalf("%s mode %d %dx%d", th, mode, sz[0], sz[1])
					}
					if sz[0] >= 40 && !strings.Contains(ansi.Strip(strings.SplitN(frame, "\n", 2)[0]), "preview.md") {
						t.Errorf("header lost the file name")
					}
				}
			}
		}
	}
}
