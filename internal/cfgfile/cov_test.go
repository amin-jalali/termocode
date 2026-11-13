package cfgfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTildeHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := tildeHome(filepath.Join(home, "a", "b")); got != "~"+string(filepath.Separator)+filepath.Join("a", "b") {
		t.Fatalf("under home: %q", got)
	}
	if got := tildeHome(home); got != home {
		t.Fatalf("home itself: %q", got)
	}
	if got := tildeHome("/somewhere/else"); got != "/somewhere/else" {
		t.Fatalf("outside home: %q", got)
	}
	t.Setenv("HOME", "")
	if got := tildeHome("/x/y"); got != "/x/y" {
		t.Fatalf("no home: %q", got)
	}
}

// A write into a folder under HOME that cannot be written names the folder
// with ~ in the error.
func TestWriteFileErrorNamesFolderWithTilde(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "cfg")
	if err := os.Mkdir(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	err := WriteFile(filepath.Join(dir, "c.json"), []byte("{}"))
	if err == nil || !containsStr(err.Error(), "~/cfg") {
		t.Fatalf("error: %v", err)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// If any step on the temp file fails, WriteFile returns the error and
// leaves no temp file and no change behind.
func TestWriteFileTempFileStepsFail(t *testing.T) {
	old := createTemp
	t.Cleanup(func() { createTemp = old })
	dir := t.TempDir()
	target := filepath.Join(dir, "c.json")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(dir, pattern string) (*os.File, error){
		"chmod fails (file already closed)": func(d, p string) (*os.File, error) {
			f, err := os.CreateTemp(d, p)
			if err == nil {
				f.Close()
			}
			return f, err
		},
		"write fails (read-only handle)": func(d, p string) (*os.File, error) {
			f, err := os.CreateTemp(d, p)
			if err != nil {
				return nil, err
			}
			f.Close()
			return os.Open(f.Name())
		},
		"sync fails (a pipe)": func(d, p string) (*os.File, error) {
			_, w, err := os.Pipe()
			return w, err
		},
	}
	for name, mk := range cases {
		createTemp = mk
		if err := WriteFile(target, []byte("new")); err == nil {
			t.Errorf("%s: no error", name)
		}
		if b, _ := os.ReadFile(target); string(b) != "old" {
			t.Errorf("%s: target changed to %q", name, b)
		}
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".c.json-*"))
	if len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}
