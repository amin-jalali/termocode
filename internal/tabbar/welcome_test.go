package tabbar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/nvim"
)

func TestWelcomeTabAndDuplicateNames(t *testing.T) {
	m := New()
	m.SetWidth(120)
	m.SetBuffers([]nvim.BufferInfo{{ID: 1, Path: ""}}, 1)
	if got := ansi.Strip(m.View()); !strings.Contains(got, "Welcome") || strings.Contains(got, " . ") {
		t.Errorf("lone unnamed buffer should read Welcome: %q", got)
	}
	m.SetBuffers([]nvim.BufferInfo{
		{ID: 1, Path: "/p/internal/toast/model.go"},
		{ID: 2, Path: "/p/internal/statusbar/model.go"},
		{ID: 3, Path: "/p/README.md"},
	}, 2)
	got := ansi.Strip(m.View())
	for _, want := range []string{"model.go toast", "model.go statusbar", "README.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "README.md p") {
		t.Errorf("unique names must not get a suffix: %q", got)
	}
}

// A buffer may carry a Title: the tab shows it instead of the file name.
func TestTabShowsTitleInsteadOfFileName(t *testing.T) {
	m := New()
	m.SetWidth(120)
	m.SetBuffers([]nvim.BufferInfo{
		{ID: 1, Path: "/cfg/db/consoles/aaaa/175674a2dbc4.sql", Title: "SQL console · local-pg"},
		{ID: 2, Path: "/p/README.md"},
	}, 1)
	got := ansi.Strip(m.View())
	if !strings.Contains(got, "SQL console · local-pg") || strings.Contains(got, "175674a2dbc4") || !strings.Contains(got, "README.md") {
		t.Errorf("tabs = %q", got)
	}
}
