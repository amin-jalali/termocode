package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const newPass = "a much longer new passphrase"

// changeFixture is an age store with one value and one reference.
func changeFixture(t *testing.T) (Store, string) {
	t.Helper()
	dir := t.TempDir()
	s := openAge(t, dir, Options{Getenv: func(string) string { return "env-v" }})
	if err := s.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("ai.key.openai", "sk-SECRET-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("ai.key.anthropic", "${env:X}"); err != nil {
		t.Fatal(err)
	}
	return s, dir
}

// opens reports whether pass opens the file at dir.
func opens(t *testing.T, dir, p string) bool {
	t.Helper()
	a := newAgeFile(filepath.Join(dir, AgeFile), 0)
	err := a.Unlock(p)
	a.Lock()
	return err == nil
}

// Spec (U19 SS-8): Change passphrase re-encrypts the file: the new
// passphrase opens it, the old one does not, every value stays and the
// store keeps working.
func TestChangePassphrase(t *testing.T) {
	s, dir := changeFixture(t)
	if err := CheckCurrentPassphrase(s, []byte(pass)); err != nil {
		t.Fatalf("check current: %v", err)
	}
	if err := CheckCurrentPassphrase(s, []byte("wrong passphrase")); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("check wrong: %v", err)
	}
	if err := ChangePassphrase(s, []byte(pass), []byte(newPass)); err != nil {
		t.Fatal(err)
	}
	if !opens(t, dir, newPass) || opens(t, dir, pass) {
		t.Fatal("the file does not open with the new passphrase only")
	}
	if v, ok, err := s.Get("ai.key.openai"); v != "sk-SECRET-1" || !ok || err != nil {
		t.Fatalf("Get after change = %q %v %v", v, ok, err)
	}
	if err := s.Set("db.main", "pw-SECRET"); err != nil {
		t.Fatal(err)
	}
	if !opens(t, dir, newPass) {
		t.Fatal("a write after the change used the old passphrase")
	}
	if m := mode(t, filepath.Join(dir, AgeFile)); m != 0o600 {
		t.Errorf("mode %o", m)
	}
	noTemps(t, dir)
}

// Spec (U19 SS-8): a locked store can change its passphrase (the
// current one is asked); afterwards it is open with the new one.
func TestChangePassphraseWhileLocked(t *testing.T) {
	s, dir := changeFixture(t)
	s.Lock()
	if err := ChangePassphrase(s, []byte(pass), []byte(newPass)); err != nil {
		t.Fatal(err)
	}
	if s.Locked() {
		t.Fatal("still locked after the change")
	}
	if v, _, _ := s.Get("ai.key.openai"); v != "sk-SECRET-1" {
		t.Fatalf("Get = %q", v)
	}
	if !opens(t, dir, newPass) {
		t.Fatal("new passphrase does not open the file")
	}
}

// Spec (U19 SS-8): on any failure the old file stays as it was and the
// old passphrase keeps working.
func TestChangePassphraseFailuresKeepOldFile(t *testing.T) {
	cases := map[string]struct {
		old, new string
		hook     func() func()
		want     error
	}{
		"wrong current": {old: "wrong passphrase", new: newPass, want: ErrWrongPassphrase},
		"too short":     {old: pass, new: "short", want: ErrWeakPassphrase},
		"same":          {old: pass, new: pass, want: ErrSamePassphrase},
		"rename fails": {old: pass, new: newPass, hook: func() func() {
			return setRename(func(string, string) error { return errors.New("disk full") })
		}},
		"check fails": {old: pass, new: newPass, hook: func() func() {
			return setReencryptCheck(func(string, []byte, map[string]string) error { return errors.New("bad copy") })
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, dir := changeFixture(t)
			path := filepath.Join(dir, AgeFile)
			before, _ := os.ReadFile(path)
			restore := func() {}
			if c.hook != nil {
				restore = c.hook()
			}
			err := ChangePassphrase(s, []byte(c.old), []byte(c.new))
			restore()
			if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(before) {
				t.Fatal("the file changed")
			}
			noTemps(t, dir)
			if !opens(t, dir, pass) {
				t.Fatal("the old passphrase no longer opens the file")
			}
			if err := s.Set("db.after", "v-after"); err != nil {
				t.Fatalf("the store broke: %v", err)
			}
			if !opens(t, dir, pass) {
				t.Fatal("the store writes with a new passphrase after a failed change")
			}
		})
	}
}

// Spec (U19 SS-8): only the encrypted file has a passphrase.
func TestChangePassphraseNotForOtherBackends(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Dir: dir, Backend: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ChangePassphrase(s, []byte(pass), []byte(newPass)); !errors.Is(err, ErrNoPassphrase) {
		t.Fatalf("err = %v", err)
	}
	if err := CheckCurrentPassphrase(s, []byte(pass)); !errors.Is(err, ErrNoPassphrase) {
		t.Fatalf("check err = %v", err)
	}
}
