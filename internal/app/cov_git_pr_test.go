package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

// A draft, approved pull request with a long description and every kind
// of review.
const covPRDraft = `{"number":7,"title":"Draft work","body":"l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nl12\nl13\nl14\nl15\nl16",
 "author":{"login":"ann"},"state":"OPEN","isDraft":true,"url":"https://github.com/o/r/pull/7","reviewDecision":"APPROVED",
 "headRefName":"wip","baseRefName":"main","mergeable":"MERGEABLE","statusCheckRollup":[],"files":[],
 "reviews":[{"author":{"login":"a"},"state":"APPROVED","body":""},{"author":{"login":"b"},"state":"COMMENTED","body":"ok"},
            {"author":{"login":"c"},"state":"DISMISSED","body":""},{"author":{"login":"d"},"state":"PENDING","body":""}],
 "comments":[]}`

// The card of a draft: its state line, a clipped description, the review
// states, and no Files… button (no files).
func TestCovGitPRViewDraft(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr view": covPRDraft}}
	m := prModel(f)
	prDo(t, m, m.prViewCmd(7))
	text := prViewText(*m.prS().detail)
	for _, want := range []string{"Open · Draft · Approved", "l14\n… 2 more lines", "a · Approved", "b · Commented", "c · Dismissed", "\nd\n"} {
		if !strings.Contains(text+"\n", want) {
			t.Errorf("card misses %q:\n%s", want, text)
		}
	}
	for _, b := range m.confirm.Buttons() {
		if b.ID == "files" {
			t.Error("Files… shown with no files")
		}
	}
	if cmd, ok := m.handlePRConfirm("close"); !ok || cmd != nil {
		t.Error("Close did something")
	}
	if m.onPRActionPicked("nope") != nil {
		t.Error("unknown action did something")
	}
	// Request changes needs a message.
	m.onPRActionPicked("request-changes")
	m.promptOpen = false
	cmd, _ := m.handlePRPromptSubmit("")
	d := covMsg[prDoneMsg](t, cmd)
	if d.err == nil || d.err.Error() != "write a message first" {
		t.Fatalf("empty review: %v", d.err)
	}
	m.onPRDone(d)
	if !toastHas(m, "write a message first") {
		t.Error("no toast")
	}
}

// With no pull request loaded, its actions do nothing.
func TestCovGitPRNoDetail(t *testing.T) {
	m := prModel(&prFakeGH{})
	m.showPRView()
	if m.confirmOpen {
		t.Error("view opened with no pull request")
	}
	for name, cmd := range map[string]func() tea.Cmd{
		"actions":  m.prOpenActions,
		"pick":     func() tea.Cmd { return m.onPRActionPicked("merge") },
		"review":   func() tea.Cmd { return m.prAskReview(ghci.ReviewApprove) },
		"files":    m.prOpenFiles,
		"diff btn": func() tea.Cmd { return m.prDiffAction("copy-link") },
		"checkout": m.prCheckout,
	} {
		if cmd() != nil || m.pickerOpen || m.promptOpen {
			t.Errorf("%s with no pull request did something", name)
		}
	}
	m.promptKind = promptKindPRReview
	if cmd, ok := m.handlePRPromptSubmit("x"); !ok || cmd != nil {
		t.Error("review with no pull request was sent")
	}
	m.promptKind = promptKindPRCreateTitle
	if cmd, ok := m.handlePRPromptSubmit("x"); !ok || cmd != nil {
		t.Error("create title with no request did something")
	}
	if m.onPRDiff(prDiffMsg{number: 3}) != nil {
		t.Error("a diff for no pull request opened")
	}
}

// The diff card: deleted and renamed files are tagged; Copy link copies;
// a diff for another pull request (the user moved on) is dropped; an AI
// review still waiting keeps its card.
func TestCovGitPRDiffCard(t *testing.T) {
	m, _ := prViewModel(t)
	files, add, del := prDiffCardFiles([]ghci.FileDiff{
		{Path: "gone.go", Deleted: true, Del: 3},
		{Path: "new.go", OldPath: "old.go", Add: 1},
	})
	if files[0].Tag != "deleted" || files[1].Tag != "from old.go" || add != 1 || del != 3 {
		t.Errorf("files %+v add %d del %d", files, add, del)
	}
	if m.onPRDiff(prDiffMsg{number: 99}) != nil || m.reviewOpen {
		t.Error("a stale diff opened")
	}
	pending := &aiReview{card: diffview.New("AI", []diffview.File{{Name: "x.go", Old: "a\n", New: "b\n"}}, true)}
	m.aiS().review = pending
	m.onPRDiff(prDiffMsg{number: 14, files: []ghci.FileDiff{{Path: "p.go", Old: "a\n", New: "b\n"}}})
	if m.aiS().review != pending || !toastHas(m, "Finish the AI review first") {
		t.Fatal("the waiting AI review was replaced")
	}
	m.aiS().review = nil
	m.onPRDiff(prDiffMsg{number: 14, files: []ghci.FileDiff{{Path: "p.go", Old: "a\n", New: "b\n"}}})
	if !m.reviewOpen {
		t.Fatal("diff card not open")
	}
	m.toast = toast.New()
	m.aiS().review.onAction(m, "copy-link")
	if m.toast.Empty() { // the text depends on the clipboard the machine has
		t.Error("Copy link said nothing")
	}
	if m.prDiffAction("other") != nil {
		t.Error("unknown button did something")
	}
}

// Check Out in a folder that is not a repository says it can not read
// the git state; a pull request with no description says so.
func TestCovGitPRCheckoutOutsideRepo(t *testing.T) {
	covGitEnv(t)
	f := &prFakeGH{answers: map[string]string{"pr view": prViewText14}}
	m := prModel(f)
	ws, err := localhost.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.ws = ws
	prDo(t, m, m.prViewCmd(14))
	m.onPRActionPicked("checkout")
	if !toastHas(m, "Could not read the git state") || f.called("pr checkout") {
		t.Error("checkout outside a repo not refused")
	}
	m.prS().detail.Body = "  "
	m.onPRActionPicked("description")
	if !toastHas(m, "#14 has no description") {
		t.Error("no note for an empty description")
	}
}

// What a finished gh change says: no GitHub remote is a quiet note; a new
// pull request asks CI again.
func TestCovGitPRDoneMessages(t *testing.T) {
	m := prModel(&prFakeGH{answers: map[string]string{}})
	m.onPRDone(prDoneMsg{err: ghci.ErrNoGitHub, failed: "x"})
	if !toastHas(m, "This repository has no GitHub remote.") {
		t.Error("no remote note")
	}
	if m.onPRDone(prDoneMsg{ok: "Created #3", ci: true, number: 3}) == nil || m.ciW().gen == 0 {
		t.Error("CI not asked again")
	}
	m.ciW().busy = true
	m.onPRDone(prDoneMsg{ok: "Created #4", ci: true})
	if !m.ciW().again {
		t.Error("a busy CI fetch was not told to run again")
	}
}

// Create Pull Request: outside a branch it says so; a branch with no new
// commits has nothing to propose; Push on the push card pushes.
func TestCovGitPRCreateRefusals(t *testing.T) {
	covGitEnv(t)
	m := prModel(&prFakeGH{})
	m.gitBranch.Name = ""
	m.prCreateCmd()
	if !toastHas(m, "needs a git branch") {
		t.Error("no branch note")
	}
	m.onPRCreatePrep(prCreatePrepMsg{branch: "feat", base: "main", pushed: true})
	if !toastHas(m, "nothing to propose") {
		t.Error("no nothing-to-propose note")
	}
	m2, _ := covGitRepo(t)
	m2.ciW().run = (&prFakeGH{}).run
	m2.onPRCreatePrep(prCreatePrepMsg{branch: "main2", base: "main"})
	if !m2.confirmOpen || m2.confirmKind != confirmKindPRPush {
		t.Fatal("no push card")
	}
	m2.confirmOpen = false
	if cmd, ok := m2.handlePRConfirm("push"); !ok || cmd == nil || m2.gitRemoteBusy == "" {
		t.Error("Push did not push")
	}
}
