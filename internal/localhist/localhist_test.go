package localhist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func at(min int) time.Time { return time.Date(2026, 11, 6, 12, min, 0, 0, time.UTC) }

// GITX-35 (R1–R4): each save keeps a copy; the same text twice is one
// version; the oldest go past the cap; a label marks a version; a file
// that is gone is listed as deleted under its root and can be read back.
func TestStoreVersions(t *testing.T) {
	s := &Store{Dir: t.TempDir(), MaxPerFile: 3}
	root := t.TempDir()
	p := filepath.Join(root, "sub", "a.go")

	for i, body := range []string{"one", "two", "two", "three", "four"} {
		saved, err := s.Save(p, []byte(body), at(i))
		if err != nil {
			t.Fatal(err)
		}
		if want := i != 2; saved != want {
			t.Fatalf("save %d (%q): saved %v, want %v", i, body, saved, want)
		}
	}
	vs, err := s.Versions(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 3 {
		t.Fatalf("versions %+v", vs)
	}
	var got []string
	for _, v := range vs {
		b, err := s.Read(p, v.ID)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(b))
	}
	if strings.Join(got, ",") != "four,three,two" {
		t.Fatalf("newest first: %v", got)
	}
	if !vs[0].At.Equal(at(4)) || vs[0].Size != 4 {
		t.Fatalf("newest %+v", vs[0])
	}

	// A label on the same text marks the newest version.
	if err := s.Label(p, []byte("four"), "before refactor", at(9)); err != nil {
		t.Fatal(err)
	}
	vs, _ = s.Versions(p)
	if len(vs) != 3 || vs[0].Label != "before refactor" {
		t.Fatalf("label %+v", vs)
	}
	// A label on new text is a new version.
	if err := s.Label(p, []byte("five"), "wip", at(10)); err != nil {
		t.Fatal(err)
	}
	vs, _ = s.Versions(p)
	if vs[0].Label != "wip" || vs[1].Label != "before refactor" {
		t.Fatalf("labels %+v", vs)
	}

	// Deleted files.
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(p, []byte("five"), 0o644)
	if d, _ := s.Deleted(root); len(d) != 0 {
		t.Fatalf("nothing is deleted yet: %+v", d)
	}
	_ = os.Remove(p)
	other := filepath.Join(t.TempDir(), "elsewhere.txt")
	_, _ = s.Save(other, []byte("x"), at(1))
	d, err := s.Deleted(root)
	if err != nil || len(d) != 1 || d[0].Path != p || !d[0].Last.Equal(at(10)) {
		t.Fatalf("deleted %+v %v", d, err)
	}
	_ = os.WriteFile(p, []byte("back"), 0o644)
	if d, _ := s.Deleted(root); len(d) != 0 {
		t.Fatalf("a file that is back is listed: %+v", d)
	}

	// Too big is not kept.
	s.MaxBytes = 3
	if saved, _ := s.Save(p, []byte("toolong"), at(20)); saved {
		t.Fatal("a file over MaxBytes was kept")
	}
	if _, err := s.Read(p, "../../etc/passwd"); err == nil {
		t.Fatal("Read took a path as an id")
	}
}

// Versions older than MaxAge go at the next save.
func TestStoreMaxAge(t *testing.T) {
	s := &Store{Dir: t.TempDir(), MaxAge: 24 * time.Hour}
	p := filepath.Join(t.TempDir(), "a.txt")
	_, _ = s.Save(p, []byte("old"), at(0))
	_, _ = s.Save(p, []byte("new"), at(0).Add(48*time.Hour))
	vs, _ := s.Versions(p)
	if len(vs) != 1 || vs[0].Size != 3 || !vs[0].At.Equal(at(0).Add(48*time.Hour)) {
		t.Fatalf("versions %+v", vs)
	}
}
