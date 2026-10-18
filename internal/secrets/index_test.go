package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestIndexMissingIsEmpty(t *testing.T) {
	idx, err := loadIndex(filepath.Join(t.TempDir(), "secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Entries) != 0 || idx.Backend != "" {
		t.Fatalf("want empty index, got %+v", idx)
	}
}

func TestIndexWriteModesAndRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "termocode")
	path := filepath.Join(dir, "secrets.json")
	err := updateIndex(path, func(i *Index) {
		i.Backend = "age"
		i.Entries["ai.key.openai"] = Entry{Kind: KindRef, Ref: "${env:OPENAI_API_KEY}"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if m := mode(t, path); m != 0o600 {
		t.Errorf("index mode = %o, want 600", m)
	}
	if m := mode(t, dir); m != 0o700 {
		t.Errorf("new dir mode = %o, want 700", m)
	}
	idx, err := loadIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if idx.Backend != "age" || idx.Entries["ai.key.openai"].Ref != "${env:OPENAI_API_KEY}" {
		t.Fatalf("round trip lost data: %+v", idx)
	}
	noTemps(t, dir)
}

func TestIndexCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadIndex(path); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
	// A corrupt index is never overwritten (it may hold references).
	if err := updateIndex(path, func(i *Index) { i.Backend = "age" }); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("update err = %v, want ErrCorrupt", err)
	}
	if got := readFile(t, path); got != "{not json" {
		t.Fatalf("corrupt index was overwritten: %q", got)
	}
}

func TestIndexMergesConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	// Two UIs: each update re-reads the file, so neither loses the other's name.
	if err := updateIndex(path, func(i *Index) { i.Entries["a"] = Entry{Kind: KindValue} }); err != nil {
		t.Fatal(err)
	}
	if err := updateIndex(path, func(i *Index) { i.Entries["b"] = Entry{Kind: KindValue} }); err != nil {
		t.Fatal(err)
	}
	idx, _ := loadIndex(path)
	if len(idx.Entries) != 2 {
		t.Fatalf("entries = %v, want a and b", idx.Entries)
	}
}

func TestAtomicWriteKeepsOldFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.json")
	if err := writeFileAtomic(path, []byte("old"), false); err != nil {
		t.Fatal(err)
	}
	restore := setRename(func(string, string) error { return errors.New("disk full") })
	err := writeFileAtomic(path, []byte("new"), false)
	restore()
	if err == nil {
		t.Fatal("want the rename error")
	}
	if got := readFile(t, path); got != "old" {
		t.Fatalf("file = %q, want old content kept", got)
	}
	noTemps(t, dir)
}

func TestAtomicWriteTightensExistingFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.json")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("y"), false); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, path); m != 0o600 {
		t.Errorf("mode = %o, want 600", m)
	}
}

func TestAtomicWriteTightenDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ai")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(filepath.Join(dir, "c.json"), []byte("{}"), true); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, dir); m != 0o700 {
		t.Errorf("dir mode = %o, want 700", m)
	}
}
