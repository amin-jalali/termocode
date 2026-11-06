package app

import (
	"strconv"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestHead is HEAD's full hash.
func webTestHead(run func(args ...string) string) string {
	return strings.TrimSpace(run("rev-parse", "HEAD"))
}

// webTestConfirm presses the laptop card's action and runs what follows.
func webTestConfirm(t *testing.T, m *Model) {
	t.Helper()
	m.confirmOpen = false
	webDrain(t, m, m.handleConfirmSelect("confirm"))
	m.webAfterUpdate(nil)
}

// Spec (Git: Undo Last Git Action… and Git: Redo from the web):
// git.undo_plan says in plain words what the last action was and what
// the undo does (the laptop's own card text); git.undo then waits for a
// laptop card with that text and runs the laptop's undo (reset --keep);
// git.redo takes the undo back, after its own card. Without a plan read
// first, or with nothing undone, the web gets a conflict.
func TestGitUndoRedoFromWeb(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	first := webTestHead(run)
	webTestWrite(t, dir, "b.txt", "b\n")
	run("add", "b.txt")
	run("commit", "-q", "-m", "second")
	second := webTestHead(run)
	webTestGitLoad(&m)
	c := webTestSub(t, &m)
	if f := webTestCallBg(t, &m, c, "git.undo", `{}`); webTestErr(f) != "conflict" {
		t.Fatalf("undo before a plan: %v", f)
	}
	if f := webTestCallBg(t, &m, c, "git.redo", `{}`); webTestErr(f) != "conflict" {
		t.Fatalf("redo before an undo: %v", f)
	}
	var plan struct {
		Title  string `json:"title"`
		Text   string `json:"text"`
		Action string `json:"action"`
	}
	webTestData(t, webTestCallBg(t, &m, c, "git.undo_plan", `{}`), &plan)
	if !strings.Contains(plan.Text, "Last action:") || !strings.Contains(plan.Text, "first") || plan.Action != "Undo" {
		t.Fatalf("plan %+v", plan)
	}
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, "git.undo", `{}`), &ref)
	if ref.Card == "" || m.confirm.SafeID() != "cancel" || !strings.Contains(m.confirm.View(), "Last action:") {
		t.Fatalf("card %+v:\n%s", ref, m.confirm.View())
	}
	if webTestHead(run) != second {
		t.Fatal("undone before the laptop said so")
	}
	webTestConfirm(t, &m)
	if got := webTestHead(run); got != first {
		t.Fatalf("HEAD %s after the undo, want %s", got, first)
	}
	if !strings.Contains(webTestToasts(m), "Undone") {
		t.Errorf("toasts: %s", webTestToasts(m))
	}
	webTestData(t, webTestCallBg(t, &m, c, "git.redo", `{}`), &ref)
	if !strings.Contains(m.confirm.View(), "Redo") {
		t.Errorf("redo card:\n%s", m.confirm.View())
	}
	webTestConfirm(t, &m)
	if got := webTestHead(run); got != second {
		t.Fatalf("HEAD %s after the redo, want %s", got, second)
	}
}

// Spec (Git: Reflog… from the web): git.reflog lists HEAD's moves, newest
// first; git.undo_plan with a listed line (its index and short hash)
// plans moving HEAD there; git.undo with move waits for a laptop card,
// then HEAD moves. A line that is not the listed one is refused.
func TestGitReflogMoveFromWeb(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	first := webTestHead(run)
	for _, n := range []string{"b", "c"} {
		webTestWrite(t, dir, n+".txt", n+"\n")
		run("add", n+".txt")
		run("commit", "-q", "-m", "add "+n)
	}
	webTestGitLoad(&m)
	c := webTestSub(t, &m)
	var lg struct {
		Entries []struct {
			Hash    string `json:"hash"`
			Summary string `json:"summary"`
		} `json:"entries"`
	}
	webTestData(t, webTestCallBg(t, &m, c, "git.reflog", `{}`), &lg)
	if len(lg.Entries) != 3 || lg.Entries[2].Hash != first[:7] || !strings.Contains(lg.Entries[0].Summary, "add c") {
		t.Fatalf("reflog %+v", lg)
	}
	if f := webTestCallBg(t, &m, c, "git.undo_plan", `{"reflog":true,"index":2,"hash":"abcdef0"}`); webTestErr(f) != "not_found" {
		t.Fatalf("a line not listed: %v", f)
	}
	var plan struct {
		Text string `json:"text"`
		Move bool   `json:"move"`
	}
	webTestData(t, webTestCallBg(t, &m, c, "git.undo_plan", `{"reflog":true,"index":2,"hash":"`+first[:7]+`"}`), &plan)
	if !plan.Move || !strings.Contains(plan.Text, "Reflog line:") {
		t.Fatalf("plan %+v", plan)
	}
	if f := webTestCallBg(t, &m, c, "git.undo", `{}`); webTestErr(f) != "conflict" {
		t.Fatalf("an undo for a move plan: %v", f)
	}
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, &m, c, "git.undo", `{"move":true}`), &ref)
	if !strings.Contains(m.confirm.View(), "Move HEAD") {
		t.Errorf("card:\n%s", m.confirm.View())
	}
	webTestConfirm(t, &m)
	if got := webTestHead(run); got != first {
		t.Fatalf("HEAD %s, want %s ("+strconv.Itoa(len(lg.Entries))+" lines)", got, first)
	}
}
