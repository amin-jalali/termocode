package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

// Spec (amend): git.amend waits for a laptop card that names the last
// commit and says when it is on the remote already; nothing changes
// before the laptop says yes; then the commit is replaced with the
// staged changes and the new message (or keeps its own).
func TestGitAmendNeedsLaptopCard(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, _, run := webTestRemoteRepo(t, &m)
	webTestWrite(t, dir, "b.txt", "b\n")
	run("add", "b.txt")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)

	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitAmend, `{"message":"first, amended"}`), &ref)
	if ref.Card == "" || !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no laptop card: %+v", ref)
	}
	v := m.confirm.View()
	for _, want := range []string{"first", "force-push", "1 staged"} {
		if !strings.Contains(v, want) {
			t.Errorf("card has no %q:\n%s", want, v)
		}
	}
	if s := strings.TrimSpace(run("log", "-1", "--format=%s")); s != "first" {
		t.Fatalf("changed before the laptop said so: %q", s)
	}
	webTestLaptopConfirmRemote(t, &m)
	if s := strings.TrimSpace(run("log", "-1", "--format=%s")); s != "first, amended" {
		t.Errorf("subject after amend %q", s)
	}
	if files := run("show", "--name-only", "--format=", "HEAD"); !strings.Contains(files, "b.txt") {
		t.Errorf("the staged file is not in the amended commit: %q", files)
	}
	if n := strings.TrimSpace(run("rev-list", "--count", "HEAD")); n != "1" {
		t.Errorf("%s commits, want 1 (replaced, not added)", n)
	}
}

// Spec (amend): an amend that would change nothing (nothing staged, no
// new message) is refused before any card.
func TestGitAmendNothingToChange(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	webTestRepo(t, &m)
	c := webTestSub(t, &m)
	if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitAmend, `{}`)); got != string(webproto.CodeConflict) || m.confirmOpen {
		t.Errorf("amend with nothing to change: %q, card open %v", got, m.confirmOpen)
	}
}

// Spec (clone): git.clone waits for a laptop card that names the URL and
// the folder (the laptop's default, next to this repository); nothing
// starts before the laptop says yes; then the laptop's own clone runs
// (its progress toast, its sign-in prompt on the laptop). A folder that
// is not empty is refused before any card.
func TestGitCloneNeedsLaptopCard(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir, _ := webTestRepo(t, &m)
	c := webTestSub(t, &m)
	url := "https://example.com/team/tool.git"
	dest := filepath.Join(filepath.Dir(dir), "tool")
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitClone, `{"url":"`+url+`"}`), &ref)
	if ref.Card == "" || !m.confirmOpen || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no laptop card: %+v", ref)
	}
	if v := m.confirm.View(); !strings.Contains(v, "example.com/team/tool.git") || !strings.Contains(v, "tool") {
		t.Errorf("card:\n%s", v)
	}
	if m.clone.running {
		t.Fatal("started before the laptop said so")
	}
	m.confirmOpen = false
	cmd := m.handleConfirmSelect("confirm")
	if cmd == nil || !m.clone.running || m.clone.url != url || m.clone.dest != dest {
		t.Errorf("after yes: running %v url %q dest %q (want %q)", m.clone.running, m.clone.url, m.clone.dest, dest)
	}
	webTestCloneDone(t, cmd) // the tests' proxy stops it at once
	m.clone = cloneState{}

	if err := os.MkdirAll(filepath.Join(dest, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.confirmOpen = false
	if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitClone, `{"url":"`+url+`"}`)); got != string(webproto.CodeConflict) || m.confirmOpen {
		t.Errorf("a folder that is not empty: %q", got)
	}
}

// webTestCloneDone runs the clone's commands until it says it is done (a
// toast's timer is left alone).
func webTestCloneDone(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	got := make(chan tea.Msg, 16)
	start := func(c tea.Cmd) {
		if c != nil {
			go func() { got <- c() }()
		}
	}
	start(cmd)
	deadline := time.After(60 * time.Second)
	for {
		select {
		case msg := <-got:
			switch msg := msg.(type) {
			case cloneDoneMsg:
				return
			case cloneProgressMsg:
				start(waitCloneMsg(msg.ch))
			case tea.BatchMsg:
				for _, c := range msg {
					start(c)
				}
			}
		case <-deadline:
			t.Fatal("the clone did not end")
		}
	}
}
