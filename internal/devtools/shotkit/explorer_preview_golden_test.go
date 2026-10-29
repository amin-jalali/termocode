package shotkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/theme"
)

// This component scene uses only a temp workspace and a manual clock. It
// needs no editor process, repository setup, clipboard or settling sleeps.
func TestExplorerPreviewGolden(t *testing.T) {
	goldenPath, err := filepath.Abs(filepath.Join(goldenDir, "explorer-preview-150x42.ans"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Chdir(dir)
	for name, body := range map[string]string{"main.go": "package main\n\nfunc main() {\n    println(\"Hello\")\n}\n", "README.md": "# A small project\n", ".env": "private"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	oldProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(oldProfile) })
	oldTheme, _ := theme.LookupTheme("")
	theme.ApplyTheme("tokyo-night")
	t.Cleanup(func() { theme.SetPalette(oldTheme.Palette) })
	oldIcons := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeASCII
	t.Cleanup(func() { theme.CurrentIconMode = oldIcons })
	t.Cleanup(clock.Set(func() time.Time { return DemoNow }))
	t.Cleanup(clock.SetAfter(func(time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); ch <- clock.Now(); return ch }))
	m := explorer.New()
	m.SetSize(150, 42)
	m.SetTerminalWidth(150)
	m.SetMeta("workspace", "", false, 2)
	m.RevealPath(filepath.Join(dir, "main.go"))
	cmd := m.TogglePreview()
	m, _ = m.Update(cmd())
	frame := m.View(true, "", false, nil) + "\n"
	rows := strings.Split(strings.TrimSuffix(frame, "\n"), "\n")
	if len(rows) != 42 {
		t.Fatal(len(rows))
	}
	for _, row := range rows {
		if ansi.StringWidth(row) != 150 {
			t.Fatal("frame width", ansi.StringWidth(row))
		}
	}
	path := goldenPath
	if *update || os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(frame), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != frame {
		t.Fatal("explorer preview golden differs; run with -update")
	}
}
