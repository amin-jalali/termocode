package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/statusbar"
)

// covCICard loads the CI state from f and returns the card text.
func covCICard(t *testing.T, f *ciFakeGH) (*Model, string) {
	t.Helper()
	m := ciModel("feat/x", f)
	ciRun(t, m, m.ciOnGitSnapshot(git.Branch{}))
	m.openCICard()
	return m, ciCardText(m.ciW())
}

// What the card says in each state: no GitHub remote, GitHub not
// reachable, runs of every kind without a pull request, a draft pull
// request waiting for review, no runs at all.
func TestCovGitCICardStates(t *testing.T) {
	// gh's own words for a repository with no GitHub remote.
	noRemote := ciModel("feat/x", &ciFakeGH{})
	noRemote.ciW().run = func(context.Context, string, ...string) (string, string, error) {
		return "", "none of the git remotes configured for this repository point to a known GitHub host", errors.New("exit status 1")
	}
	ciRun(t, noRemote, noRemote.ciOnGitSnapshot(git.Branch{}))
	if text := ciCardText(noRemote.ciW()); !strings.Contains(text, "This repository has no GitHub remote.") {
		t.Errorf("no remote:\n%s", text)
	}
	_, text := covCICard(t, &ciFakeGH{err: errors.New("timeout")})
	if !strings.Contains(text, "Could not reach GitHub: gh: timeout") {
		t.Errorf("unreachable:\n%s", text)
	}
	runs := `[{"databaseId":1,"workflowName":"Pend","status":"in_progress","headSha":"0123456789ab","url":"u1"},
	 {"databaseId":2,"workflowName":"Red","status":"completed","conclusion":"failure","headSha":"0123456789ab","url":"u2"},
	 {"databaseId":3,"workflowName":"Skip","status":"completed","conclusion":"cancelled","headSha":"0123456789ab","url":"u3"}]`
	m, text := covCICard(t, &ciFakeGH{runs: runs})
	for _, want := range []string{"commit 0123456", "No pull request for this branch.", "… Pend", "✗ Red", "– Skip"} {
		if !strings.Contains(text, want) {
			t.Errorf("runs card misses %q:\n%s", want, text)
		}
	}
	if got := ciURL(m.ciW().snap); got != "u2" {
		t.Errorf("link of a red branch without a pull request = %q", got)
	}
	pr := `{"number":5,"title":"WIP","state":"OPEN","isDraft":true,"url":"p5","reviewDecision":"REVIEW_REQUIRED","statusCheckRollup":[]}`
	_, text = covCICard(t, &ciFakeGH{runs: "[]", pr: pr})
	for _, want := range []string{"Draft · Review required", "No CI runs for this branch yet."} {
		if !strings.Contains(text, want) {
			t.Errorf("draft card misses %q:\n%s", want, text)
		}
	}
	if ciTitle("") != "" || shortSHA("abc") != "abc" {
		t.Error("empty title / short sha")
	}
}

// The card's buttons: Open Log opens the log page, Open in Browser opens
// the link (or copies it with no browser), Close does nothing; another
// card's answer is not the CI card's.
func TestCovGitCICardButtons(t *testing.T) {
	m, _ := ciRedModel(t)
	m.openCICard()
	m.confirmOpen = false
	cmd, ok := m.handleCIConfirm("log")
	if !ok {
		t.Fatal("log not taken")
	}
	lg := covMsg[ciLogMsg](t, cmd)
	m.onCILog(lg)
	if m.focus != FocusEditor || m.err != "" {
		t.Errorf("log page: focus %v err %q", m.focus, m.err)
	}

	old := osOpen
	defer func() { osOpen = old }()
	osOpen = func(string) error { return errors.New("no browser") }
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	m.confirmKind = confirmKindCI
	m.handleCIConfirm("browser")
	if !toastHas(m, "No browser: copied the link") {
		t.Error("no copy note without a browser")
	}
	if cmd, ok := m.handleCIConfirm("close"); !ok || cmd != nil {
		t.Error("Close did something")
	}
	if cmd, _ := m.handleCIConfirm("fix"); cmd == nil {
		t.Error("Fix with AI did nothing")
	}
	m.confirmKind = confirmKindPRView
	if _, ok := m.handleCIConfirm("log"); ok {
		t.Error("another card's answer was taken")
	}
	if m.openLink("") != nil {
		t.Error("empty link opened")
	}
}

// Fix with AI: a log that can not be read says so; while the AI still
// answers it waits; an old chat is kept and a new one gets the log; a
// green branch has no log.
func TestCovGitCIFixWithAI(t *testing.T) {
	m, _ := ciRedModel(t)
	m.onCILog(ciLogMsg{fix: true, err: errors.New("gone")})
	if !toastHas(m, "Could not read the CI log") {
		t.Error("no log error toast")
	}
	m.aiS().streaming = true
	m.onCILog(ciLogMsg{fix: true, branch: "feat/x"})
	if !toastHas(m, "The AI is still answering") {
		t.Error("no wait note")
	}
	m.aiS().streaming = false
	m.aiS().conv.Messages = []ai.StoredMessage{{Role: "user", Text: "old"}}
	m.onCILog(ciLogMsg{fix: true, branch: "feat/x", run: ghci.Run{Workflow: "CI"}, log: ghci.FailedLog{Job: "test"}})
	if !strings.Contains(string(m.aiS().input), "feat/x") || len(m.aiS().conv.Messages) != 0 {
		t.Errorf("new chat not filled: %q", string(m.aiS().input))
	}

	green := ciModel("feat/x", &ciFakeGH{runs: `[{"databaseId":1,"workflowName":"CI","status":"completed","conclusion":"success","headSha":"b"}]`})
	ciRun(t, green, green.ciOnGitSnapshot(git.Branch{}))
	green.ciFetchLog(false)
	if !toastHas(green, "CI is not red on feat/x") {
		t.Error("green branch: no note")
	}
	var st statusbar.State
	green.fillCIStatus(&st)
	if st.CI != statusbar.CIPass {
		t.Errorf("chip %v", st.CI)
	}
	noRepo := &Model{w: 160, h: 40, gitLoaded: true}
	noRepo.openCICard()
	if noRepo.confirmOpen || !strings.Contains(ansi.Strip(covToasts(noRepo)), "CI needs a git branch") {
		t.Error("CI card outside a repo")
	}
}

// A fetch that ends after the branch moved, or while another was asked
// for, starts again; an answer of an older chain schedules nothing.
func TestCovGitCIResultRaces(t *testing.T) {
	f := &ciFakeGH{runs: ciRedRuns}
	m := ciModel("feat/x", f)
	w := m.ciW()
	w.branch = "feat/x"
	w.again = true
	if m.onCIResult(ciResultMsg{branch: "feat/x"}) == nil || w.again {
		t.Error("a fetch asked for during a run was dropped")
	}
	if m.onCIResult(ciResultMsg{branch: "old"}) == nil {
		t.Error("the moved branch was not fetched")
	}
	w.gen = 5
	if cmd := m.onCIResult(ciResultMsg{branch: "feat/x", gen: 4}); cmd != nil {
		t.Error("an older chain scheduled a poll")
	}
}

// covToasts is the text of every toast.
func covToasts(m *Model) string {
	var b strings.Builder
	for _, c := range m.toast.Cards(160) {
		b.WriteString(strings.Join(c.Lines, "\n") + "\n")
	}
	return b.String()
}
