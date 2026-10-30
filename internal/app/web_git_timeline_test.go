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
