package app

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func writeBak(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestParseHotBackup(t *testing.T) {
	b, ok := parseHotBackup([]byte("/w/a.txt\none\n\nthree"))
	if !ok || b.Path != "/w/a.txt" || !reflect.DeepEqual(b.Lines, []string{"one", "", "three"}) {
		t.Errorf("got %+v %v", b, ok)
	}
	// An empty buffer is one empty line.
	if b, ok := parseHotBackup([]byte("/w/e.txt\n")); !ok || !reflect.DeepEqual(b.Lines, []string{""}) {
		t.Errorf("empty buffer: %+v %v", b, ok)
	}
	if _, ok := parseHotBackup([]byte("no newline")); ok {
		t.Error("accepted a file without lines")
	}
}

// TestCollectHotBackups: backups of dead runs and of our own pid (a
// re-exec) are moved to previous/ and returned; a live instance keeps its
// own; a newer backup of the same file wins.
func TestCollectHotBackups(t *testing.T) {
	root := t.TempDir()
	writeBak(t, filepath.Join(root, "100"), "aaa.bak", "/w/a.txt\ndead run")
	writeBak(t, filepath.Join(root, "200"), "bbb.bak", "/w/b.txt\nlive instance")
	writeBak(t, filepath.Join(root, "300"), "ccc.bak", "/w/c.txt\nbefore reload")
	writeBak(t, filepath.Join(root, "previous"), "aaa.bak", "/w/a.txt\nolder")
	writeBak(t, filepath.Join(root, "100"), "x.tmp", "half written")

	alive := func(pid int) bool { return pid == 200 || pid == 300 }
	got, err := collectHotBackups(root, 300, alive)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Path < got[j].Path })
	want := []hotBackup{
		{Path: "/w/a.txt", Lines: []string{"dead run"}},
		{Path: "/w/c.txt", Lines: []string{"before reload"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, "200", "bbb.bak")); err != nil {
		t.Errorf("live instance lost its backup: %v", err)
	}
	for _, gone := range []string{"100", "300"} {
		if _, err := os.Stat(filepath.Join(root, gone)); !os.IsNotExist(err) {
			t.Errorf("dir %s not removed", gone)
		}
	}
}

func TestCollectHotBackupsEmpty(t *testing.T) {
	got, err := collectHotBackups(filepath.Join(t.TempDir(), "none"), 1, func(int) bool { return false })
	if err != nil || got != nil {
		t.Errorf("got %v %v", got, err)
	}
}

func TestHotExitRoot(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	got, err := hotExitRoot("/w/proj")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(got) != "/state/termocode/backup" || len(filepath.Base(got)) != 16 {
		t.Errorf("root %s", got)
	}
}
