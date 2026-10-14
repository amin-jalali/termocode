package tabbar

import (
	"math/rand"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/theme"
)

// TestViewCacheMatchesDraw: after random changes to the tabs, the active
// tab, focus, width, scroll and theme, the cached View equals a fresh
// draw, and the cached tab cells equal freshly rendered ones.
func TestViewCacheMatchesDraw(t *testing.T) {
	prevProfile, prevIcons := lipgloss.ColorProfile(), theme.CurrentIconMode
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() {
		lipgloss.SetColorProfile(prevProfile)
		theme.CurrentIconMode = prevIcons
		theme.ApplyTheme("vscode-dark-plus")
	})
	paths := []string{"/a/main.go", "/b/main.go", "/a/README.md", "/c/x.ts", "", "/d/very-long-file-name-for-the-tab.py"}
	r := rand.New(rand.NewSource(3))
	m := New()
	m.SetWidth(80)
	var bufs []nvim.BufferInfo
	for step := 0; step < 2000; step++ {
		_ = m.View()
		switch r.Intn(7) {
		case 0:
			bufs = append(bufs, nvim.BufferInfo{ID: step + 1, Path: paths[r.Intn(len(paths))]})
			m.SetBuffers(bufs, bufs[r.Intn(len(bufs))].ID)
		case 1:
			if len(bufs) > 0 {
				i := r.Intn(len(bufs))
				bufs = append(bufs[:i:i], bufs[i+1:]...)
				active := 0
				if len(bufs) > 0 {
					active = bufs[0].ID
				}
				m.SetBuffers(bufs, active)
			}
		case 2:
			if len(bufs) > 0 {
				i := r.Intn(len(bufs))
				nb := append([]nvim.BufferInfo(nil), bufs...)
				nb[i].Modified = !nb[i].Modified
				bufs = nb
				m.SetBuffers(bufs, m.activeID)
			}
		case 3:
			m.SetWidth(10 + r.Intn(120))
		case 4:
			m.SetFocused(r.Intn(2) == 0)
		case 5:
			m.scrollByTabs(r.Intn(5) - 2)
		case 6:
			switch r.Intn(2) {
			case 0:
				theme.ApplyTheme([]string{"vscode-dark-plus", "github-dark", "one-dark"}[r.Intn(3)])
			case 1:
				theme.CurrentIconMode = []theme.IconMode{theme.IconModeASCII, theme.IconModeUnicode, theme.IconModeNerdFont}[r.Intn(3)]
			}
		}
		if got, want := m.View(), m.draw(); got != want {
			t.Fatalf("step %d: cached View differs from a fresh draw", step)
		}
		for i, b := range m.bufs {
			c := m.tabCells(i)
			f := renderTabCellsSfx(b, b.ID == m.activeID, m.suffix(b), m.isWelcome(b), 0)
			if len(c) != len(f) {
				t.Fatalf("step %d: cached cells differ", step)
			}
			for j := range c {
				if c[j] != f[j] {
					t.Fatalf("step %d: cached cells differ", step)
				}
			}
		}
	}
}
