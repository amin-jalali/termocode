package migrate

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeAt(t *testing.T, root, rel, data string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func keyMap(d Document) map[string][]string {
	out := map[string][]string{}
	for _, k := range d.Keys {
		out[k.Command] = append(out[k.Command], k.Key)
	}
	return out
}

func hasSkipped(d Document, kind RowKind, part string) bool {
	for _, s := range d.Skipped {
		if s.Kind == kind && strings.Contains(s.Source+" "+s.Reason, part) {
			return true
		}
	}
	return false
}

func TestJetBrainsParentChainAndActiveKeymap(t *testing.T) {
	d, err := ReadJetBrains(filepath.Join("testdata", "jetbrains-linux"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"GotoFile":             {"control alt P"},
		"GotoAction":           {"control shift A"},
		"ReformatCode":         {"control alt L"},
		"CommentByLineComment": {"control SLASH"},
		"FindInPath":           {"control K, control F"},
		"Unknown.Action":       {"alt F18"},
	}
	// SaveAll is bound in the parent, unbound by the child; "Unused" is not active.
	if got := keyMap(d); !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	if !hasSkipped(d, RowKey, "$default") {
		t.Errorf("built-in base keymap not reported: %+v", d.Skipped)
	}
}

func TestJetBrainsMacBuiltInBaseByName(t *testing.T) {
	d, err := ReadJetBrains(filepath.Join("testdata", "jetbrains-macos"))
	if err != nil {
		t.Fatal(err)
	}
	if got := keyMap(d)["GotoAction"]; !reflect.DeepEqual(got, []string{"meta shift A"}) {
		t.Fatalf("keys = %v", d.Keys)
	}
	if !hasSkipped(d, RowKey, "Mac OS X 10.5+") {
		t.Errorf("built-in base not named: %+v", d.Skipped)
	}
	if d.Theme != "Solarized Dark" {
		t.Errorf("theme = %q", d.Theme)
	}
}

func TestJetBrainsMissingParentAndLoop(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "options/keymap.xml", `<application><component name="KeymapManager"><active_keymap name="Mine"/></component></application>`)
	writeAt(t, dir, "keymaps/Mine.xml", `<keymap name="Mine" parent="Gone"><action id="GotoFile"><keyboard-shortcut first-keystroke="control P"/></action></keymap>`)
	d, err := ReadJetBrains(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Keys) != 1 || !hasSkipped(d, RowKey, "Gone") {
		t.Fatalf("missing parent: keys=%v skipped=%+v", d.Keys, d.Skipped)
	}
	writeAt(t, dir, "keymaps/Mine.xml", `<keymap name="Mine" parent="Other"><action id="GotoFile"><keyboard-shortcut first-keystroke="control P"/></action></keymap>`)
	writeAt(t, dir, "keymaps/Other.xml", `<keymap name="Other" parent="Mine"/>`)
	d, err = ReadJetBrains(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Keys) != 1 || !hasSkipped(d, RowKey, "loop") {
		t.Fatalf("parent loop: keys=%v skipped=%+v", d.Keys, d.Skipped)
	}
}

func TestJetBrainsMalformedXMLGivesRowsNotPanics(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "options/keymap.xml", `<application><component name="KeymapManager"><active_keymap name="Mine"/></component></application>`)
	writeAt(t, dir, "keymaps/Mine.xml", `<keymap name="Mine"><action id="x">`)
	writeAt(t, dir, "options/editor.xml", `<application><component`)
	writeAt(t, dir, "options/laf.xml", `not xml at all`)
	writeAt(t, dir, "plugins/p/META-INF/plugin.xml", `<idea-plugin><id>`)
	d, err := ReadJetBrains(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []RowKind{RowKey, RowSetting, RowTheme} {
		if !hasSkipped(d, kind, "not valid XML") {
			t.Errorf("no error row for %s: %+v", kind, d.Skipped)
		}
	}
	// A broken descriptor falls back to the folder name.
	if !reflect.DeepEqual(d.Extensions, []string{"p"}) {
		t.Errorf("extensions = %v", d.Extensions)
	}
}

func TestJetBrainsSettingsThemeAndPlugins(t *testing.T) {
	d, err := ReadJetBrains(filepath.Join("testdata", "jetbrains-linux"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]any{}
	for _, s := range d.Settings {
		got[s.Name] = s.Value
	}
	for name, want := range map[string]any{"USE_SOFT_WRAPS": true, "TAB_SIZE": "4", "TAB_SIZE (Python)": "8", "FONT_SIZE": "15", "SAVE_FILES_ON_FRAME_DEACTIVATION": "true"} {
		if v, ok := got[name]; !ok || v != want {
			t.Errorf("setting %s = %v (present %v), want %v; all: %v", name, v, ok, want, got)
		}
	}
	if d.Theme != "Darcula" {
		t.Errorf("theme = %q", d.Theme)
	}
	if want := []string{"IdeaVIM", "localtool", "org.jetbrains.plugins.go"}; !reflect.DeepEqual(d.Extensions, want) {
		t.Errorf("extensions = %v, want %v", d.Extensions, want)
	}
	if !hasSkipped(d, RowExtension, "com.github.copilot") {
		t.Errorf("disabled plugin not reported: %+v", d.Skipped)
	}
}

func TestJetBrainsSoftWrapOnlyConsoleMeansOff(t *testing.T) {
	d, _ := ReadJetBrains(filepath.Join("testdata", "jetbrains-macos"))
	found := false
	for _, s := range d.Settings {
		if s.Name == "USE_SOFT_WRAPS" {
			found = true
			if s.Value != false {
				t.Fatalf("CONSOLE-only wrap must be false: %v", s)
			}
		}
	}
	if !found {
		t.Fatal("no wrap setting")
	}
}

func TestJetBrainsPluginJarMetadata(t *testing.T) {
	dir := t.TempDir()
	jar := filepath.Join(dir, "plugins", "some-folder", "lib", "plugin.jar")
	if err := os.MkdirAll(filepath.Dir(jar), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(jar)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create("META-INF/plugin.xml")
	_, _ = w.Write([]byte(`<idea-plugin><id>org.rust.lang</id><name>Rust</name></idea-plugin>`))
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	d, err := ReadJetBrains(dir)
	if err != nil || !reflect.DeepEqual(d.Extensions, []string{"org.rust.lang"}) {
		t.Fatalf("%v %+v", err, d)
	}
}

func TestJetBrainsDoesNotFollowLinksOutOfConfigDir(t *testing.T) {
	outside := t.TempDir()
	writeAt(t, outside, "secret.xml", `<application><component name="EditorSettings"><option name="LEAK" value="1"/></component></application>`)
	writeAt(t, outside, "plug/META-INF/plugin.xml", `<idea-plugin><id>leaked.plugin</id></idea-plugin>`)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "options"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.xml"), filepath.Join(dir, "options", "editor.xml")); err != nil {
		t.Skip("symlinks not available:", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "plug"), filepath.Join(dir, "plugins", "plug")); err != nil {
		t.Fatal(err)
	}
	d, err := ReadJetBrains(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Settings) != 0 || len(d.Extensions) != 0 {
		t.Fatalf("followed a link out of the folder: %+v", d)
	}
	if !hasSkipped(d, RowSetting, "outside") || !hasSkipped(d, RowExtension, "link") {
		t.Fatalf("skip not reported: %+v", d.Skipped)
	}
}

func TestJetBrainsOversizeFileIsSkipped(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "options/editor.xml", `<application><component name="EditorSettings"><option name="A" value="1"/></component></application>`+strings.Repeat(" ", maxConfigFile))
	d, err := ReadJetBrains(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Settings) != 0 || !hasSkipped(d, RowSetting, "larger than") {
		t.Fatalf("oversize file read: %+v", d)
	}
}

func TestDetectJetBrainsNewestPerProduct(t *testing.T) {
	for _, tc := range []struct{ goos, root string }{
		{"linux", ".config/JetBrains"},
		{"darwin", "Library/Application Support/JetBrains"},
		{"windows", "AppData/Roaming/JetBrains"},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			home := t.TempDir()
			for _, n := range []string{"GoLand2023.3", "GoLand2024.1", "GoLand2024.10", "PyCharm2025.1", "IntelliJIdea2024.3", "Unrelated", "WebStorm2024.1"} {
				writeAt(t, home, filepath.Join(tc.root, n, "options", "editor.xml"), "<application/>")
			}
			// A product folder without any config is not offered.
			if err := os.MkdirAll(filepath.Join(home, tc.root, "Rider2024.1"), 0o700); err != nil {
				t.Fatal(err)
			}
			var titles []string
			for _, s := range Detect(home, tc.goos, "") {
				if s.ID == "jetbrains" {
					titles = append(titles, s.Title+"|"+filepath.Base(s.Dir))
				}
			}
			want := []string{"GoLand 2024.10|GoLand2024.10", "IntelliJ IDEA 2024.3|IntelliJIdea2024.3", "PyCharm 2025.1|PyCharm2025.1", "WebStorm 2024.1|WebStorm2024.1"}
			if !reflect.DeepEqual(titles, want) {
				t.Fatalf("got %v, want %v", titles, want)
			}
		})
	}
}

func TestDetectJetBrainsHonoursXDG(t *testing.T) {
	home, xdg := t.TempDir(), t.TempDir()
	writeAt(t, xdg, "JetBrains/PyCharm2024.2/options/editor.xml", "<application/>")
	var n int
	for _, s := range Detect(home, "linux", xdg) {
		if s.ID == "jetbrains" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("found %d", n)
	}
}
