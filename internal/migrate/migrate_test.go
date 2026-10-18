package migrate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSources(t *testing.T) {
	cases := []struct {
		name                            string
		fn                              func(string) (Document, error)
		fixture                         string
		wantSettings, wantKeys, wantExt int
	}{
		{"vscode", ReadVSCode, "vscode", 1, 1, 1}, {"zed", ReadZed, "zed", 1, 1, 0}, {"helix", ReadHelix, "helix", 2, 1, 0}, {"jetbrains", ReadJetBrains, "jetbrains", 1, 1, 0}, {"sublime", ReadSublime, "sublime", 1, 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, e := tc.fn(filepath.Join("testdata", tc.fixture))
			if e != nil {
				t.Fatal(e)
			}
			if len(d.Settings) < tc.wantSettings || len(d.Keys) < tc.wantKeys || len(d.Extensions) < tc.wantExt {
				t.Fatalf("unexpected document: %+v", d)
			}
		})
	}
}

func TestHelixSectionAndJetBrainsSecondStrokeArePreserved(t *testing.T) {
	helia, err := ReadHelix(filepath.Join("testdata", "helix"))
	if err != nil {
		t.Fatal(err)
	}
	var autoSave any
	for _, setting := range helia.Settings {
		if setting.Name == "[editor].auto-save" {
			autoSave = setting.Value
		}
	}
	if autoSave != "false" {
		t.Fatalf("Helix section/value lost: %+v", helia.Settings)
	}
	jb, err := ReadJetBrains(filepath.Join("testdata", "jetbrains"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, binding := range jb.Keys {
		if binding.Command == "GotoFile" && binding.Key == "control shift N, control shift S" {
			found = true
		}
	}
	if !found {
		t.Fatalf("JetBrains second stroke lost: %+v", jb.Keys)
	}
}

func TestDetectAndVim(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "Code", "User")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"editor.tabSize":4}`), 0600); err != nil {
		t.Fatal(err)
	}
	vim := filepath.Join(home, ".vimrc")
	if err := os.WriteFile(vim, []byte("set number\nmap <C-p> :Files\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sources := Detect(home, "linux", "")
	var code, v bool
	for _, s := range sources {
		code = code || s.ID == "vscode"
		v = v || s.ID == "vim"
	}
	if !code || !v {
		t.Fatalf("missing sources: %+v", sources)
	}
	d, e := ReadVim(vim)
	if e != nil || len(d.Keys) != 1 || len(d.Settings) != 1 {
		t.Fatalf("vim read: %+v, %v", d, e)
	}
}
