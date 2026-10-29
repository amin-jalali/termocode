package shellint

import (
	"os"
	"path/filepath"
	"testing"
)

// Folders with spaces and non-ASCII names come back exactly, whether the
// shell percent-encodes them (in either hex case) or not.
func TestParseCwdSpacesAndUnicode(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b]7;file://h/home/u/caf%C3%A9 bar":   "/home/u/café bar",
		"\x1b]7;file://h/home/u/caf%c3%a9%20bar": "/home/u/café bar",
		"\x1b]7;file://h/home/u/日本 語\a":          "/home/u/日本 語",
		"\x1b]7;file://h/x%2":                    "/x%2",
		"\x1b]7;file://h/x%g0":                   "/x%g0",
	} {
		got, ok := Parse(in)
		if !ok || got.Kind != Cwd || got.Cwd != want {
			t.Errorf("Parse(%q) = %+v %v, want cwd %q", in, got, ok, want)
		}
	}
}

func TestDirFollowsXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/data")
	if got := Dir(); got != filepath.Join("/data", "termocode", "shell") {
		t.Errorf("XDG_DATA_HOME: %q", got)
	}
	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", home)
	if got := Dir(); got != filepath.Join(home, ".local", "share", "termocode", "shell") {
		t.Errorf("HOME: %q", got)
	}
	t.Setenv("HOME", "")
	if got := Dir(); got != "" {
		t.Errorf("no home: %q, want \"\"", got)
	}
}

func TestInstallWritesScriptsOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shell")
	if err := Install(""); err == nil {
		t.Fatal("no dir: want an error")
	}
	if err := Install(dir); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"termocode.bash", "zsh/.zshrc", "zsh/.zshenv", "fish/fish/vendor_conf.d/termocode.fish"} {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil || string(b) != Script(rel) || len(b) == 0 {
			t.Errorf("%s: %d bytes, %v", rel, len(b), err)
		}
	}
	// An unchanged file is not written again; a changed one is fixed.
	bash := filepath.Join(dir, "termocode.bash")
	old := []byte("# edited by hand\n")
	if err := os.WriteFile(bash, old, 0o644); err != nil {
		t.Fatal(err)
	}
	zshrc := filepath.Join(dir, "zsh", ".zshrc")
	st1, _ := os.Stat(zshrc)
	if err := Install(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(bash); string(b) != Script("termocode.bash") {
		t.Error("a changed script was not put back")
	}
	if st2, _ := os.Stat(zshrc); !os.SameFile(st1, st2) {
		t.Error("an unchanged script was rewritten")
	}
	if Script("no/such") != "" {
		t.Error("unknown script: want \"\"")
	}
}

func TestInstallFails(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Install(filepath.Join(file, "shell")); err == nil {
		t.Error("a file in the path: want an error")
	}
	// The script's place is taken by a folder: the rename fails and the
	// temp file goes away.
	shell := filepath.Join(dir, "shell")
	if err := os.MkdirAll(filepath.Join(shell, "termocode.bash", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(shell); err == nil {
		t.Error("folder in the way: want an error")
	}
	if _, err := os.Stat(filepath.Join(shell, "termocode.bash.tmp")); err == nil {
		t.Error("temp file left")
	}
	if os.Geteuid() != 0 {
		ro := filepath.Join(dir, "ro")
		if err := os.Mkdir(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
		if err := Install(ro); err == nil {
			t.Error("read-only folder: want an error")
		}
	}
}

func TestSupported(t *testing.T) {
	for shell, want := range map[string]bool{
		"/bin/bash": true, "-zsh": true, "/usr/local/bin/fish": true,
		"/bin/sh": false, "dash": false, "nu": false, "": false,
	} {
		if got := Supported(shell); got != want {
			t.Errorf("Supported(%q) = %v", shell, got)
		}
	}
}
