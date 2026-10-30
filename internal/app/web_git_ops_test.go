package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
			webTestGitRefresh(t, m, am(m))
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

// webTestGitRefresh runs the laptop's git refresh among the commands an
// answer returned (a toast's timer is left alone).
func webTestGitRefresh(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	got := make(chan tea.Msg, 8)
	var start func(tea.Cmd)
	n := 0
	start = func(c tea.Cmd) {
		if c == nil {
			return
		}
		n++
		go func() { got <- c() }()
	}
	start(cmd)
	deadline := time.After(10 * time.Second)
	for n > 0 {
		select {
		case msg := <-got:
			n--
			switch msg := msg.(type) {
			case GitMsg:
				m.applyGitSnapshot(msg)
				return
			case tea.BatchMsg:
				for _, c := range msg {
					start(c)
				}
			}
		case <-deadline:
			t.Fatal("no git refresh after the answer")
		}
	}
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

// webTestGitCodes maps path → git status code of the laptop's state.
func webTestGitCodes(m *Model) map[string]string {
	out := map[string]string{}
	for _, f := range m.gitFiles {
		out[f.Path] = f.Code
	}
	return out
}

// webTestAudit is the Web output channel (what the web did, as the laptop
// shows it).
func webTestAudit(m *Model) string { return strings.Join(m.output.Lines(webAuditChannel), "\n") }

// Spec (WEB-23): git.stage and git.unstage take files the Git tab lists,
// change only the index, refresh the laptop's git state and leave one
// audit line each; a hunk is staged and unstaged by its header, and a
// header that moved changes nothing. Unlisted files, conflicts and the
// wrong side are refused.
func TestStageUnstageRoundTrip(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	webTestWrite(t, dir, "a.txt", "ONE\ntwo\n")
	webTestWrite(t, dir, "new.txt", "fresh\n")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)

	if f := webTestCallBg(t, &m, c, webproto.OpGitStage, `{"paths":["a.txt","new.txt"]}`); f["t"] != "res" {
		t.Fatalf("stage: %v", f)
	}
	if got := webTestGitCodes(&m); got["a.txt"] != "M " || got["new.txt"] != "A " {
		t.Fatalf("after stage the laptop has %v", got)
	}
	if !strings.Contains(webTestAudit(&m), "git.stage  a.txt new.txt  by=web:phone  → ok") {
		t.Errorf("audit:\n%s", webTestAudit(&m))
	}
	if f := webTestCallBg(t, &m, c, webproto.OpGitUnstage, `{"paths":["new.txt"]}`); f["t"] != "res" {
		t.Fatalf("unstage: %v", f)
	}
	if got := webTestGitCodes(&m); got["new.txt"] != "??" || got["a.txt"] != "M " {
		t.Fatalf("after unstage the laptop has %v", got)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(body) != "ONE\ntwo\n" {
		t.Fatalf("the work tree changed: %q", body)
	}

	// Hunks: unstage a.txt's one hunk by its header, stage it back.
	var d webproto.GitDiff
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitDiff, `{"path":"a.txt","staged":true}`), &d)
	hunk := d.Rows[0].Text
	if f := webTestCallBg(t, &m, c, webproto.OpGitUnstageHunk, `{"path":"a.txt","hunk":"`+hunk+`"}`); f["t"] != "res" {
		t.Fatalf("unstage hunk: %v", f)
	}
	if got := webTestGitCodes(&m)["a.txt"]; got != " M" {
		t.Fatalf("after unstage hunk: %q", got)
	}
	if f := webTestCallBg(t, &m, c, webproto.OpGitStageHunk, `{"path":"a.txt","hunk":"@@ -9,9 +9,9 @@"}`); webTestErr(f) != "conflict" {
		t.Fatalf("a moved hunk: %v", f)
	}
	if got := webTestGitCodes(&m)["a.txt"]; got != " M" {
		t.Fatalf("a moved hunk changed the index: %q", got)
	}
	if f := webTestCallBg(t, &m, c, webproto.OpGitStageHunk, `{"path":"a.txt","hunk":"`+hunk+`"}`); f["t"] != "res" {
		t.Fatalf("stage hunk: %v", f)
	}
	if got := webTestGitCodes(&m)["a.txt"]; got != "M " {
		t.Fatalf("after stage hunk: %q", got)
	}

	// Refused: a file with no change, the wrong side, a conflict.
	m.gitFiles = append(m.gitFiles, gitFileConflict("c.txt"))
	for _, tc := range []struct{ op, args, code string }{
		{webproto.OpGitStage, `{"paths":["nothing.txt"]}`, "not_found"},
		{webproto.OpGitUnstage, `{"paths":["new.txt"]}`, "conflict"},
		{webproto.OpGitStage, `{"paths":["c.txt"]}`, "conflict"},
		{webproto.OpGitStageHunk, `{"path":"new.txt","hunk":"@@ -0,0 +1 @@"}`, "conflict"},
	} {
		if got := webTestErr(webTestCallBg(t, &m, c, tc.op, tc.args)); got != tc.code {
			t.Errorf("%s %s: %q, want %s", tc.op, tc.args, got, tc.code)
		}
	}
	_ = run
}

// Spec (WEB-23): git.commit needs a controller and a message (the args
// rules), something staged and no rebase or merge stopped half-way; it
// answers the new short hash and the file count, the laptop shows a toast
// and refreshes, and the Web channel keeps one line (never the message).
func TestCommitNeedsMessageAndController(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	c := webTestSub(t, &m)

	viewer := &webConn{hub: m.web, id: "v-1", role: webproto.RoleViewer, out: newWebQueue(), subs: map[string]*webSub{}}
	viewer.ctx, viewer.cancel = context.WithCancel(m.web.ctx)
	viewer.request(&webproto.Req{T: webproto.KindReq, ID: 1, Op: webproto.OpGitCommit, Args: json.RawMessage(`{"message":"x"}`)})
	if f := webTestNow(t, viewer); len(f) != 1 || webTestErr(f[0]) != "forbidden" {
		t.Fatalf("viewer commit: %v", f)
	}
	for _, msg := range []string{`""`, `"   "`, `"\n\nbody"`} {
		if _, err := webproto.DecodeArgs(webproto.OpGitCommit, json.RawMessage(`{"message":`+msg+`}`)); err == nil {
			t.Errorf("message %s decoded", msg)
		}
	}
	if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitCommit, `{"message":"nothing"}`)); got != "conflict" {
		t.Fatalf("nothing staged: %q", got)
	}

	webTestWrite(t, dir, "a.txt", "ONE\ntwo\n")
	webTestWrite(t, dir, "b.txt", "b\n")
	run("add", ".")
	webTestGitLoad(&m)
	m.gitRebase.InProgress = true
	if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitCommit, `{"message":"during a rebase"}`)); got != "conflict" {
		t.Fatalf("during a rebase: %q", got)
	}
	m.gitRebase.InProgress = false

	f := webTestCallBg(t, &m, c, webproto.OpGitCommit, `{"message":"fix: the secret plan\n\nmore words"}`)
	var r webproto.GitCommitted
	webTestData(t, f, &r)
	head := strings.TrimSpace(run("rev-parse", "--short", "HEAD"))
	if r.Hash != head || r.Files != 2 {
		t.Fatalf("committed %+v, HEAD %s", r, head)
	}
	if subj := strings.TrimSpace(run("log", "-1", "--format=%s")); subj != "fix: the secret plan" {
		t.Errorf("subject %q", subj)
	}
	if len(m.gitFiles) != 0 || len(m.gitCommits) == 0 || m.gitCommits[0].Hash != head {
		t.Errorf("the laptop's git state was not refreshed: %v %v", m.gitFiles, m.gitCommits)
	}
	if a := webTestAudit(&m); !strings.Contains(a, "git.commit  2 files") || strings.Contains(a, "secret plan") {
		t.Errorf("audit:\n%s", a)
	}
	if !strings.Contains(webTestToasts(m), "Committed on phone") {
		t.Errorf("toasts %q", webTestToasts(m))
	}
}
