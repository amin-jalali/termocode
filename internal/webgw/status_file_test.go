//go:build unix

package webgw

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The status file is written 0600 through a temp file; a write that
// cannot finish fails and leaves no temp file.
func TestWriteStatusFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web.json")
	if err := writeStatus(path, Status{PID: 7, Port: 41237, URL: "http://127.0.0.1:41237/"}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", fi, err)
	}
	if st, err := ReadStatus(path); err != nil || st.Port != 41237 || st.PID != 7 {
		t.Fatalf("read back %+v %v", st, err)
	}

	if err := writeStatus(filepath.Join(dir, "missing", "web.json"), Status{}); err == nil {
		t.Fatal("missing folder: no error")
	}
	busy := filepath.Join(dir, "busy.json")
	if err := os.MkdirAll(filepath.Join(busy, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeStatus(busy, Status{}); err == nil {
		t.Fatal("rename onto a folder: no error")
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".web-status-") {
			t.Errorf("temp file left: %s", e.Name())
		}
	}
}
