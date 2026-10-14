package activity

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/theme"
)

// TestViewCacheFollowsChanges: View is cached, but a change of the active
// view, the height, the icon set or the theme draws it again.
func TestViewCacheFollowsChanges(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	oldIcons := theme.CurrentIconMode
	t.Cleanup(func() { lipgloss.SetColorProfile(old); theme.CurrentIconMode = oldIcons })

	m := New()
	m.SetHeight(20)
	a := m.View()
	if m.View() != a || a != m.draw() {
		t.Fatal("cached view differs from a fresh draw")
	}
	m.SetActive(ViewGit)
	if v := m.View(); v == a || v != m.draw() {
		t.Error("active view change not drawn")
	}
	m.SetHeight(21)
	if v := m.View(); v != m.draw() {
		t.Error("height change not drawn")
	}
	theme.CurrentIconMode = theme.IconModeASCII
	if v := m.View(); v != m.draw() {
		t.Error("icon mode change not drawn")
	}
	before := m.View()
	if _, ok := theme.ApplyTheme("catppuccin-latte"); !ok {
		t.Fatal("no catppuccin-latte theme")
	}
	t.Cleanup(func() { theme.ApplyTheme("vscode-dark-plus") })
	if v := m.View(); v == before || v != m.draw() {
		t.Error("theme change not drawn")
	}
}
