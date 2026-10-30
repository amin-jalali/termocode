package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (web Git Graph): git.graph answers the commits of every branch
// with their refs, the HEAD mark, the time in ms and each row's lanes (a
// side branch's commit sits in its own column); head:true lists HEAD's
// history only; skip pages on with the lanes still right.
func TestGitGraphRowsAndLanes(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	run("checkout", "-q", "-b", "side")
	webTestWrite(t, dir, "s.txt", "s\n")
	run("add", "s.txt")
	run("commit", "-q", "-m", "on side")
	run("checkout", "-q", "main")
	webTestWrite(t, dir, "m.txt", "m\n")
	run("add", "m.txt")
	run("commit", "-q", "-m", "on main")
	run("tag", "v1")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)

	var g webproto.GitGraph
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitGraph, `{}`), &g)
	if len(g.Rows) != 3 || g.More || g.Skip != 0 {
		t.Fatalf("graph %+v", g)
	}
	by := map[string]webproto.GitGraphRow{}
	for _, r := range g.Rows {
		by[r.Subject] = r
		if r.Hash == "" || r.Author != "T" || r.When < 1e12 || r.Up == nil || r.Down == nil {
			t.Errorf("row %+v", r)
		}
	}
	mainTip, side, root := by["on main"], by["on side"], by["first"]
	var refs []string
	for _, r := range mainTip.Refs {
		refs = append(refs, r.Kind+":"+r.Name)
	}
	if !mainTip.Head || strings.Join(refs, ",") != "local:main,tag:v1" {
		t.Errorf("main's tip %+v", mainTip)
	}
	if side.Col == mainTip.Col || len(side.Refs) != 1 || side.Refs[0].Name != "side" || side.Head {
		t.Errorf("side %+v, main col %d", side, mainTip.Col)
	}
	if root.Col != 0 || len(root.Up) != 2 || len(root.Down) != 0 || len(root.Parents) != 0 {
		t.Errorf("root %+v", root)
	}

	g = webproto.GitGraph{}
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitGraph, `{"head":true}`), &g)
	if len(g.Rows) != 2 {
		t.Errorf("HEAD only: %d rows", len(g.Rows))
	}
	g = webproto.GitGraph{}
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitGraph, `{"skip":2}`), &g)
	if len(g.Rows) != 1 || g.Skip != 2 || g.Rows[0].Subject != "first" || len(g.Rows[0].Up) != 2 {
		t.Errorf("page 2: %+v", g)
	}
}

// Spec (blame): git.blame lists each commit once and points every line
// of the saved file at one; a line changed since the last commit is
// uncommitted. A new file, a file that may hold secrets and a path out
// of the workspace are refused.
func TestGitBlameLines(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	webTestWrite(t, dir, "a.txt", "one\ntwo\nthree\n")
	run("commit", "-q", "-am", "add three")
	webTestWrite(t, dir, "a.txt", "ONE\ntwo\nthree\n")
	webTestWrite(t, dir, "new.txt", "n\n")
	webTestWrite(t, dir, ".env", "TOKEN=x\n")
	run("add", "-f", ".env")
	run("commit", "-q", "-m", "env", "--", ".env")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)

	var b webproto.GitBlame
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitBlame, `{"path":"a.txt"}`), &b)
	if b.Path != "a.txt" || len(b.Lines) != 3 || b.Cut {
		t.Fatalf("blame %+v", b)
	}
	at := func(line int) webproto.GitBlameCommit { return b.Commits[b.Lines[line-1]] }
	if l := at(1); !l.Uncommitted || l.Hash != "" {
		t.Errorf("line 1 (changed): %+v", l)
	}
	if l := at(2); l.Subject != "first" || l.Author != "T" || l.When < 1e12 || len(l.Hash) < 7 {
		t.Errorf("line 2: %+v", l)
	}
	if l := at(3); l.Subject != "add three" {
		t.Errorf("line 3: %+v", l)
	}
	if len(b.Commits) != 3 {
		t.Errorf("commits listed %d times, want 3 distinct: %+v", len(b.Commits), b.Commits)
	}
	for path, want := range map[string]webproto.ErrCode{"new.txt": webproto.CodeNotFound, ".env": webproto.CodeForbidden} {
		if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitBlame, `{"path":"`+path+`"}`)); got != string(want) {
			t.Errorf("%s: %q, want %s", path, got, want)
		}
	}
}
