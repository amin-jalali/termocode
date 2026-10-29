package secrets

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
)

// unlockedAge is an age file in a temp dir, open with pass and holding
// one value.
func unlockedAge(t *testing.T) (*ageFile, string) {
	t.Helper()
	dir := t.TempDir()
	a := newAge(t, dir)
	if err := a.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	if err := a.Set("ai.key.openai", canary); err != nil {
		t.Fatal(err)
	}
	return a, filepath.Join(dir, AgeFile)
}

// Spec (U19 SS-2): an age file removed while open reads as empty and
// the next write makes it again.
func TestAgeFileRemovedWhileOpen(t *testing.T) {
	a, path := unlockedAge(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := a.Get("ai.key.openai"); ok || err != nil {
		t.Fatalf("Get after remove = %v %v", ok, err)
	}
	if err := a.Set("ai.key.b", "v"); err != nil {
		t.Fatal(err)
	}
	b := newAgeFile(path, 10)
	if err := b.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := b.Get("ai.key.b"); v != "v" || !ok {
		t.Fatalf("the file was not made again: %q %v", v, ok)
	}
}

// Spec (U19 SS-2): an age file damaged by another program while open
// fails reads and writes as corrupt; the error has no value in it.
func TestAgeFileDamagedWhileOpen(t *testing.T) {
	a, path := unlockedAge(t)
	if err := os.WriteFile(path, []byte("garbage that is not age"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := a.Get("ai.key.openai")
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Get err = %v", err)
	}
	noValueIn(t, err)
	if err := a.Set("ai.key.b", "v"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Set err = %v", err)
	}
	if got := readFile(t, path); got != "garbage that is not age" {
		t.Fatal("a write replaced the damaged file")
	}
}

// Spec (U19 SS-2): an age file that decrypts to something other than a
// JSON object is corrupt.
func TestAgeFileNotJSONInside(t *testing.T) {
	path := filepath.Join(t.TempDir(), AgeFile)
	r, err := age.NewScryptRecipient(pass)
	if err != nil {
		t.Fatal(err)
	}
	r.SetWorkFactor(10)
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(`["` + canary + `"]`)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	err = newAgeFile(path, 10).Unlock(pass)
	if !errors.Is(err, ErrCorrupt) || !strings.Contains(err.Error(), "not a JSON object") {
		t.Fatalf("err = %v", err)
	}
	noValueIn(t, err)
}

// Spec (U19 SS-2): an empty passphrase never opens a file.
func TestAgeFileEmptyPassphrase(t *testing.T) {
	_, path := unlockedAge(t)
	if err := newAgeFile(path, 10).Unlock(""); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("err = %v", err)
	}
}

// Spec (U19 SS-2): when the first write of a new age file fails, the
// store stays locked and no file is left.
func TestAgeFileFirstWriteFails(t *testing.T) {
	dir := t.TempDir()
	a := newAge(t, dir)
	t.Cleanup(setRename(func(string, string) error { return errors.New("disk full") }))
	if err := a.Unlock(pass); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("err = %v", err)
	}
	if !a.Locked() {
		t.Fatal("the store opened without a file")
	}
	gone(t, filepath.Join(dir, AgeFile))
	noTemps(t, dir)
}

// Spec (U19 SS-8): the passphrase belongs to the encrypted file only.
func TestPassphraseNeedsAgeStore(t *testing.T) {
	s, _, _ := fakeStore(t)
	for _, st := range []Store{s, otherStore{s}} {
		if err := CheckCurrentPassphrase(st, []byte(pass)); !errors.Is(err, ErrNoPassphrase) {
			t.Fatalf("CheckCurrentPassphrase err = %v", err)
		}
		if err := ChangePassphrase(st, []byte(pass), []byte(pass+"2")); !errors.Is(err, ErrNoPassphrase) {
			t.Fatalf("ChangePassphrase err = %v", err)
		}
	}
}

// Spec (U19 SS-8): the check of a re-encrypted file fails on a wrong
// passphrase or different values, and names no value.
func TestCheckReencrypted(t *testing.T) {
	_, path := unlockedAge(t)
	err := checkReencrypted(path, []byte("not the passphrase"), map[string]string{"ai.key.openai": canary})
	if !errors.Is(err, ErrWrongPassphrase) || !strings.HasPrefix(err.Error(), "check of the new file") {
		t.Fatalf("wrong pass: err = %v", err)
	}
	err = checkReencrypted(path, []byte(pass), map[string]string{"ai.key.openai": canary + "x"})
	if err == nil || !strings.Contains(err.Error(), "the values differ") {
		t.Fatalf("values differ: err = %v", err)
	}
	noValueIn(t, err)
	if err := checkReencrypted(path, []byte(pass), map[string]string{"ai.key.openai": canary}); err != nil {
		t.Fatalf("a good file fails the check: %v", err)
	}
}

// Spec (U19 SS-7): setting aside needs a file, and a failed rename keeps
// the file where it was.
func TestSetAsideAgeFileErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := SetAsideAgeFile(dir, time.Now()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no file: err = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, AgeFile), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(setRename(func(string, string) error { return errors.New("read-only") }))
	if _, err := SetAsideAgeFile(dir, time.Now()); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("rename fails: err = %v", err)
	}
	if got := readFile(t, filepath.Join(dir, AgeFile)); got != "x" {
		t.Fatal("the file moved")
	}
}

// Spec (U19 SS-3): the plain backend refuses a damaged field instead of
// overwriting it, and a delete of something absent writes nothing.
func TestPlainDamagedFieldsAndNoopDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	p := newPlain(path)
	if err := p.Delete("ai.claude_oauth"); err != nil {
		t.Fatal(err)
	}
	if err := p.Delete("ai.key.openai"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a delete of nothing wrote the file")
	}
	if err := p.Set("ai.claude_oauth", `{"access_token":"x"}`); err != nil {
		t.Fatal(err)
	}
	if err := p.Delete("ai.claude_oauth"); err != nil {
		t.Fatal(err)
	}
	if b := readFile(t, path); strings.Contains(b, "claude_oauth") {
		t.Fatalf("claude_oauth stayed:\n%s", b)
	}

	const damaged = `{"keys": 5}`
	if err := os.WriteFile(path, []byte(damaged), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Get("ai.key.openai"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Get err = %v", err)
	}
	if err := p.Set("ai.key.openai", canary); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Set err = %v", err)
	}
	if got := readFile(t, path); got != damaged {
		t.Fatal("the damaged file was overwritten")
	}
}

// Spec (U19 SS-3): a plain path that can not be read (a directory) is an
// error, not an empty file.
func TestPlainUnreadablePath(t *testing.T) {
	p := newPlain(t.TempDir())
	if _, _, err := p.Get("ai.key.openai"); err == nil {
		t.Fatal("Get of a directory did not fail")
	}
}

// Spec (U19 SS-2): an index with no entries field reads as empty; an
// index path that is a directory is an error.
func TestIndexEdgeCases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, IndexFile)
	if err := os.WriteFile(path, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	idx, err := loadIndex(path)
	if err != nil || idx.Entries == nil || len(idx.Entries) != 0 {
		t.Fatalf("loadIndex = %+v %v", idx, err)
	}
	if err := updateIndex(path, func(i *Index) { i.Entries["a"] = Entry{Kind: KindValue} }); err != nil {
		t.Fatalf("updateIndex on an index without entries: %v", err)
	}
	if _, err := loadIndex(dir); err == nil {
		t.Fatal("loadIndex of a directory did not fail")
	}
	if err := updateIndex(dir, func(*Index) {}); err == nil {
		t.Fatal("updateIndex of a directory did not fail")
	}
}

// Spec (U19 SS-2): an atomic write under a file (not a dir) fails and
// leaves nothing behind.
func TestAtomicWriteBadDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(filepath.Join(file, "sub", "secrets.json"), []byte("{}"), false); err == nil {
		t.Fatal("write under a file did not fail")
	}
	if err := writeFileAtomic(filepath.Join(file, "secrets.json"), []byte("{}"), true); err == nil {
		t.Fatal("write with tightenDir under a file did not fail")
	}
	noTemps(t, dir)
}
