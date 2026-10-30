package webdevices

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A save that cannot finish fails loudly and leaves no temp file behind.
func TestWriteAtomicFailures(t *testing.T) {
	dir := t.TempDir()
	if err := writeAtomic(filepath.Join(dir, "missing", "devices.json"), []byte("{}")); err == nil {
		t.Fatal("write into a missing folder: no error")
	}
	// The target is a folder with a file in it: the rename fails.
	target := filepath.Join(dir, "devices.json")
	if err := os.MkdirAll(filepath.Join(target, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := writeAtomic(target, []byte("{}"))
	if err == nil || !strings.Contains(err.Error(), "save devices") {
		t.Fatalf("rename onto a folder: err = %v", err)
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".devices-") {
			t.Errorf("temp file left: %s", e.Name())
		}
	}
}

// Update changes nothing when it cannot read or write, or fn says no.
func TestUpdateFailures(t *testing.T) {
	keep := func(ds []Device) ([]Device, error) { return ds, nil }

	// The store's folder would be under a file.
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Open(filepath.Join(file, "web")).Update(keep); err == nil {
		t.Fatal("folder under a file: no error")
	}

	// devices.json cannot be read.
	s := testStore(t)
	if err := os.MkdirAll(s.Path(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(keep); err == nil {
		t.Fatal("unreadable devices.json: no error")
	}
	if _, err := s.List(); err == nil {
		t.Fatal("List of an unreadable devices.json: no error")
	}

	// fn refuses: the file stays as it was.
	s = testStore(t)
	if err := s.Add(dev("dev-1", RoleViewer)); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.Path())
	no := errors.New("no")
	if err := s.Update(func([]Device) ([]Device, error) { return nil, no }); !errors.Is(err, no) {
		t.Fatalf("err = %v", err)
	}
	after, _ := os.ReadFile(s.Path())
	if string(before) != string(after) {
		t.Fatalf("file changed:\n%s\n%s", before, after)
	}
}
