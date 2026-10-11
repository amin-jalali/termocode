package specui

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/testkit"
)

// setSetting changes a setting through the Settings UI: filter the rows
// by name, Enter, then type the value into the prompt (or pick it from
// the choice list).
func setSetting(t *testing.T, d *testkit.Driver, name, value string) {
	t.Helper()
	d.Palette("open-settings-ui")
	mustSee(t, d, "⚙ Settings")
	d.Type(name)
	d.Key("enter")
	if strings.Contains(screen(d), "Setting") && strings.Contains(screen(d), name) && strings.Contains(screen(d), "enter  submit") {
		for range 12 {
			d.Key("backspace")
		}
	}
	d.Type(value)
	d.Key("enter")
}

// savedConfig reads the sandboxed config.json.
func savedConfig(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatalf("config.json: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("config.json: %v\n%s", err, b)
	}
	return m
}

// restart starts a new app with the config.json the old one saved.
func restart(t *testing.T, files map[string]string) *testkit.Driver {
	t.Helper()
	b, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatal(err)
	}
	return testkit.New(t, testkit.Options{W: 120, H: 36, Nvim: true, Files: files,
		Config: map[string]string{"config.json": string(b)}})
}

var settingFiles = map[string]string{
	"a.txt":    "alpha\n\tindented\n",
	"a.go":     "package a\n\nfunc A() {}\n",
	"long.txt": strings.Repeat("lorem ", 40) + "END\n",
	".hidden":  "secret\n",
}

// Spec: Given a Go file (tabs by its file type) and a text file,
// when each is the active editor,
// then the status bar shows that buffer's indentation, not a fixed value.
func TestStatusBarShowsBufferIndent(t *testing.T) {
	d := editorWith(t, settingFiles, "a.go", "func A")
	statusShows := func(s string) bool {
		return d.WaitFor(func() bool { return strings.Contains(statusBar(d), s) }, wait)
	}
	if !statusShows("Tabs: 4") {
		t.Errorf("Go file: status bar %q, want Tabs: 4", statusBar(d))
	}
	d.Action("open:a.txt")
	mustSee(t, d, "indented")
	if !statusShows("Spaces: 4") {
		t.Errorf("text file: status bar %q, want Spaces: 4", statusBar(d))
	}
}

// Spec: Given a text file,
// when the user sets tab_size to 2 in the Settings UI,
// then the status bar shows "Spaces: 2" at once, config.json holds
// tab_size 2, and after a restart the setting still applies.
func TestSettingTabSize(t *testing.T) {
	d := editorWith(t, settingFiles, "a.txt", "indented")
	setSetting(t, d, "tab_size", "2")
	mustSee(t, d, "Spaces: 2")
	if got := savedConfig(t)["tab_size"]; got != float64(2) {
		t.Fatalf("config.json tab_size = %v", got)
	}
	d = restart(t, settingFiles)
	d.Action("open:a.txt")
	mustSee(t, d, "indented")
	mustSee(t, d, "Spaces: 2")
}

// Spec: Given a file with one long line,
// when the user turns word_wrap on in the Settings UI and opens the file,
// then the line wraps (its end is visible); after a restart it still
// wraps.
func TestSettingWordWrap(t *testing.T) {
	d := editorWith(t, settingFiles, "a.txt", "indented")
	setSetting(t, d, "word_wrap", "true")
	d.Action("open:long.txt")
	mustSee(t, d, "END")
	if got := savedConfig(t)["word_wrap"]; got != true {
		t.Fatalf("config.json word_wrap = %v", got)
	}
	d = restart(t, settingFiles)
	d.Action("open:long.txt")
	mustSee(t, d, "END")
}

// Spec: Given a workspace with a dot-file,
// when the user turns show_hidden on,
// then the explorer lists it, and turning it off hides it again.
func TestSettingShowHidden(t *testing.T) {
	d := editorWith(t, settingFiles, "a.txt", "indented")
	mustNotSee(t, d, ".hidden")
	setSetting(t, d, "show_hidden", "true")
	mustSee(t, d, ".hidden")
	setSetting(t, d, "show_hidden", "false")
	mustNotSee(t, d, ".hidden")
	if got := savedConfig(t)["show_hidden"]; got != false {
		t.Errorf("config.json show_hidden = %v", got)
	}
}

// Spec: Given the explorer with unicode icons,
// when the user picks icons = ascii,
// then the explorer's file icons change to ASCII tags at once and the
// choice is saved.
func TestSettingIcons(t *testing.T) {
	d := editorWith(t, settingFiles, "a.txt", "indented")
	setSetting(t, d, "icons", "unicode")
	before := lineWith(d, "a.go")
	setSetting(t, d, "icons", "ascii")
	after := lineWith(d, "a.go")
	if before == after {
		t.Errorf("explorer row did not change: %q", after)
	}
	if strings.ContainsFunc(after, func(r rune) bool { return r > 0x2fff && r != '▕' && r != '│' }) {
		t.Errorf("ascii icons still show symbols: %q", after)
	}
	if got := savedConfig(t)["icons"]; got != "ascii" {
		t.Errorf("config.json icons = %v", got)
	}
}
