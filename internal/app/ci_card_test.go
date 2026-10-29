package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
)

const ciPR = `{"number":12,"title":"Add parser","state":"OPEN","isDraft":false,"url":"https://github.com/o/r/pull/12",
 "reviewDecision":"CHANGES_REQUESTED","statusCheckRollup":[
  {"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"d1"},
  {"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"SUCCESS","detailsUrl":"d3"}]}`

func ciFailedLogText() string {
	return "test\tRun tests\t2026-10-29T08:00:01.1Z --- FAIL: TestParse\n" +
		"test\tRun tests\t2026-10-29T08:00:01.2Z     parse_test.go:12: token=" + "ghp_" + "abcdefghijklmnopqrstuv\n"
}

// ciRedModel is a model whose branch has red CI and a PR, already loaded.
func ciRedModel(t *testing.T) (*Model, *ciFakeGH) {
	t.Helper()
	f := &ciFakeGH{runs: ciRedRuns, pr: ciPR, log: ciFailedLogText()}
	m := ciModel("feat/x", f)
	ciRun(t, m, m.ciOnGitSnapshot(git.Branch{}))
	return m, f
}

// The card shows the PR, its review state and checks; on red, Fix with
// AI is the first, focused button.
func TestCICardRed(t *testing.T) {
	m, _ := ciRedModel(t)
	m.openCICard()
	if !m.ciCardOpen() {
		t.Fatal("card not open")
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"CI and Pull Request", "Branch feat/x · commit bbb", "Pull request #12 · Add parser",
		"Open · Changes requested", "✗ test", "✓ build"} {
		if !strings.Contains(box, want) {
			t.Errorf("card misses %q:\n%s", want, box)
		}
	}
	b := m.confirm.Buttons()
	if len(b) != 4 || b[0].ID != "fix" || b[1].ID != "log" || b[2].ID != "browser" || b[3].ID != "close" {
		t.Errorf("buttons %+v", b)
	}
}

// Without gh (or its login) the card shows one quiet hint and Close.
func TestCICardNoGH(t *testing.T) {
	f := &ciFakeGH{err: ghci.ErrNotLoggedIn}
	m := ciModel("feat/x", f)
	ciRun(t, m, m.ciOnGitSnapshot(git.Branch{}))
	m.openCICard()
	box := ansi.Strip(m.confirm.Box())
	if !strings.Contains(box, ciGHHint) {
		t.Errorf("no hint:\n%s", box)
	}
	if b := m.confirm.Buttons(); len(b) != 1 || b[0].ID != "close" {
		t.Errorf("buttons %+v", b)
	}
}

// Opening the card asks gh again; the answer redraws the open card.
func TestCICardRefreshes(t *testing.T) {
	f := &ciFakeGH{runs: ciPendingRuns}
	m := ciModel("feat/x", f)
	cmd := m.openCICard()
	if !strings.Contains(ansi.Strip(m.confirm.Box()), "Checking CI…") {
		t.Errorf("first card:\n%s", ansi.Strip(m.confirm.Box()))
	}
	ciRun(t, m, cmd)
	box := ansi.Strip(m.confirm.Box())
	if !strings.Contains(box, "No pull request for this branch.") || !strings.Contains(box, "… CI") {
		t.Errorf("refreshed card:\n%s", box)
	}
}

// The palette and a click on the chip open the card.
func TestCICardEntryPoints(t *testing.T) {
	m, _ := ciRedModel(t)
	m.dispatchPaletteAction("git-ci")
	if !m.ciCardOpen() {
		t.Fatal("palette did not open the card")
	}
	m.confirmOpen = false
	var hit bool
	for _, sp := range m.statusSpans() {
		if sp.id == "status:ci" {
			hit = true
			bx, _ := m.statusBarRect()
			if got := m.statusHitAt(bx+sp.x0, m.h-1); got != "status:ci" {
				t.Errorf("hit %q", got)
			}
		}
	}
	if !hit {
		t.Error("no status:ci span")
	}
}

// Fix with AI puts the failing log, redacted, in the chat input of a new
// chat; nothing is sent.
func TestCIFixWithAIFillsInput(t *testing.T) {
	m, f := ciRedModel(t)
	s := m.aiS()
	s.store = ai.ChatStore{Dir: t.TempDir()}
	s.conv = ai.Conversation{ID: "old", Messages: []ai.StoredMessage{{Role: ai.StoredUser, Text: "hi"}}}
	ciRun(t, m, m.ciLogCmd(true))
	if !strings.Contains(strings.Join(f.calls, ";"), "run view 30 --log-failed") {
		t.Errorf("calls %v", f.calls)
	}
	in := string(s.input)
	for _, want := range []string{"CI failed on branch feat/x (workflow CI).", "Job: test", "Step: Run tests",
		"--- FAIL: TestParse", "Find the cause and fix it."} {
		if !strings.Contains(in, want) {
			t.Errorf("input misses %q:\n%s", want, in)
		}
	}
	if strings.Contains(in, "ghp_"+"abcdefghijklmnopqrstuv") {
		t.Error("token reached the chat input")
	}
	if s.conv.ID == "old" || len(s.conv.Messages) != 0 || s.streaming {
		t.Errorf("chat %q messages %d streaming %v: want a new, unsent chat", s.conv.ID, len(s.conv.Messages), s.streaming)
	}
	if !m.actionsPanelVisible() || !s.focused {
		t.Error("chat panel not open and focused")
	}
}

// Fix with AI on a green branch opens the card instead.
func TestCIFixWithAINotRed(t *testing.T) {
	f := &ciFakeGH{runs: ciPendingRuns}
	m := ciModel("feat/x", f)
	ciRun(t, m, m.ciOnGitSnapshot(git.Branch{}))
	m.ciFixWithAI()
	if !m.ciCardOpen() || len(m.aiS().input) != 0 {
		t.Error("want the card, not a chat message")
	}
}

// Open Log shows every line of the failed job with where it failed.
func TestCILogPage(t *testing.T) {
	msg := ciLogMsg{run: ghci.Run{ID: 30, Workflow: "CI", URL: "u30"}, log: ghci.ParseFailedLog(ciFailedLogText())}
	page := ciLogPage(msg)
	if !strings.HasPrefix(page, "CI · test · Run tests\nu30\n\n--- FAIL: TestParse\n") {
		t.Errorf("page:\n%s", page)
	}
}

// Open in Browser opens the PR; over SSH it copies the link instead.
func TestCIOpenBrowser(t *testing.T) {
	m, _ := ciRedModel(t)
	var opened, copied []string
	old := osOpen
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	defer func() { osOpen = old }()
	defer SetClipboardSink(func(s string) { copied = append(copied, s) })()
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	m.ciOpenBrowser()
	if len(opened) != 1 || opened[0] != "https://github.com/o/r/pull/12" {
		t.Errorf("opened %v", opened)
	}
	t.Setenv("SSH_CONNECTION", "1.2.3.4 5 6.7.8.9 22")
	m.ciOpenBrowser()
	if len(opened) != 1 || len(copied) != 1 || copied[0] != "https://github.com/o/r/pull/12" {
		t.Errorf("over SSH: opened %v copied %v", opened, copied)
	}
}
