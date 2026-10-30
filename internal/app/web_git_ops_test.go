package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// webTestRepo makes a git repository with a.txt committed, opens it as
// the Model's workspace and loads its git state; run runs git there.
func webTestRepo(t *testing.T, m *Model) (dir string, run func(args ...string) string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir = t.TempDir()
	run = func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@e.x")
	run("config", "user.name", "T")
	run("config", "commit.gpgsign", "false")
	webTestWrite(t, dir, "a.txt", "one\ntwo\n")
	run("add", ".")
	run("commit", "-q", "-m", "first")
	m.ws = workspace.Workspace{Root: dir}
	webTestGitLoad(m)
	return dir, run
}

func webTestWrite(t *testing.T, dir, name, body string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// webTestGitLoad reads the git state as the laptop's refresh does.
func webTestGitLoad(m *Model) {
	m.applyGitSnapshot(m.pollGitCmd()().(GitMsg))
}

// webTestCallBg runs a request whose answer comes from its background
// command: the command runs here, and an applyMsg it returns runs on the
// Model, as Update would do. It returns the answer frame.
func webTestCallBg(t *testing.T, m *Model, c *webConn, op, args string) map[string]any {
	t.Helper()
	webTestNow(t, c)
	a, err := webproto.DecodeArgs(op, json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s %s: %v", op, args, err)
	}
	c.lastID++
	call := &webCall{ctx: c.ctx, conn: c, id: c.lastID, op: op, args: a}
	c.inflight.Add(1)
	fn := m.web.ops[op]
	if fn == nil {
		t.Fatalf("no handler for %s", op)
	}
	cmd := m.webRunOp(fn, call)
	if cmd != nil {
		if am, ok := cmd().(applyMsg); ok {
			am(m)
		}
	}
	for _, f := range webTestNow(t, c) {
		if f["id"] == float64(call.id) {
			return f
		}
	}
	t.Fatalf("%s gave no answer", op)
	return nil
}

// webTestErr is the code of an err frame ("" for a res).
func webTestErr(f map[string]any) string {
	if f["t"] == "err" {
		code, _ := f["code"].(string)
		return code
	}
	return ""
}

// Spec (WEB-23): git.diff answers one side of a changed file as rows:
// hunk rows carry the exact header, add / del / ctx rows their line
// numbers; a new file is all added. Files git does not list as changed,
// and folders of new files, are refused.
func TestGitDiffRows(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	webTestWrite(t, dir, "a.txt", "one\nTWO\n")
	run("add", "a.txt")
	webTestWrite(t, dir, "a.txt", "ONE\nTWO\n")
	webTestWrite(t, dir, "new.txt", "fresh\n")
	webTestWrite(t, dir, "sub/x.txt", "x\n")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)

	f := webTestCallBg(t, &m, c, webproto.OpGitDiff, `{"path":"a.txt","staged":true}`)
	var d webproto.GitDiff
	webTestData(t, f, &d)
	want := []webproto.DiffRow{
		{K: "hunk", Text: "@@ -1,2 +1,2 @@", Old: 1, New: 1},
		{K: "ctx", Text: "one", Old: 1, New: 1},
		{K: "del", Text: "two", Old: 2},
		{K: "add", Text: "TWO", New: 2},
	}
	if d.Path != "a.txt" || !d.Staged || d.Added != 1 || d.Removed != 1 || d.Next != 0 || len(d.Rows) != len(want) {
		t.Fatalf("staged diff %+v", d)
	}
	for i := range want {
		if d.Rows[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, d.Rows[i], want[i])
		}
	}
	d = webproto.GitDiff{}
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitDiff, `{"path":"a.txt"}`), &d)
	if len(d.Rows) != 4 || d.Rows[1] != (webproto.DiffRow{K: "del", Text: "one", Old: 1}) || d.Rows[2] != (webproto.DiffRow{K: "add", Text: "ONE", New: 1}) {
		t.Errorf("unstaged diff %+v", d.Rows)
	}
	d = webproto.GitDiff{}
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitDiff, `{"path":"new.txt"}`), &d)
	if d.Added != 1 || len(d.Rows) != 2 || d.Rows[1] != (webproto.DiffRow{K: "add", Text: "fresh", New: 1}) {
		t.Errorf("new file %+v", d.Rows)
	}
	for _, tc := range []struct{ args, code string }{
		{`{"path":"nothing.txt"}`, "not_found"},
		{`{"path":"sub/"}`, "bad_request"},
	} {
		if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitDiff, tc.args)); got != tc.code {
			t.Errorf("git.diff %s: %q, want %s", tc.args, got, tc.code)
		}
	}
}

// Spec (WEB-23): a long diff comes in pages (from / next, at most
// MaxGitDiffRows rows each) and stops at MaxGitDiff with truncated set.
func TestGitDiffCapped(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir, _ := webTestRepo(t, &m)
	webTestWrite(t, dir, "big.txt", strings.Repeat("a line of text that is not short at all\n", 20000))
	webTestGitLoad(&m)
	c := webTestSub(t, &m)
	var d webproto.GitDiff
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitDiff, `{"path":"big.txt"}`), &d)
	if !d.Truncated || d.From != 0 || d.Next != len(d.Rows) || len(d.Rows) > webproto.MaxGitDiffRows {
		t.Fatalf("first page: rows %d from %d next %d truncated %v", len(d.Rows), d.From, d.Next, d.Truncated)
	}
	total, pages, from := len(d.Rows), 1, d.Next
	for from != 0 {
		var p webproto.GitDiff
		webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitDiff, `{"path":"big.txt","from":`+itoa(from)+`}`), &p)
		if p.From != from || len(p.Rows) == 0 {
			t.Fatalf("page at %d: from %d rows %d", from, p.From, len(p.Rows))
		}
		size := 0
		for _, r := range p.Rows {
			size += len(r.Text)
		}
		if size > webproto.MaxGitDiffPage {
			t.Fatalf("page of %d bytes", size)
		}
		total, from = total+len(p.Rows), p.Next
		pages++
	}
	if pages < 3 || total*41 > webproto.MaxGitDiff+41*2 {
		t.Fatalf("%d pages, %d rows: not capped at MaxGitDiff", pages, total)
	}
}

// Spec (WEB-23): git.show answers a commit's head and its rows, a file
// row before each file.
func TestGitShow(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	_, run := webTestRepo(t, &m)
	hash := strings.TrimSpace(run("rev-parse", "--short", "HEAD"))
	c := webTestSub(t, &m)
	var d webproto.GitDiff
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitShow, `{"hash":"`+hash+`"}`), &d)
	if d.Commit == nil || d.Commit.Subject != "first" || d.Commit.Hash != hash || d.Commit.When == 0 {
		t.Fatalf("head %+v", d.Commit)
	}
	if len(d.Rows) < 2 || d.Rows[0] != (webproto.DiffRow{K: "file", Text: "a.txt"}) || d.Rows[1].K != "hunk" || d.Added != 2 {
		t.Fatalf("rows %+v", d.Rows)
	}
	if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitShow, `{"hash":"deadbeef"}`)); got != "not_found" {
		t.Errorf("unknown commit: %q", got)
	}
}
