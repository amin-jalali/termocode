package cfgfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, b)
	}
	return m
}

func noTemps(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "config.json" {
			t.Errorf("leftover file %s", e.Name())
		}
	}
}

func TestUpdateKeepsOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"theme":"x","tab_size":4,"nested":{"a":[1,2]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Update(path, func(m map[string]any) error { m["mcp_server"] = "on"; return nil }); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, path)
	if m["theme"] != "x" || m["tab_size"] != float64(4) || m["mcp_server"] != "on" || m["nested"] == nil {
		t.Fatalf("keys lost: %v", m)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode changed to %v", st.Mode().Perm())
	}
	noTemps(t, filepath.Dir(path))
}

func TestUpdateCreatesFileAndFolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	if err := Update(path, func(m map[string]any) error { m["a"] = 1; return nil }); err != nil {
		t.Fatal(err)
	}
	if readJSON(t, path)["a"] != float64(1) {
		t.Fatal("not written")
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o644 {
		t.Fatalf("new file mode %v", st.Mode().Perm())
	}
}

func TestUpdateInvalidJSONUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	const bad = `{"theme": "x", // user comment` + "\n"
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	err := Update(path, func(m map[string]any) error { called = true; return nil })
	if err == nil || called {
		t.Fatalf("want an error without calling fn, got %v (called %v)", err, called)
	}
	if b, _ := os.ReadFile(path); string(b) != bad {
		t.Fatalf("file changed: %q", b)
	}
	noTemps(t, dir)
}

func TestUpdateWriteFailureLeavesOldFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	const old = `{"theme":"x"}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("disk full")
	rename = func(string, string) error { return boom }
	defer func() { rename = os.Rename }()
	err := Update(path, func(m map[string]any) error { m["a"] = 1; return nil })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != old {
		t.Fatalf("file changed: %q", b)
	}
	noTemps(t, dir)
}

func TestUpdateRawKeepsValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"big":12345678901234567890}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpdateRaw(path, func(m map[string]json.RawMessage) error { m["theme"] = json.RawMessage(`"y"`); return nil }); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !json.Valid(b) || !strings.Contains(string(b), "12345678901234567890") {
		t.Fatalf("number re-encoded: %s", b)
	}
}

func TestUpdateReadOnlyFileStays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"a":1}`), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := Update(path, func(m map[string]any) error { m["b"] = 2; return nil }); err == nil {
		t.Fatal("a read-only file was replaced")
	}
	if b, _ := os.ReadFile(path); string(b) != `{"a":1}` {
		t.Fatalf("file changed: %s", b)
	}
	noTemps(t, filepath.Dir(path))
}

func TestWriteFileSaysWhyInPlainWords(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("runs as root: file modes do not stop root from writing")
	}
	dir := filepath.Join(t.TempDir(), "termocode")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	err := WriteFile(path, []byte(`{"a":1}`))
	if err == nil || !strings.HasPrefix(err.Error(), "no permission to write in ") || strings.Contains(err.Error()[:40], ".tmp") {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("the cause is lost: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "{}\n" {
		t.Errorf("config.json changed: %q", b)
	}
}

func TestWriteFileFullDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	old := rename
	rename = func(string, string) error { return &os.LinkError{Op: "rename", Err: syscall.ENOSPC} }
	t.Cleanup(func() { rename = old })
	err := WriteFile(path, []byte(`{}`))
	if err == nil || !strings.HasPrefix(err.Error(), "the disk is full; config.json was not changed") {
		t.Fatalf("err = %v", err)
	}
	if _, serr := os.Stat(path); serr == nil {
		t.Error("a failed write left config.json")
	}
}
