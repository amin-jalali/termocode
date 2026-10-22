package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

// Spec (U19 SS-8, plan §5): a keyring that worked at setup but does
// not answer later (an SSH login to the same machine) tells the app on
// every failed Get; a missing item is not a failure.
func TestKeyringGetFailureNotifies(t *testing.T) {
	n := 0
	SetKeyringFailNotifier(func() { n++ })
	t.Cleanup(func() { SetKeyringFailNotifier(nil) })
	keyring.MockInit()
	s, err := Open(Options{Dir: t.TempDir(), Backend: "keyring", KeyringProbe: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("ai.key.missing"); ok || err != nil || n != 0 {
		t.Fatalf("missing item: ok=%v err=%v notices=%d", ok, err, n)
	}
	keyring.MockInitWithError(errors.New("dbus: no session"))
	t.Cleanup(keyring.MockInit)
	if _, _, err := s.Get("ai.key.openai"); err == nil {
		t.Fatal("Get worked without a keyring")
	}
	if n != 1 {
		t.Fatalf("notices = %d, want 1", n)
	}
}

// Spec (U19 SS-8, plan §5): a damaged secrets.age is kept as
// secrets.age.broken-<YYYYMMDD-HHMMSS>, never deleted; the index drops
// the values it held (references stay), so a new file can start.
func TestSetAsideBrokenAgeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, AgeFile)
	if err := os.WriteFile(path, []byte("not an age file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := updateIndex(filepath.Join(dir, IndexFile), func(i *Index) {
		i.Backend = "age"
		i.Entries["ai.key.openai"] = Entry{Kind: KindValue, Backend: "age"}
		i.Entries["mcp.secret.GH"] = Entry{Kind: KindRef, Ref: "${env:GH}"}
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 22, 9, 5, 7, 0, time.UTC)
	got, err := SetAsideAgeFile(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "secrets.age.broken-20261022-090507")
	if got != want {
		t.Fatalf("kept as %s, want %s", got, want)
	}
	if readFile(t, want) != "not an age file" {
		t.Fatal("the content changed")
	}
	gone(t, path)
	e := Entries(dir)
	if _, ok := e["ai.key.openai"]; ok {
		t.Error("the lost value is still listed")
	}
	if e["mcp.secret.GH"].Kind != KindRef {
		t.Error("the reference was dropped")
	}
	// The same second again: a second name, the first file stays.
	if err := os.WriteFile(path, []byte("broken again"), 0o600); err != nil {
		t.Fatal(err)
	}
	again, err := SetAsideAgeFile(dir, now)
	if err != nil || again == want {
		t.Fatalf("second = %s %v", again, err)
	}
	if readFile(t, want) != "not an age file" || readFile(t, again) != "broken again" {
		t.Fatal("a kept file was overwritten")
	}
	// The new file starts with a new passphrase.
	a := newAgeFile(path, 0)
	if err := a.Unlock("a new passphrase"); err != nil {
		t.Fatal(err)
	}
}

// The unlock error of a damaged file names the file.
func TestCorruptAgeFileErrorNamesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, AgeFile)
	if err := os.WriteFile(path, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := newAgeFile(path, 0).Unlock(pass)
	if !errors.Is(err, ErrCorrupt) || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v", err)
	}
}
