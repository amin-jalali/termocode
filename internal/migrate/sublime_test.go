package migrate

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestSublimeJSONCSettingsKeysAndPackages(t *testing.T) {
	d, err := ReadSublime(filepath.Join("testdata", "sublime-linux"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]any{}
	for _, s := range d.Settings {
		got[s.Name] = s.Value
	}
	if got["tab_size"] != float64(4) || got["word_wrap"] != "auto" || got["save_on_focus_lost"] != true || got["font_size"] != float64(13) {
		t.Errorf("settings = %v", got)
	}
	if _, ok := got["color_scheme"]; ok {
		t.Errorf("color_scheme must be the theme, not a setting")
	}
	if d.Theme != "Monokai" {
		t.Errorf("theme = %q", d.Theme)
	}
	want := map[string]string{
		"show_overlay:command_palette": "ctrl+shift+p",
		"show_overlay:goto:@":          "ctrl+alt+s",
		"show_overlay:goto::":          "ctrl+alt+l",
		"show_overlay":                 "ctrl+alt+p",
		"show_panel:find_in_files":     "ctrl+alt+f",
		"toggle_side_bar":              "ctrl+k ctrl+b",
		"duplicate_line":               "ctrl+alt+d",
		"rename_something_odd":         "ctrl+alt+r",
		"toggle_comment:block":         "alt+shift+m",
	}
	gotKeys := map[string]string{}
	for _, k := range d.Keys {
		gotKeys[k.Command] = k.Key
	}
	if !reflect.DeepEqual(gotKeys, want) {
		t.Errorf("keys = %v\nwant %v", gotKeys, want)
	}
	if !hasSkipped(d, RowKey, "context") || !hasSkipped(d, RowKey, "no_keys_here") {
		t.Errorf("context / keyless entries not reported: %+v", d.Skipped)
	}
	if want := []string{"GitGutter", "LSP", "LSP-gopls", "Dracula Color Scheme", "SideBarEnhancements"}; !sameSet(d.Extensions, want) {
		t.Errorf("packages = %v", d.Extensions)
	}
	if !hasSkipped(d, RowExtension, "Package Control") {
		t.Errorf("the package manager itself should be reported: %+v", d.Skipped)
	}
}

func sameSet(a, b []string) bool {
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	return reflect.DeepEqual(x, y)
}

func TestSublimeMacFixture(t *testing.T) {
	d, err := ReadSublime(filepath.Join("testdata", "sublime-macos"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Theme != "Solarized (Dark)" || len(d.Keys) != 2 {
		t.Fatalf("%+v", d)
	}
	for _, k := range d.Keys {
		if k.Key == "super+p" && k.Command != "show_overlay" {
			t.Errorf("goto with show_files must stay the file picker: %+v", k)
		}
	}
}

func TestSublimeMalformedFilesGiveRows(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "Preferences.sublime-settings", `{"tab_size": `)
	writeAt(t, dir, "Default (Linux).sublime-keymap", `[{"keys": `)
	writeAt(t, dir, "Package Control.sublime-settings", `not json`)
	d, err := ReadSublime(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []RowKind{RowSetting, RowKey, RowExtension} {
		if !hasSkipped(d, kind, "not valid JSON") {
			t.Errorf("no error row for %s: %+v", kind, d.Skipped)
		}
	}
}

func TestSublimeAutoColorSchemeUsesDark(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "Preferences.sublime-settings", `{"color_scheme": "auto", "dark_color_scheme": "Mariana.sublime-color-scheme", "light_color_scheme": "Breakers.sublime-color-scheme"}`)
	d, _ := ReadSublime(dir)
	if d.Theme != "Mariana" {
		t.Fatalf("theme = %q", d.Theme)
	}
}

func TestSublimeDoesNotFollowLinksOutOfConfigDir(t *testing.T) {
	outside := t.TempDir()
	writeAt(t, outside, "p.json", `{"tab_size": 9}`)
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "p.json"), filepath.Join(dir, "Preferences.sublime-settings")); err != nil {
		t.Skip("symlinks not available:", err)
	}
	d, err := ReadSublime(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Settings) != 0 || !hasSkipped(d, RowSetting, "outside") {
		t.Fatalf("followed a link: %+v", d)
	}
}

func TestSublimeOversizeFileIsSkipped(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "Preferences.sublime-settings", `{"tab_size": 4}`+strings.Repeat(" ", maxConfigFile))
	d, err := ReadSublime(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Settings) != 0 || !hasSkipped(d, RowSetting, "larger than") {
		t.Fatalf("%+v", d)
	}
}

func TestDetectSublime(t *testing.T) {
	for _, tc := range []struct{ goos, st4, st3 string }{
		{"linux", ".config/sublime-text", ".config/sublime-text-3"},
		{"darwin", "Library/Application Support/Sublime Text", "Library/Application Support/Sublime Text 3"},
		{"windows", "AppData/Roaming/Sublime Text", "AppData/Roaming/Sublime Text 3"},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			home := t.TempDir()
			writeAt(t, home, filepath.Join(tc.st3, "Packages/User/Preferences.sublime-settings"), "{}")
			find := func() (Source, int) {
				var n int
				var src Source
				for _, s := range Detect(home, tc.goos, "") {
					if s.ID == "sublime" {
						n++
						src = s
					}
				}
				return src, n
			}
			s, n := find()
			if n != 1 || s.Title != "Sublime Text 3" || filepath.Base(filepath.Dir(filepath.Dir(s.Dir))) != filepath.Base(tc.st3) {
				t.Fatalf("ST3 only: %+v (%d)", s, n)
			}
			writeAt(t, home, filepath.Join(tc.st4, "Packages/User/Package Control.sublime-settings"), "{}")
			s, n = find()
			if n != 1 || s.Title != "Sublime Text 4" {
				t.Fatalf("ST4 should win: %+v (%d)", s, n)
			}
		})
	}
}

func TestDetectSublimeHonoursXDG(t *testing.T) {
	home, xdg := t.TempDir(), t.TempDir()
	writeAt(t, xdg, "sublime-text/Packages/User/Preferences.sublime-settings", "{}")
	for _, s := range Detect(home, "linux", xdg) {
		if s.ID == "sublime" {
			return
		}
	}
	t.Fatal("not found under XDG_CONFIG_HOME")
}
