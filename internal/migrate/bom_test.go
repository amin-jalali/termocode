package migrate

import (
	"os"
	"path/filepath"
	"testing"
)

const bom = "\xef\xbb\xbf"

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Editors on Windows often save JSON with a byte order mark. The import
// must read those files like any other.
func TestJSONFilesWithByteOrderMark(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "settings.json"), bom+`{"editor.tabSize": 3}`)
	write(t, filepath.Join(dir, "keybindings.json"), bom+`[{"key":"ctrl+k","command":"x"}]`)
	d, err := ReadVSCode(dir)
	if err != nil {
		t.Fatalf("ReadVSCode: %v", err)
	}
	if len(d.Settings) != 1 || len(d.Keys) != 1 {
		t.Fatalf("doc %+v", d)
	}

	zed := t.TempDir()
	write(t, filepath.Join(zed, "settings.json"), bom+`{"tab_size": 2}`)
	if d, err := ReadZed(zed); err != nil || len(d.Settings) != 1 {
		t.Fatalf("ReadZed: %+v %v", d, err)
	}

	subl := t.TempDir()
	write(t, filepath.Join(subl, "Preferences.sublime-settings"), bom+`{"tab_size": 4}`)
	if d, err := ReadSublime(subl); err != nil || len(d.Settings) != 1 || len(d.Skipped) != 0 {
		t.Fatalf("ReadSublime: %+v %v", d, err)
	}

	one := filepath.Join(t.TempDir(), "settings.json")
	write(t, one, bom+`{"editor.tabSize": 3}`)
	if _, d, err := ReadFile(one); err != nil || len(d.Settings) != 1 {
		t.Fatalf("ReadFile: %+v %v", d, err)
	}
	pick := filepath.Join(t.TempDir(), "Preferences.sublime-settings")
	write(t, pick, bom+`{"tab_size": 4}`)
	if _, d, err := ReadFile(pick); err != nil || len(d.Settings) != 1 {
		t.Fatalf("ReadFile sublime: %+v %v", d, err)
	}
}
