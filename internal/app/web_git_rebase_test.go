package app

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestRebaseRepo has four commits on main: first, c2, c3, c4.
func webTestRebaseRepo(t *testing.T, m *Model) (dir string, run func(args ...string) string, hashes map[string]string) {
	t.Helper()
	dir, run = webTestRepo(t, m)
	for _, s := range []string{"c2", "c3", "c4"} {
		webTestWrite(t, dir, s+".txt", s+"\n")
		run("add", s+".txt")
		run("commit", "-q", "-m", s)
	}
	hashes = map[string]string{}
	for _, l := range strings.Split(strings.TrimSpace(run("log", "--format=%s %h")), "\n") {
		s, h, _ := strings.Cut(l, " ")
		hashes[s] = h
	}
	webTestGitLoad(m)
	return dir, run, hashes
}

// Spec (interactive rebase): git.rebase_plan lists a commit and every
// newer one of HEAD, oldest first, as picks, with the base it replays
// onto; a commit not on HEAD's history is refused.
func TestGitRebasePlan(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	_, run, h := webTestRebaseRepo(t, &m)
	c := webTestSub(t, &m)

	var p webproto.GitRebasePlan
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitRebasePlan, `{"from":"`+h["c3"]+`"}`), &p)
	var got []string
	for _, s := range p.Steps {
		got = append(got, s.Action+" "+s.Subject)
	}
	if strings.Join(got, ",") != "pick c3,pick c4" || !strings.HasPrefix(p.Base, h["c2"]) || p.Branch != "main" || p.Dirty || p.Pushed != 0 {
		t.Errorf("plan %+v steps %v", p, got)
	}
	run("checkout", "-q", "-b", "other", h["first"])
	webTestWrite(t, mustDir(t, run), "x.txt", "x\n")
	run("add", "x.txt")
	run("commit", "-q", "-m", "elsewhere")
	other := strings.TrimSpace(run("rev-parse", "--short", "HEAD"))
	run("checkout", "-q", "main")
	webTestGitLoad(&m)
	if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitRebasePlan, `{"from":"`+other+`"}`)); got != string(webproto.CodeConflict) {
		t.Errorf("a commit on another branch: %q", got)
	}
}

func mustDir(t *testing.T, run func(args ...string) string) string {
	t.Helper()
	return strings.TrimSpace(run("rev-parse", "--show-toplevel"))
}

// Spec (interactive rebase): git.rebase takes the plan's rows in a new
// order with actions; it waits for a laptop card that says what happens
// (the laptop's own text); confirmed, the laptop runs git with exactly
// that list. Rows not from the plan, or a list git could not run, are
// refused before any card.
func TestGitRebaseNeedsLaptopCard(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	_, _, h := webTestRebaseRepo(t, &m)
	c := webTestSub(t, &m)
	var ran *git.RebaseTodo
	var ranOpt git.RebaseOptions
	webRunRebase = func(dir string, todo git.RebaseTodo, opt git.RebaseOptions) (git.RebaseOutcome, error) {
		ran, ranOpt = &todo, opt
		return git.RebaseOutcome{}, nil
	}
	t.Cleanup(func() { webRunRebase = git.RunRebase })

	if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitRebase, `{"base":"","steps":[{"hash":"`+h["c3"]+`","action":"pick"}]}`)); got != string(webproto.CodeNotFound) {
		t.Errorf("no plan yet: %q", got)
	}
	var p webproto.GitRebasePlan
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitRebasePlan, `{"from":"`+h["c2"]+`"}`), &p)
	base := p.Base[:8]
	for name, steps := range map[string]string{
		"a row missing":      `[{"hash":"` + h["c2"] + `","action":"pick"},{"hash":"` + h["c3"] + `","action":"pick"}]`,
		"a row twice":        `[{"hash":"` + h["c2"] + `","action":"pick"},{"hash":"` + h["c2"] + `","action":"pick"},{"hash":"` + h["c4"] + `","action":"pick"}]`,
		"a foreign commit":   `[{"hash":"` + h["c2"] + `","action":"pick"},{"hash":"` + h["c3"] + `","action":"pick"},{"hash":"` + h["first"] + `","action":"pick"}]`,
		"a fixup at the top": `[{"hash":"` + h["c2"] + `","action":"fixup"},{"hash":"` + h["c3"] + `","action":"pick"},{"hash":"` + h["c4"] + `","action":"pick"}]`,
	} {
		f := webTestCallBg(t, &m, c, webproto.OpGitRebase, `{"base":"`+base+`","steps":`+steps+`}`)
		if webTestErr(f) == "" || m.confirmOpen {
			t.Errorf("%s: %v (card open %v)", name, f, m.confirmOpen)
		}
	}
	steps := `[{"hash":"` + h["c4"] + `","action":"reword","message":"c4 new"},{"hash":"` + h["c2"] + `","action":"pick"},{"hash":"` + h["c3"] + `","action":"fixup"}]`
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitRebase, `{"base":"`+base+`","steps":`+steps+`}`), &ref)
	if ref.Card == "" || !m.confirmOpen || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no laptop card: %+v", ref)
	}
	if v := m.confirm.View(); !strings.Contains(v, "3 commits") || !strings.Contains(v, "ORIG_HEAD") {
		t.Errorf("card:\n%s", v)
	}
	if ran != nil {
		t.Fatal("ran before the laptop said so")
	}
	m.confirmOpen = false
	webDrain(t, &m, m.handleConfirmSelect("confirm"))
	if ran == nil {
		t.Fatal("not run after the laptop's yes")
	}
	var rows []string
	for _, it := range ran.Items {
		rows = append(rows, string(it.Action)+" "+it.Subject+" "+it.Message)
	}
	if strings.Join(rows, ",") != "reword c4 c4 new,pick c2 ,fixup c3 " || ran.Base != p.Base || ranOpt.Autostash {
		t.Errorf("ran %v base %s opt %+v", rows, ran.Base, ranOpt)
	}
}

// Spec (interactive rebase): git.rebase_state says where a stopped
// rebase is; git.rebase_ctl abort waits for a laptop card, then puts the
// branch back (the laptop's own Abort).
func TestGitRebaseStateAndAbort(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir, run := webTestConflictRepo(t, &m)
	run("merge", "--abort")
	cmd := exec.Command("git", "rebase", "side")
	cmd.Dir = dir
	if err := cmd.Run(); err == nil {
		t.Fatal("the rebase did not stop")
	}
	webTestGitLoad(&m)
	c := webTestSub(t, &m)
	var st webproto.GitRebaseState
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitRebaseState, `{}`), &st)
	if !st.InProgress || st.Total < 1 || st.Subject != "main change" || len(st.Conflicts) != 1 || st.Conflicts[0] != "a.txt" {
		t.Errorf("state %+v", st)
	}
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitRebaseCtl, `{"do":"abort"}`), &ref)
	if ref.Card == "" || !m.confirmOpen || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no laptop card")
	}
	m.confirmOpen = false
	webDrain(t, &m, m.handleConfirmSelect("confirm"))
	if s := strings.TrimSpace(run("log", "-1", "--format=%s")); s != "main change" || git.RebaseStatus(dir).InProgress {
		t.Errorf("after abort: HEAD %q, in progress %v", s, git.RebaseStatus(dir).InProgress)
	}
	webTestGitLoad(&m)
	if got := webTestErr(webTestCallBg(t, &m, c, webproto.OpGitRebaseCtl, `{"do":"continue"}`)); got != string(webproto.CodeConflict) {
		t.Errorf("no rebase: %q", got)
	}
}
