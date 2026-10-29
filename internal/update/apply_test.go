package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestApplyAndRollback(t *testing.T) {
	f := newFakeRelease(t, "v0.2.0", "termocode v0.2.0")
	exe := installedBinary(t)
	st, err := prepare(t, f, exe)
	if err != nil {
		t.Fatal(err)
	}
	if CanRollback(exe) {
		t.Fatal("rollback offered before any update")
	}
	if err := st.Apply(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, exe), "v0.2.0") || !strings.Contains(read(t, OldPath(exe)), "v0.1.0") {
		t.Fatal("after Apply: want the new binary in place and the old one as termocode.old")
	}
	if info, _ := os.Stat(exe); info.Mode().Perm() != 0o750 {
		t.Fatalf("mode %v", info.Mode())
	}
	if _, err := os.Stat(st.NewPath); err == nil {
		t.Fatal("staged file still there")
	}
	if !CanRollback(exe) {
		t.Fatal("rollback not offered after an update")
	}
	if err := Rollback(exe); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, exe), "v0.1.0") {
		t.Fatal("rollback did not put the old binary back")
	}
	if CanRollback(exe) {
		t.Fatal("only one rollback")
	}
	if err := Rollback(exe); err == nil {
		t.Fatal("second rollback: want an error")
	}
	onlyFile(t, exe)
}

func TestApplyReplacesAnOlderBackup(t *testing.T) {
	f := newFakeRelease(t, "v0.2.0", "termocode v0.2.0")
	exe := installedBinary(t)
	if err := os.WriteFile(OldPath(exe), []byte("ancient"), 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := prepare(t, f, exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Apply(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, OldPath(exe)), "v0.1.0") {
		t.Fatal("termocode.old must be the binary just replaced")
	}
}

func TestApplyNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	exe := installedBinary(t)
	dir := filepath.Dir(exe)
	staged := filepath.Join(dir, ".termocode-new-x")
	_ = os.WriteFile(staged, []byte("new"), 0o750)
	_ = os.Chmod(dir, 0o555)
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	st := Staged{Tag: "v0.2.0", Exe: exe, NewPath: staged}
	if err := st.Apply(); err == nil {
		t.Fatal("read-only folder: want an error")
	}
	if !strings.Contains(read(t, exe), "v0.1.0") {
		t.Fatal("the old binary must stay")
	}
}
