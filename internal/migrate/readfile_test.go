package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadFileChoosesReaderByNameAndContent(t *testing.T) {
	cases := []struct {
		name, body, wantID     string
		wantSettings, wantKeys int
		wantTheme              string
	}{
		{"settings.json", "// c\n{\"editor.tabSize\": 2, \"workbench.colorTheme\": \"One Dark Pro\",}", "vscode", 1, 0, "One Dark Pro"},
		{"keybindings.json", `[{"key":"ctrl+p","command":"workbench.action.quickOpen"}]`, "vscode", 0, 1, ""},
		{"settings.json", `{"tab_size":2,"theme":"Ayu Dark"}`, "zed", 1, 0, "Ayu Dark"},
		{"keymap.json", `[{"context":"Editor","bindings":{"ctrl+p":"editor::OpenFile"}}]`, "zed", 0, 1, ""},
		{"settings (1).json", `{"editor.tabSize": 4}`, "vscode", 1, 0, ""},
		{"config.toml", "theme = \"onedark\"\n[editor]\nline-number = \"relative\"\n", "helix", 1, 0, "onedark"},
		{"Preferences.sublime-settings", `{"tab_size": 4, "color_scheme": "Packages/Color Scheme - Default/Monokai.sublime-color-scheme"}`, "sublime", 1, 0, "Monokai"},
		{"work.sublime-settings", `{"tab_size": 3}`, "sublime", 1, 0, ""},
		{"Default (OSX).sublime-keymap", `[{"keys":["ctrl+p"],"command":"show_overlay","args":{"overlay":"goto","show_files":true}}]`, "sublime", 0, 1, ""},
		{"Personal.xml", `<keymap version="1" name="Personal"><action id="GotoFile"><keyboard-shortcut first-keystroke="control alt P"/></action></keymap>`, "jetbrains", 0, 1, ""},
	}
	for _, tc := range cases {
		t.Run(tc.wantID+"/"+tc.name, func(t *testing.T) {
			p := writeTemp(t, tc.name, tc.body)
			src, doc, err := ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if src.ID != tc.wantID {
				t.Errorf("source id %q, want %q", src.ID, tc.wantID)
			}
			if src.Dir != filepath.Dir(p) || !strings.Contains(src.Title, tc.name) {
				t.Errorf("source %+v", src)
			}
			if len(doc.Settings) != tc.wantSettings || len(doc.Keys) != tc.wantKeys || doc.Theme != tc.wantTheme {
				t.Errorf("document %+v", doc)
			}
		})
	}
}

func TestReadFileReadsOnlyTheChosenFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"editor.tabSize": 2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keybindings.json"), []byte(`[{"key":"ctrl+p","command":"workbench.action.quickOpen"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc, err := ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Keys) != 0 {
		t.Errorf("sibling keybindings.json was read: %+v", doc.Keys)
	}
}

func TestReadFileWritesNothingNextToIt(t *testing.T) {
	p := writeTemp(t, "Preferences.sublime-settings", `{"tab_size": 4}`)
	before, _ := os.ReadDir(filepath.Dir(p))
	if _, _, err := ReadFile(p); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadDir(filepath.Dir(p))
	if len(before) != len(after) {
		t.Errorf("folder changed: %d -> %d entries", len(before), len(after))
	}
}

func TestReadFileRefusals(t *testing.T) {
	dir := t.TempDir()
	big := writeTemp(t, "settings.json", "{\"a\":\""+strings.Repeat("x", maxConfigFile)+"\"}")
	cases := []struct{ name, path, want string }{
		{"folder", dir, "folder"},
		{"missing", filepath.Join(dir, "nope.json"), "cannot be read"},
		{"unknown", writeTemp(t, "notes.txt", "hello"), "not a supported"},
		{"unknown json", writeTemp(t, "data.json", `"just a string"`), "not a supported"},
		{"oversize", big, "1 MB"},
		{"bad json", writeTemp(t, "settings.json", `{"a": `), "not valid"},
		{"bad sublime", writeTemp(t, "x.sublime-settings", `{"a": `), "not valid"},
		{"bad keymap", writeTemp(t, "x.sublime-keymap", `{"a": 1}`), "not valid"},
		{"bad xml", writeTemp(t, "k.xml", `<keymap`), "not valid"},
		{"other xml", writeTemp(t, "o.xml", `<application/>`), "not a JetBrains keymap"},
		{"empty path", "", "no file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ReadFile(tc.path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err %v, want it to mention %q", err, tc.want)
			}
		})
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
}

func TestReadFileRefusesADevice(t *testing.T) {
	if _, err := os.Stat("/dev/null"); err != nil {
		t.Skip("no /dev/null")
	}
	if _, _, err := ReadFile("/dev/null"); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("err %v", err)
	}
}
