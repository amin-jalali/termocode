//go:build unix

package app

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (GITX-21 on the web): git.peek answers the change under a line of
// the saved file, as the laptop's gutter peek card shows it: its exact
// hunk header (git.stage_hunk takes it), its rows, "change N of M",
// whether it is staged and where every change starts (Next Change). A
// line with no change is not found; a file with unsaved edits on the
// laptop and a file that may hold secrets are refused.
func TestWebGitPeek(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	var lines []string
	for i := 1; i <= 12; i++ {
		lines = append(lines, "l"+string(rune('a'+i)))
	}
	webTestWrite(t, dir, "a.txt", strings.Join(lines, "\n")+"\n")
	run("commit", "-q", "-am", "twelve")
	lines[1], lines[10] = "TWO", "ELEVEN"
	webTestWrite(t, dir, "a.txt", strings.Join(lines, "\n")+"\n")
	webTestWrite(t, dir, ".env", "TOKEN=x\n")
	run("add", "-f", ".env")
	run("commit", "-q", "-m", "env", "--", ".env")
	webTestWrite(t, dir, ".env", "TOKEN=y\n")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)

	var p webproto.GitPeek
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitPeek, `{"path":"a.txt","line":2}`), &p)
	if p.Path != "a.txt" || p.Line != 2 || p.Index != 1 || p.Count != 2 || p.Staged || !strings.HasPrefix(p.Header, "@@ ") {
		t.Fatalf("peek %+v", p)
	}
	if !strings.Contains(strings.Join(p.Lines, "\n"), "-lc\n+TWO") || len(p.Starts) != 2 || p.Starts[0] != 2 || p.Starts[1] != 11 {
		t.Errorf("rows %q starts %v", p.Lines, p.Starts)
	}
	// The header is the one git.stage_hunk stages.
	if e := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitStageHunk, `{"path":"a.txt","hunk":`+strconv.Quote(p.Header)+`}`)); e != "" {
		t.Fatalf("stage the peeked header: %s", e)
	}
	webTestGitLoad(&m)
	p = webproto.GitPeek{}
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitPeek, `{"path":"a.txt","line":2}`), &p)
	if !p.Staged {
		t.Errorf("after stage: %+v", p)
	}

	for args, want := range map[string]webproto.ErrCode{
		`{"path":"a.txt","line":6}`: webproto.CodeNotFound,
		`{"path":".env","line":1}`:  webproto.CodeForbidden,
	} {
		if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitPeek, args)); got != string(want) {
			t.Errorf("%s: %q, want %s", args, got, want)
		}
	}
	m.bufs = append(m.bufs, nvimBufferInfo(filepath.Join(dir, "a.txt"), true))
	if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitPeek, `{"path":"a.txt","line":11}`)); got != string(webproto.CodeConflict) {
		t.Errorf("unsaved: %q, want conflict", got)
	}
}
