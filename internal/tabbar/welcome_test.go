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
