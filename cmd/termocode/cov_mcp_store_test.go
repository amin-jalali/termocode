//go:build unix

package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/amin-jalali/termocode/internal/secrets"
)

// fakeStore is a secrets.Store whose answers the test sets.
type fakeStore struct {
	locked    bool
	unlockErr error
	setErr    error
	delErr    error
	set       map[string]string
	deleted   []string
}

func (f *fakeStore) Backend() string { return "fake" }
func (f *fakeStore) Locked() bool    { return f.locked }
func (f *fakeStore) Unlock(p string) error {
	if f.unlockErr == nil && p == "right" {
		f.locked = false
		return nil
	}
	if f.unlockErr != nil {
		return f.unlockErr
	}
	return errors.New("wrong passphrase")
}
func (f *fakeStore) Lock()                            {}
func (f *fakeStore) Get(string) (string, bool, error) { return "", false, nil }
func (f *fakeStore) Names() []string                  { return nil }
func (f *fakeStore) Delete(n string) error            { f.deleted = append(f.deleted, n); return f.delErr }
func (f *fakeStore) Set(n, v string) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.set == nil {
		f.set = map[string]string{}
	}
	f.set[n] = v
	return nil
}

func useStore(t *testing.T, st secrets.Store, err error) {
	t.Helper()
	old := openMCPSecretStore
	openMCPSecretStore = func() (secrets.Store, error) { return st, err }
	t.Cleanup(func() { openMCPSecretStore = old })
}

func TestMCPAdminSecretStoreFailures(t *testing.T) {
	cwd := adminEnv(t)
	tok := "sk-" + "test0123456789abcdef"
	useStore(t, nil, errors.New("no keyring"))
	if code, _, errs := runAdmin(t, cwd, tok+"\n", "add", "--secret", "K", "s1", "--", "srv"); code == 0 || !strings.Contains(errs, "could not open the secret store") {
		t.Errorf("open error: %d %q", code, errs)
	}
	useStore(t, &fakeStore{locked: true}, nil)
	if code, _, errs := runAdmin(t, cwd, tok+"\n", "add", "--secret", "K", "s2", "--", "srv"); code == 0 || !strings.Contains(errs, "is locked") {
		t.Errorf("locked without a terminal: %d %q", code, errs)
	}
	useStore(t, &fakeStore{setErr: errors.New("disk full")}, nil)
	if code, _, errs := runAdmin(t, cwd, tok+"\n", "add", "--secret", "K", "s3", "--", "srv"); code == 0 || !strings.Contains(errs, "could not store secret") {
		t.Errorf("set error: %d %q", code, errs)
	}
}

func TestMCPAdminStoresAndDropsSecrets(t *testing.T) {
	cwd := adminEnv(t)
	tok := "sk-" + "test0123456789abcdef"
	st := &fakeStore{}
	useStore(t, st, nil)
	if code, _, errs := runAdmin(t, cwd, tok+"\n", "add", "--secret", "K", "s4", "--", "srv"); code != 0 {
		t.Fatalf("add: %d %q", code, errs)
	}
	if len(st.set) != 1 {
		t.Fatalf("stored %v", st.set)
	}
	st.delErr = errors.New("store busy")
	code, _, errs := runAdmin(t, cwd, "", "remove", "s4")
	if code != 0 || !strings.Contains(errs, "Its secrets stay in the store") {
		t.Errorf("remove with a failing store: %d %q", code, errs)
	}
}

// A locked store asks for its passphrase on a terminal.
func TestUnlockedStoreAsksOnATerminal(t *testing.T) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skip(err)
	}
	defer ptmx.Close()
	defer tty.Close()
	go func() { _, _ = io.Copy(io.Discard, ptmx) }()
	for _, c := range []struct {
		pass string
		ok   bool
	}{{"right", true}, {"wrong", false}} {
		useStore(t, &fakeStore{locked: true}, nil)
		go func(p string) {
			time.Sleep(200 * time.Millisecond)
			_, _ = ptmx.WriteString(p + "\n")
		}(c.pass)
		var errw bytes.Buffer
		a := &mcpAdmin{in: tty, errw: &errw}
		st, err := a.unlockedStore()
		if (err == nil) != c.ok || (c.ok && st == nil) {
			t.Errorf("%s: %v", c.pass, err)
		}
		if !strings.Contains(errw.String(), "Passphrase") {
			t.Errorf("no prompt: %q", errw.String())
		}
	}
}
