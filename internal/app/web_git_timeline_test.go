package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (explorer TIMELINE): git.file_log lists the commits that changed
// one file, newest first, with author and time; git.show with that path
// shows only the file's part of a commit. Paths outside the workspace
// are refused.
func TestGitFileLogAndShowOneFile(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	webTestWrite(t, dir, "b.txt", "x\n")
	run("add", "b.txt")
	run("commit", "-q", "-m", "only b")
	webTestWrite(t, dir, "a.txt", "one\ntwo\nthree\n")
	webTestWrite(t, dir, "b.txt", "y\n")
	run("commit", "-q", "-am", "both")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)

	var log webproto.GitFileLog
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitFileLog, `{"path":"a.txt"}`), &log)
	var subj []string
	for _, x := range log.Log {
		subj = append(subj, x.Subject)
		if x.Hash == "" || x.Author != "T" || x.When < 1e12 {
			t.Errorf("commit %+v (when in ms)", x)
		}
	}
	if log.Path != "a.txt" || strings.Join(subj, ",") != "both,first" {
		t.Fatalf("log %+v", log)
	}

	var d webproto.GitDiff
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitShow, `{"hash":"`+log.Log[0].Hash+`","path":"a.txt"}`), &d)
	var files []string
	for _, r := range d.Rows {
		if r.K == "file" {
			files = append(files, r.Text)
		}
	}
	if d.Commit == nil || d.Commit.Subject != "both" || strings.Join(files, ",") != "a.txt" || d.Added != 1 {
		t.Fatalf("one file's show: %+v files %v", d.Commit, files)
	}
	if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitFileLog, `{"path":"nothere.txt"}`)); got != "" {
		t.Errorf("a file git does not know: %q, want an empty log", got)
	}
}

// Spec (diff editor): git.text answers one file from the index, HEAD, a
// commit or its parent; a file not there is missing (a new file's old
// side); a binary file has no text; a file that may hold secrets and a
// path outside the workspace are refused.
func TestGitTextRevisions(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	webTestWrite(t, dir, "a.txt", "old\n")
	run("commit", "-q", "-am", "old")
	webTestWrite(t, dir, "a.txt", "staged\n")
	run("add", "a.txt")
	webTestWrite(t, dir, "a.txt", "work\n")
	webTestWrite(t, dir, "bin.dat", "a\x00b\n")
	webTestWrite(t, dir, ".env", "TOKEN=x\n")
	run("add", "bin.dat", ".env")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)
	head := strings.TrimSpace(run("rev-parse", "--short", "HEAD"))

	for rev, want := range map[string]string{"index": "staged\n", "HEAD": "old\n", head: "old\n", head + "^": "one\ntwo\n"} {
		var g webproto.GitText
		webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitText, `{"path":"a.txt","rev":"`+rev+`"}`), &g)
		if g.Text != want || g.Missing || g.Path != "a.txt" || g.Rev != rev {
			t.Errorf("%s: %+v, want %q", rev, g, want)
		}
	}
	var g webproto.GitText
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitText, `{"path":"new.txt","rev":"HEAD"}`), &g)
	if !g.Missing || g.Text != "" {
		t.Errorf("a file not in HEAD: %+v", g)
	}
	g = webproto.GitText{}
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitText, `{"path":"bin.dat","rev":"index"}`), &g)
	if !g.Binary || g.Text != "" {
		t.Errorf("a binary file: %+v", g)
	}
	if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitText, `{"path":".env","rev":"index"}`)); got != string(webproto.CodeForbidden) {
		t.Errorf("a secret file: %q, want forbidden", got)
	}
}
