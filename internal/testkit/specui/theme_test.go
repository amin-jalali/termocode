package specui

import (
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/theme"
)

// pickTheme opens Preferences: Color Theme, types the name, presses Enter.
func pickTheme(t *testing.T, d interface {
	Palette(string) string
	Type(string) string
	Key(...string) string
}, name string) {
	t.Helper()
	d.Palette("pick-theme")
	d.Type(name)
	d.Key("enter")
}

// Spec: Given the default theme and an open Go file,
// when the user picks "GitHub Dark" in Preferences: Color Theme,
// then the editor text (nvim highlights) gets the theme's background,
// the choice is saved, and a terminal opened afterwards uses the theme's
// ANSI red.
func TestThemeSwitchRecolorsEditorAndTerminal(t *testing.T) {
	t.Cleanup(func() { theme.ApplyTheme("vscode-dark-plus") })
	d := termDriver(t)
	_, bg0, ok := colorsAt(d, "func main")
	if !ok {
		t.Fatalf("no editor text:\n%s", screen(d))
	}
	pickTheme(t, d, "GitHub Dark")
	want := theme.NvimColors("github-dark")
	var bg string
	d.WaitFor(func() bool { _, bg, _ = colorsAt(d, "func main"); return bg == want["bg"] }, wait)
	if bg != want["bg"] || bg == bg0 {
		t.Errorf("editor background = %s, want the theme's %s (was %s)", bg, want["bg"], bg0)
	}
	if got := savedConfig(t)["theme"]; got != "github-dark" {
		t.Errorf("config.json theme = %v", got)
	}
	openTerminal(t, d)
	run(t, d, `printf '\033[31mRED%s\033[0m\n' X`, "REDX")
	if fg, _, _ := colorsAt(d, "REDX"); fg != want["term_1"] {
		t.Errorf("terminal red = %s, want the theme's %s", fg, want["term_1"])
	}
}

// Spec: Given the Color Theme picker,
// when the user moves to another theme (live preview) and presses Esc,
// then the previous theme comes back and nothing is saved.
func TestThemePreviewEscRestores(t *testing.T) {
	t.Cleanup(func() { theme.ApplyTheme("vscode-dark-plus") })
	d := editorWith(t, nil, "main.go", "fmt.Println")
	_, bg0, _ := colorsAt(d, "func main")
	d.Palette("pick-theme")
	d.Key("down", "down")
	if _, bg, _ := colorsAt(d, "func main"); bg == bg0 {
		t.Errorf("moving in the theme list did not preview another theme (bg %s)", bg)
	}
	d.Key("esc")
	d.Settle(200 * time.Millisecond)
	if _, bg, _ := colorsAt(d, "func main"); bg != bg0 {
		t.Errorf("after Esc the editor background is %s, want %s", bg, bg0)
	}
	if got, ok := savedConfig(t)["theme"]; ok && got != "vscode-dark-plus" {
		t.Errorf("Esc saved theme %v", got)
	}
}
