package explorer

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/anim"
	"github.com/amin-jalali/termocode/internal/theme"
)

// TestViewCacheMatchesDraw drives the explorer through random operations
// (keys, clicks, wheel, resize, meta, hidden files, reload after disk
// changes, reveal, expand, roots, selection, scroll animation, git status
// changed in place, theme / icon / colour-profile switches, View
// arguments) and checks after every step that the cached View equals a
// fresh draw. A cache key that misses an input fails here.
func TestViewCacheMatchesDraw(t *testing.T) {
	prevProfile := lipgloss.ColorProfile()
	prevIcons := theme.CurrentIconMode
	prevAnim := anim.Enabled
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(prevProfile)
		theme.CurrentIconMode = prevIcons
		anim.Enabled = prevAnim
		theme.ApplyTheme("vscode-dark-plus")
	})

	dir := t.TempDir()
	extra := t.TempDir()
	var paths []string
	mk := func(rel string, isDir bool) {
		p := filepath.Join(dir, rel)
		if isDir {
			_ = os.MkdirAll(p, 0o755)
		} else {
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			_ = os.WriteFile(p, []byte("x"), 0o644)
		}
		paths = append(paths, p)
	}
	for _, d := range []string{"a", "a/b", "c", ".hidden", "a/b/deep"} {
		mk(d, true)
	}
	for _, f := range []string{"main.go", "README.md", "a/x.go", "a/b/y.ts", "c/z.py", ".env", "a/b/deep/w.json"} {
		mk(f, false)
	}
	_ = os.WriteFile(filepath.Join(extra, "e.txt"), []byte("e"), 0o644)

	root, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := Model{root: root}
	m.SetSize(30, 20)
	git := GitStatus{}
	focused, active, dirty := true, "", false
	themes := []string{"vscode-dark-plus", "github-dark", "one-dark", "solarized-dark"}
	keys := []string{"j", "k", "down", "up", "g", "G", "home", "end", "enter", " ",
		"shift+down", "shift+up", "ctrl+@", "ctrl+a", "esc"}

	r := rand.New(rand.NewSource(7))
	for step := 0; step < 1500; step++ {
		_ = m.View(focused, active, dirty, git) // fill the cache with the old state
		op := r.Intn(15)
		switch op {
		case 0:
			k := keys[r.Intn(len(keys))]
			var msg tea.KeyMsg
			switch k {
			case "down":
				msg = tea.KeyMsg{Type: tea.KeyDown}
			case "up":
				msg = tea.KeyMsg{Type: tea.KeyUp}
			case "home":
				msg = tea.KeyMsg{Type: tea.KeyHome}
			case "end":
				msg = tea.KeyMsg{Type: tea.KeyEnd}
			case "enter":
				msg = tea.KeyMsg{Type: tea.KeyEnter}
			case " ":
				msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
			case "shift+down":
				msg = tea.KeyMsg{Type: tea.KeyShiftDown}
			case "shift+up":
				msg = tea.KeyMsg{Type: tea.KeyShiftUp}
			case "ctrl+@":
				msg = tea.KeyMsg{Type: tea.KeyCtrlAt}
			case "ctrl+a":
				msg = tea.KeyMsg{Type: tea.KeyCtrlA}
			case "esc":
				msg = tea.KeyMsg{Type: tea.KeyEsc}
			default:
				msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
			}
			m, _ = m.Update(msg)
		case 1:
			types := []tea.MouseEventType{tea.MouseLeft, tea.MouseWheelUp, tea.MouseWheelDown}
			m, _ = m.HandleMouse(r.Intn(30), r.Intn(m.h+1), types[r.Intn(3)], r.Intn(4) == 0, r.Intn(4) == 0)
		case 2:
			m.SetSize(10+r.Intn(40), 3+r.Intn(30))
		case 3:
			m.SetMeta(fmt.Sprintf("WS%d", r.Intn(3)), []string{"", "main", "feature/long-branch-name"}[r.Intn(3)], r.Intn(2) == 0, r.Intn(50))
		case 4:
			m.ToggleHidden()
		case 5:
			name := filepath.Join(dir, []string{"a", "c", "a/b", "."}[r.Intn(4)], fmt.Sprintf("n%d.go", r.Intn(5)))
			if r.Intn(2) == 0 {
				_ = os.WriteFile(name, []byte("n"), 0o644)
			} else {
				_ = os.Remove(name)
			}
			m.Reload()
		case 6:
			m.RevealPath(paths[r.Intn(len(paths))])
		case 7:
			m.ExpandPaths([]string{paths[r.Intn(len(paths))]})
		case 8:
			if r.Intn(2) == 0 {
				_ = m.AddRoot(extra)
			} else {
				m.RemoveRoot(extra)
			}
		case 9:
			switch r.Intn(3) {
			case 0:
				m.ClearSelection()
			case 1:
				m.ToggleSelection(paths[r.Intn(len(paths))])
			case 2:
				m.SelectRange(paths[r.Intn(len(paths))], paths[r.Intn(len(paths))])
			}
		case 10:
			anim.Enabled = r.Intn(2) == 0
			m.AnimStep()
		case 11: // git status changed in place (same map)
			p := paths[r.Intn(len(paths))]
			if r.Intn(2) == 0 {
				git[p] = []string{"M", "A", "U", "D", "C", "I"}[r.Intn(6)]
			} else {
				delete(git, p)
			}
		case 12:
			switch r.Intn(3) {
			case 0:
				theme.ApplyTheme(themes[r.Intn(len(themes))])
			case 1:
				theme.CurrentIconMode = []theme.IconMode{theme.IconModeASCII, theme.IconModeUnicode, theme.IconModeNerdFont}[r.Intn(3)]
			case 2:
				lipgloss.SetColorProfile([]termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.ANSI}[r.Intn(3)])
			}
		case 13:
			focused = r.Intn(2) == 0
			dirty = r.Intn(2) == 0
			active = []string{"", paths[r.Intn(len(paths))]}[r.Intn(2)]
		case 14:
			// Nothing changed: the cache must serve the same frame.
		}
		got := m.View(focused, active, dirty, git)
		want := m.draw(focused, active, dirty, git)
		if got != want {
			t.Fatalf("step %d (op %d): cached View differs from a fresh draw", step, op)
		}
	}
}

// TestCountVisibleFilesCached: the cached count follows tree changes.
func TestCountVisibleFilesCached(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.go"), nil, 0o644)
	_ = os.Mkdir(filepath.Join(dir, "d"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "d", "b.go"), nil, 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".h"), nil, 0o644)
	root, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := Model{root: root}
	m.SetSize(30, 20)
	check := func(step string) {
		t.Helper()
		if got, want := m.CountVisibleFiles(), m.countVisibleFiles(); got != want {
			t.Fatalf("%s: CountVisibleFiles = %d, want %d", step, got, want)
		}
	}
	check("start")
	m.RevealPath(filepath.Join(dir, "d", "b.go"))
	check("expand")
	m.ToggleHidden()
	check("hidden")
	_ = os.WriteFile(filepath.Join(dir, "c.go"), nil, 0o644)
	m.Reload()
	check("reload")
}
