package agentswap

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeVault map[string]string

func (v fakeVault) Get(n string) (string, bool, error) { s, ok := v[n]; return s, ok, nil }
func (v fakeVault) Set(n, s string) error              { v[n] = s; return nil }
func (v fakeVault) Delete(n string) error              { delete(v, n); return nil }

const secretWork = `{"token":"SECRET-WORK-1234"}`
const secretHome = `{"token":"SECRET-HOME-5678"}`

func setup(t *testing.T) (Swapper, string, fakeVault) {
	t.Helper()
	home := t.TempDir()
	v := fakeVault{}
	return Swapper{Home: home, Vault: v, GOOS: "linux", Running: func(string) bool { return false }}, home, v
}

func writeCred(t *testing.T, home, body string) string {
	t.Helper()
	p := filepath.Join(home, ".claude", ".credentials.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// AGX-14: capture the signed-in account, switch to another, undo.
func TestCaptureSwitchAndUndo(t *testing.T) {
	s, home, v := setup(t)
	p := writeCred(t, home, secretWork)
	if err := s.Capture("claude", "work"); err != nil {
		t.Fatal(err)
	}
	if len(v) != 1 {
		t.Fatalf("vault %v", v)
	}
	for name, val := range v {
		if name != "agent.account.claude.work" || strings.Contains(val, "SECRET-WORK") {
			t.Fatalf("stored %q = %q (a plain copy of the file must not be the stored value)", name, val)
		}
	}
	// The user signs in as another account; capture it too.
	if err := os.WriteFile(p, []byte(secretHome), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Capture("claude", "home"); err != nil {
		t.Fatal(err)
	}
	pv, err := s.Preview("claude", "work")
	if err != nil || pv.Path != p || pv.Size != len(secretWork) {
		t.Fatalf("preview %+v %v", pv, err)
	}
	if err := s.Apply("claude", "work"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != secretWork {
		t.Fatalf("file now %q", b)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	if b, _ := os.ReadFile(p + ".termocode-backup"); string(b) != secretHome {
		t.Fatalf("backup %q", b)
	}
	if err := s.Undo("claude"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != secretHome {
		t.Fatalf("after undo %q", b)
	}
	if _, err := os.Stat(p + ".termocode-backup"); !os.IsNotExist(err) {
		t.Fatal("the backup stays after an undo")
	}
	if err := s.Undo("claude"); err == nil {
		t.Fatal("a second undo has nothing to restore and must say so")
	}
}

func TestRefusedWhileTheCLIRuns(t *testing.T) {
	s, home, _ := setup(t)
	p := writeCred(t, home, secretWork)
	if err := s.Capture("claude", "work"); err != nil {
		t.Fatal(err)
	}
	s.Running = func(cli string) bool { return cli == "claude" }
	err := s.Apply("claude", "work")
	if !errors.Is(err, ErrRunning) {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != secretWork {
		t.Fatal("the file changed while the CLI runs")
	}
	if _, err := os.Stat(p + ".termocode-backup"); !os.IsNotExist(err) {
		t.Fatal("a backup was made for a refused swap")
	}
}

func TestNothingSecretInErrorsAndUnknownAccount(t *testing.T) {
	s, home, _ := setup(t)
	writeCred(t, home, secretWork)
	err := s.Apply("claude", "ghost")
	if !errors.Is(err, ErrNoAccount) || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v", err)
	}
	if err := s.Capture("claude", "bad label!"); err == nil {
		t.Fatal("an unsafe label was taken")
	}
	if err := s.Capture("nobody", "x"); err == nil {
		t.Fatal("an unknown CLI was taken")
	}
	empty, _, _ := setup(t)
	if err := empty.Capture("claude", "x"); err == nil || !strings.Contains(err.Error(), "signed in") {
		t.Fatalf("no sign-in file: %v", err)
	}
}

func TestKeychainClaudeOnMacIsOff(t *testing.T) {
	s, home, _ := setup(t)
	writeCred(t, home, secretWork)
	s.GOOS = "darwin"
	ok, why := s.Supported("claude")
	if ok || !strings.Contains(why, "Keychain") {
		t.Fatalf("supported=%v why=%q", ok, why)
	}
	if err := s.Capture("claude", "work"); err == nil {
		t.Fatal("captured on a keychain-backed CLI")
	}
	if ok, _ := s.Supported("codex"); !ok {
		t.Fatal("codex keeps a file on a Mac")
	}
}

func TestSymlinkedCredentialFileIsRefused(t *testing.T) {
	s, home, _ := setup(t)
	p := writeCred(t, home, secretWork)
	if err := s.Capture("claude", "work"); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(home, "elsewhere")
	if err := os.WriteFile(other, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(p)
	if err := os.Symlink(other, p); err != nil {
		t.Skip("no symlinks here")
	}
	if err := s.Apply("claude", "work"); err == nil {
		t.Fatal("wrote through a symlink")
	}
	if b, _ := os.ReadFile(other); string(b) != "keep" {
		t.Fatal("the symlink target was changed")
	}
}
