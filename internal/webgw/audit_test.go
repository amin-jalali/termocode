//go:build unix

package webgw

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// TestAuditRotates: past the size cap the log moves to .1 (and .1 to .2);
// two old files are kept, all 0600, none over the cap (T37).
func TestAuditRotates(t *testing.T) {
	if AuditMaxBytes != 5<<20 || AuditKeep != 2 {
		t.Fatalf("AuditMaxBytes %d AuditKeep %d, want 5 MiB and 2", AuditMaxBytes, AuditKeep)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	a := openAudit(t, path)
	a.max = 400
	for i := 0; i < 40; i++ {
		if err := a.Log(Event{Kind: EvOp, Op: "tree.list", Code: i}); err != nil {
			t.Fatal(err)
		}
	}
	last := -1
	for _, p := range []string{path + ".2", path + ".1", path} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 || fi.Size() > 400 || fi.Size() == 0 {
			t.Fatalf("%s: mode %v size %d", filepath.Base(p), fi.Mode().Perm(), fi.Size())
		}
		for _, e := range readAudit(t, p) {
			if e.Code <= last {
				t.Fatalf("%s: event %d after %d", filepath.Base(p), e.Code, last)
			}
			last = e.Code
		}
	}
	if last != 39 {
		t.Fatalf("newest event %d, want 39", last)
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("a third old file exists: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatalf("%d files in the log folder, want 3", len(entries))
	}
}

// TestNoSecretInAuditSummary: the summary is built from policy (a path, a
// label, a count), but the log cleans it again: any long token-like run is
// masked, control and bidi characters go, and it is capped (T8).
func TestNoSecretInAuditSummary(t *testing.T) {
	launch, _, err := NewLaunchSecret(t0)
	if err != nil {
		t.Fatal(err)
	}
	secrets := []string{
		launch,
		"sk-" + "test" + "-0123456789abcdefghijklmnopqrstuvwxyz",
		"ghp_" + "0123456789abcdefABCDEF0123456789abcd",
		strings.Repeat("0123456789abcdef", 4),
		"QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo0123456789+/==",
	}
	path := filepath.Join(t.TempDir(), "a.jsonl")
	a := openAudit(t, path)
	for _, s := range secrets {
		for _, ev := range []Event{
			{Kind: EvOp, Op: "buf.save", Summary: "saved src/main.go with " + s},
			{Kind: EvOp, Op: "term.send", Summary: s},
			{Kind: EvAuthFail, Why: "wrong " + s, Conn: s, Device: s, Op: s},
		} {
			if err := a.Log(ev); err != nil {
				t.Fatal(err)
			}
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range secrets {
		if bytes.Contains(raw, []byte(s)) || bytes.Contains(raw, []byte(s[:24])) {
			t.Errorf("audit log holds %.12q…", s)
		}
	}
	if !bytes.Contains(raw, []byte("saved src/main.go with ")) {
		t.Error("the readable part of the summary was lost")
	}
}

func TestAuditSummaryCleaned(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"src/internal/webgw/listen_unix_test.go", "src/internal/webgw/listen_unix_test.go"},
		{"3 files", "3 files"},
		{"a\x1b[31mred\x07\r\nb", "a[31mredb"},
		{"x‮evil⁦y", "xevily"},
		{"bad \xff utf8", "bad � utf8"},
		{strings.Repeat("ab ", 100), strings.Repeat("ab ", 66) + "a…"},
	} {
		if got := cleanAuditText(tc.in); got != tc.want {
			t.Errorf("clean(%q) = %q, want %q", tc.in, got, tc.want)
		}
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
