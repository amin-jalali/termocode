package app

import (
	"strings"
	"testing"
)

// prDiffModel has PR 14 (head abc123) open in the diff card.
func prDiffModel(t *testing.T) (*Model, *prFakeGH) {
	t.Helper()
	m, f := prThreadsModel(t)
	m.prS().detail.HeadSHA = "abc123"
	f.answers["pr diff"] = prDiffText14
	prDo(t, m, m.prDiffCmd(14))
	if !m.reviewOpen {
		t.Fatal("no diff card")
	}
	return m, f
}

// c on the focused change asks for a comment and leaves it on the real
// line of the new file at the head commit; the diff card stays open.
func TestPRLineComment(t *testing.T) {
	m, f := prDiffModel(t)
	m.prDiffAction("comment")
	if !m.promptOpen || m.promptKind != promptKindPRLineComment {
		t.Fatal("comment prompt not open")
	}
	if !strings.Contains(m.prompt.View(), "parser.go:2") {
		t.Errorf("prompt does not name the line:\n%s", m.prompt.View())
	}
	cmd, ok := m.handlePRPromptSubmit("why New?")
	if !ok || cmd == nil {
		t.Fatal("no command")
	}
	m.promptOpen = false
	done := cmd().(prDoneMsg)
	want := "api --method POST repos/{owner}/{repo}/pulls/14/comments -f body=why New? -f commit_id=abc123 -f path=parser.go -F line=2 -f side=RIGHT"
	if done.err != nil || !f.called(want) {
		t.Fatalf("done %+v calls %v", done, f.calls)
	}
	m.onPRDone(done)
	if !toastHas(m, "Commented on parser.go:2") || !m.reviewOpen {
		t.Errorf("toast or card lost (card open %v)", m.reviewOpen)
	}
}

// s prefills the line's text; the answer goes out as a suggestion block.
func TestPRLineSuggestion(t *testing.T) {
	m, f := prDiffModel(t)
	m.prDiffAction("suggest")
	if !m.promptOpen || m.promptKind != promptKindPRSuggest || m.prompt.Value() != "func New() {}" {
		t.Fatalf("suggest prompt %v kind %v value %q", m.promptOpen, m.promptKind, m.prompt.Value())
	}
	cmd, _ := m.handlePRPromptSubmit("func Newer() {}")
	cmd()
	if !f.called("api --method POST repos/{owner}/{repo}/pulls/14/comments -f body=```suggestion\nfunc Newer() {}\n``` -f commit_id=abc123") {
		t.Errorf("calls %v", f.calls)
	}
}

// The second file (a new file) comments on its line 1; a pull request
// with no head commit, or that is not open, refuses with a toast.
func TestPRLineCommentOtherFileAndRefusals(t *testing.T) {
	m, f := prDiffModel(t)
	r := m.aiS().review
	r.card.Select(1)
	m.prDiffAction("comment")
	cmd, _ := m.handlePRPromptSubmit("hi")
	cmd()
	if !f.called("api --method POST repos/{owner}/{repo}/pulls/14/comments -f body=hi -f commit_id=abc123 -f path=docs/new.md -F line=1 -f side=RIGHT") {
		t.Errorf("calls %v", f.calls)
	}

	m, _ = prDiffModel(t)
	m.prS().detail.HeadSHA = ""
	m.prDiffAction("comment")
	if m.promptOpen || !toastHas(m, "Line comments need") {
		t.Error("no head commit: not refused")
	}
	m, _ = prDiffModel(t)
	m.prS().detail.State = "MERGED"
	m.prDiffAction("suggest")
	if m.promptOpen || !toastHas(m, "Line comments need") {
		t.Error("merged PR: not refused")
	}
}
