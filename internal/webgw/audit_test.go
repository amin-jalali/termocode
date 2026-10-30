//go:build unix

package webgw

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func openAudit(t *testing.T, path string) *Audit {
	t.Helper()
	a, err := OpenAudit(path, (&fakeClock{now: t0}).Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

// readAudit decodes every line of the audit file.
func readAudit(t *testing.T, path string) []Event {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var evs []Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Event
		dec := json.NewDecoder(bytes.NewReader(sc.Bytes()))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&e); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		evs = append(evs, e)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return evs
}

// TestAuditMode0600: the log is private to the user (T37).
func TestAuditMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web.audit.jsonl")
	a := openAudit(t, path)
	if err := a.Log(Event{Kind: EvStart}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", fi.Mode().Perm())
	}
}

// TestAuditTightensLooseMode: a log left world-readable is made 0600.
func TestAuditTightensLooseMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	openAudit(t, path)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", fi.Mode().Perm())
	}
}

// TestAuditRefusesSymlink: a planted symlink cannot point the log at
// another file.
func TestAuditRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "a.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if a, err := OpenAudit(link, nil); err == nil {
		a.Close()
		t.Fatal("opened the log through a symlink")
	}
	if b, _ := os.ReadFile(target); string(b) != "keep\n" {
		t.Fatalf("target changed: %q", b)
	}
}

func TestAuditAppendsJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jsonl")
	a := openAudit(t, path)
	ok := true
	if err := a.Log(Event{Kind: EvAuthOK, Device: "loc-1a2b", Conn: "s-01"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Log(Event{Kind: EvOp, Conn: "s-01", Op: "git.commit", OK: &ok, Summary: "3 files"}); err != nil {
		t.Fatal(err)
	}
	a.Close()
	// Reopen: new lines go after the old ones.
	a2 := openAudit(t, path)
	if err := a2.Log(Event{Kind: EvStop}); err != nil {
		t.Fatal(err)
	}
	evs := readAudit(t, path)
	if len(evs) != 3 || evs[0].Kind != EvAuthOK || evs[1].Op != "git.commit" || evs[2].Kind != EvStop {
		t.Fatalf("events %+v", evs)
	}
	if !evs[0].T.Equal(t0) || evs[1].OK == nil || !*evs[1].OK || evs[1].Summary != "3 files" {
		t.Fatalf("fields %+v", evs[1])
	}
}

// TestAuditNilIsQuiet: code with no audit log configured just runs.
func TestAuditNilIsQuiet(t *testing.T) {
	var a *Audit
	if err := a.Log(Event{Kind: EvStart}); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}
