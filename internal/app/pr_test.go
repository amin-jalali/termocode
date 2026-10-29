package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

// prFakeGH answers gh calls from a table keyed by the first two args and
// records every call. It never starts a process.
type prFakeGH struct {
	mu      sync.Mutex
	answers map[string]string // stdout per "pr list"
	errs    map[string]error
	stderr  map[string]string
	calls   []string
}

func (f *prFakeGH) run(_ context.Context, _ string, args ...string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, strings.Join(args, " "))
	key := strings.Join(args[:min(2, len(args))], " ")
	if err := f.errs[key]; err != nil {
		return "", f.stderr[key], err
	}
	out, ok := f.answers[key]
	if !ok {
		return "", "unknown command " + key, errors.New("exit status 1")
	}
	return out, "", nil
}

func (f *prFakeGH) called(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

const prListText = `[
 {"number":14,"title":"Add parser","author":{"login":"ann"},"headRefName":"feat/parser","baseRefName":"main","url":"https://github.com/o/r/pull/14",
  "isDraft":false,"reviewDecision":"APPROVED","statusCheckRollup":[{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"SUCCESS"}]},
 {"number":13,"title":"WIP cache","author":{"login":"bob"},"headRefName":"wip/cache","baseRefName":"main","url":"u13",
  "isDraft":true,"reviewDecision":"","statusCheckRollup":[{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"FAILURE"}]}]`

// prModel is a repo model on feat/x whose gh is the fake.
func prModel(f *prFakeGH) *Model {
	m := &Model{w: 160, h: 40, gitIsRepo: true, gitLoaded: true, gitBranch: git.Branch{Name: "feat/x"}}
	m.ciW().run = f.run
	return m
}

// prDo runs a command (not a batch), feeds its message to the PR handler
// and returns the follow-up command.
func prDo(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	msg := cmd()
	next, ok := m.handlePRMsg(msg)
	if !ok {
		t.Fatalf("message %T not handled", msg)
	}
	return next
}

// toastHas reports a visible toast with text.
func toastHas(m *Model, text string) bool {
	for _, c := range m.toast.Cards(160) {
		if strings.Contains(ansi.Strip(strings.Join(c.Lines, "\n")), text) {
			return true
		}
	}
	return false
}

// The list asks gh for the open PRs and shows number, title, author and
// branch in the filterable row, with draft, checks and review as hint.
func TestPRListPicker(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr list": prListText}}
	m := prModel(f)
	prDo(t, m, m.prListCmd())
	if !m.pickerOpen || m.pickerKind != pickerKindPRList {
		t.Fatal("picker not open")
	}
	box := ansi.Strip(m.picker.Box())
	for _, want := range []string{"Pull Requests", "#14 Add parser · ann · feat/parser", "✓ checks · Approved", "#13 WIP cache · bob", "Draft · ✗ checks"} {
		if !strings.Contains(box, want) {
			t.Errorf("picker misses %q:\n%s", want, box)
		}
	}
	if !f.called("pr list --state open") {
		t.Errorf("calls %v", f.calls)
	}
}

// Without gh or a login the user gets the one quiet hint, no picker.
func TestPRListNoGH(t *testing.T) {
	f := &prFakeGH{errs: map[string]error{"pr list": errors.New("exit 4")}, stderr: map[string]string{"pr list": "gh auth login"}}
	m := prModel(f)
	prDo(t, m, m.prListCmd())
	if m.pickerOpen {
		t.Error("picker open")
	}
	if !toastHas(m, ciGHHint) {
		t.Errorf("no hint toast")
	}
}

func TestPRListEmptyAndNoRepo(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr list": "[]"}}
	m := prModel(f)
	prDo(t, m, m.prListCmd())
	if m.pickerOpen || !toastHas(m, "No open pull requests") {
		t.Error("empty list")
	}
	m = prModel(&prFakeGH{})
	m.gitIsRepo = false
	if cmd := m.prListCmd(); cmd != nil {
		t.Error("asked gh outside a repo")
	}
}

const prViewText14 = `{"number":14,"title":"Add parser","body":"Adds a parser.\n\n- one\n- two","author":{"login":"ann"},
 "state":"OPEN","isDraft":false,"url":"https://github.com/o/r/pull/14","reviewDecision":"CHANGES_REQUESTED",
 "headRefName":"feat/parser","baseRefName":"main","mergeable":"CONFLICTING",
 "statusCheckRollup":[{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"FAILURE"},
                      {"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"SUCCESS"}],
 "files":[{"path":"parser.go","additions":40,"deletions":2}],
 "reviews":[{"author":{"login":"bob"},"state":"CHANGES_REQUESTED","body":"Please add tests","submittedAt":"2026-10-28T10:00:00Z"}],
 "comments":[{"author":{"login":"ann"},"body":"Added.","createdAt":"2026-10-28T11:00:00Z"}]}`

// prViewModel has the PR 14 view open.
func prViewModel(t *testing.T) (*Model, *prFakeGH) {
	t.Helper()
	f := &prFakeGH{answers: map[string]string{"pr list": prListText, "pr view": prViewText14}}
	m := prModel(f)
	prDo(t, m, m.prViewCmd(14))
	return m, f
}

// The view shows title, state, branches, checks, files, description and
// reviews; Actions… is the focused first button.
func TestPRViewCard(t *testing.T) {
	m, f := prViewModel(t)
	if !m.prViewOpen() {
		t.Fatal("view not open")
	}
	if box := ansi.Strip(m.confirm.Box()); !strings.Contains(box, "#14 Pull Request") || !strings.Contains(box, "Add parser") {
		t.Errorf("card:\n%s", box)
	}
	box := prViewText(*m.prS().detail) // the card scrolls: check the whole text
	for _, want := range []string{"Add parser", "Open · Changes requested", "feat/parser → main · by ann",
		"Has merge conflicts with main", "✗ test", "✓ build", "Files (1)", "parser.go  +40 -2", "Description", "Adds a parser.",
		"Reviews and comments", "bob · Changes requested", "Please add tests", "ann"} {
		if !strings.Contains(box, want) {
			t.Errorf("card misses %q:\n%s", want, box)
		}
	}
	b := m.confirm.Buttons()
	if len(b) != 3 || b[0].ID != "actions" || b[1].ID != "files" || b[2].ID != "close" {
		t.Errorf("buttons %+v", b)
	}
	if !f.called("pr view 14 --json") {
		t.Errorf("calls %v", f.calls)
	}
}

// Enter on a row of the list opens that pull request.
func TestPRListEnterOpensView(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr list": prListText, "pr view": prViewText14}}
	m := prModel(f)
	prDo(t, m, m.prListCmd())
	cmd, ok := m.handlePRPickerSelect("#14")
	if !ok || cmd == nil {
		t.Fatal("no command for the chosen row")
	}
	m.pickerOpen = false
	if cmd, ok = m.handlePRPickerSelect("#bogus"); !ok || cmd != nil {
		t.Error("a bad id did something")
	}
}

func TestPRViewError(t *testing.T) {
	f := &prFakeGH{errs: map[string]error{"pr view": errors.New("exit 1")}, stderr: map[string]string{"pr view": "Could not resolve to a PullRequest"}}
	m := prModel(f)
	prDo(t, m, m.prViewCmd(99))
	if m.prViewOpen() || !toastHas(m, "Could not read #99") {
		t.Error("error not shown")
	}
}

const prDiffText14 = `diff --git a/parser.go b/parser.go
--- a/parser.go
+++ b/parser.go
@@ -1,3 +1,3 @@
 package p
-func Old() {}
+func New() {}
 var x = 1
diff --git a/docs/new.md b/docs/new.md
new file mode 100644
--- /dev/null
+++ b/docs/new.md
@@ -0,0 +1 @@
+# Title
`

// Files… reads `gh pr diff` and opens the diff card with every file; the
// view card closes and the open card is not the AI review.
func TestPRFilesOpensDiffCard(t *testing.T) {
	m, f := prViewModel(t)
	f.answers["pr diff"] = prDiffText14
	prDo(t, m, m.prDiffCmd(14))
	if !m.reviewOpen || m.confirmOpen {
		t.Fatalf("diff card %v confirm %v", m.reviewOpen, m.confirmOpen)
	}
	card := m.aiS().review.card
	if card.Files() != 2 || card.Review() {
		t.Errorf("files %d review %v", card.Files(), card.Review())
	}
	box := ansi.Strip(card.Box())
	for _, want := range []string{"#14 Add parser", "parser.go", "new.md", "2 files"} {
		if !strings.Contains(box, want) {
			t.Errorf("card misses %q:\n%s", want, box)
		}
	}
	if !f.called("pr diff 14") {
		t.Errorf("calls %v", f.calls)
	}
}

// The Files… button asks for the diff of the pull request on screen.
func TestPRFilesButton(t *testing.T) {
	m, _ := prViewModel(t)
	cmd, ok := m.handlePRConfirm("files")
	if !ok || cmd == nil {
		t.Fatal("Files… did nothing")
	}
}

func TestPRDiffError(t *testing.T) {
	m, f := prViewModel(t)
	f.errs = map[string]error{"pr diff": errors.New("exit 1")}
	f.stderr = map[string]string{"pr diff": "diff too large"}
	prDo(t, m, m.prDiffCmd(14))
	if m.reviewOpen || !toastHas(m, "Could not read the changes of #14") {
		t.Error("error not shown")
	}
}

func TestPRDiffCardFiles(t *testing.T) {
	files, add, del := prDiffCardFiles(ghci.ParseDiff(prDiffText14))
	if len(files) != 2 || add != 2 || del != 1 || files[1].Status != 'A' || files[0].Status != 'M' {
		t.Errorf("files %+v add %d del %d", files, add, del)
	}
}

// Actions… opens a picker; an open non-draft PR can be checked out,
// reviewed, converted to draft and merged.
func TestPRActionsPicker(t *testing.T) {
	m, _ := prViewModel(t)
	cmd, ok := m.handlePRConfirm("actions")
	if !ok || cmd != nil || !m.pickerOpen || m.pickerKind != pickerKindPRActions || m.confirmOpen {
		t.Fatalf("picker open %v kind %v", m.pickerOpen, m.pickerKind)
	}
	box := ansi.Strip(m.picker.Box())
	for _, want := range []string{"#14 Actions", "Open in Browser", "Check Out Branch", "Approve…", "Request Changes…", "Comment…", "Convert to Draft", "Merge…"} {
		if !strings.Contains(box, want) {
			t.Errorf("picker misses %q:\n%s", want, box)
		}
	}
	if strings.Contains(box, "Mark Ready") {
		t.Error("Mark Ready on a PR that is not a draft")
	}
}

func TestPRActionItemsByState(t *testing.T) {
	ids := func(d ghci.PRDetail) string {
		var out []string
		for _, it := range prActionItems(d) {
			out = append(out, it.ID)
		}
		return strings.Join(out, ",")
	}
	if got := ids(ghci.PRDetail{State: "OPEN", PRItem: ghci.PRItem{Draft: true}}); !strings.Contains(got, "ready") || strings.Contains(got, "draft") {
		t.Errorf("draft: %s", got)
	}
	if got := ids(ghci.PRDetail{State: "MERGED"}); got != "browser,description" {
		t.Errorf("merged PR offers %s", got)
	}
}

// Open in Browser opens the PR link, or copies it over SSH.
func TestPROpenBrowser(t *testing.T) {
	m, _ := prViewModel(t)
	var opened, copied []string
	old := osOpen
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	defer func() { osOpen = old }()
	defer SetClipboardSink(func(s string) { copied = append(copied, s) })()
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	m.onPRActionPicked("browser")
	if len(opened) != 1 || opened[0] != "https://github.com/o/r/pull/14" {
		t.Errorf("opened %v", opened)
	}
	t.Setenv("SSH_CONNECTION", "1.2.3.4 5 6.7.8.9 22")
	m.onPRActionPicked("browser")
	if len(opened) != 1 || len(copied) != 1 || copied[0] != "https://github.com/o/r/pull/14" {
		t.Errorf("over SSH: opened %v copied %v", opened, copied)
	}
}

// An empty description says so instead of opening an empty page.
func TestPRReadDescriptionEmpty(t *testing.T) {
	m, _ := prViewModel(t)
	m.prS().detail.Body = "  "
	m.onPRActionPicked("description")
	if !toastHas(m, "has no description") {
		t.Error("no hint")
	}
}

// prRepoModel is prModel whose folder is a fresh temp git repo with one
// commit, so tests that read the git state never touch this repository.
func prRepoModel(t *testing.T, f *prFakeGH) (*Model, string) {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"}, {"checkout", "-q", "-b", "main"}} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "a.txt"}, {"commit", "-q", "-m", "one"}} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	ws, err := localhost.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := prModel(f)
	m.ws = ws
	prDo(t, m, m.prViewCmd(14))
	return m, dir
}

// Check Out runs `gh pr checkout` on a clean tree and refreshes git.
func TestPRCheckout(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr view": prViewText14, "pr checkout": ""}}
	m, _ := prRepoModel(t, f)
	cmd := m.onPRActionPicked("checkout")
	if cmd == nil {
		t.Fatal("no command")
	}
	msg := cmd()
	done, ok := msg.(prDoneMsg)
	if !ok || done.err != nil || !done.refresh {
		t.Fatalf("msg %#v", msg)
	}
	if !f.called("pr checkout 14") {
		t.Errorf("calls %v", f.calls)
	}
	m.onPRDone(done)
	if !toastHas(m, "Checked out #14") {
		t.Error("no success toast")
	}
}

// A tree with changes to tracked files refuses, says why, and never calls gh.
func TestPRCheckoutRefusesDirtyTree(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr view": prViewText14, "pr checkout": ""}}
	m, dir := prRepoModel(t, f)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.onPRActionPicked("checkout")
	if f.called("pr checkout") {
		t.Fatal("gh pr checkout ran on a dirty tree")
	}
	if !toastHas(m, "Not checked out: you have changes") || !toastHas(m, "Commit, stash or discard") {
		t.Error("no explanation")
	}
}

// A failing gh shows its error.
func TestPRCheckoutFails(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr view": prViewText14},
		errs: map[string]error{"pr checkout": errors.New("exit 1")}, stderr: map[string]string{"pr checkout": "fatal: couldn't find remote ref"}}
	m, _ := prRepoModel(t, f)
	m.onPRDone(m.onPRActionPicked("checkout")().(prDoneMsg))
	if !toastHas(m, "Could not check out #14") {
		t.Error("no failure toast")
	}
}

// Approve, Request Changes and Comment open a prompt; its text goes to
// `gh pr review` as a separate argument, and the card reopens.
func TestPRReviewPrompt(t *testing.T) {
	for _, c := range []struct {
		pick, flag, text, toast string
		empty                   bool
	}{
		{"approve", "--approve", "ship it", "Approved #14", true},
		{"request-changes", "--request-changes", "add a test", "Asked for changes on #14", false},
		{"comment", "--comment", "why not a map?", "Commented on #14", false},
	} {
		f := &prFakeGH{answers: map[string]string{"pr view": prViewText14, "pr review": ""}}
		m, _ := prViewModel(t)
		m.ciW().run = f.run
		m.onPRActionPicked(c.pick)
		if !m.promptOpen || m.promptKind != promptKindPRReview {
			t.Fatalf("%s: prompt not open", c.pick)
		}
		cmd, ok := m.handlePRPromptSubmit(c.text)
		if !ok || cmd == nil {
			t.Fatalf("%s: no command", c.pick)
		}
		done := cmd().(prDoneMsg)
		if done.err != nil || !done.reopen {
			t.Fatalf("%s: %+v", c.pick, done)
		}
		want := "pr review 14 " + c.flag + " --body " + c.text
		if !f.called(want) {
			t.Errorf("%s: calls %v, want %q", c.pick, f.calls, want)
		}
		m.onPRDone(done)
		if !toastHas(m, c.toast) {
			t.Errorf("%s: no toast %q", c.pick, c.toast)
		}
	}
}

// Request Changes and Comment do not accept an empty message; Approve does.
func TestPRReviewEmptyMessage(t *testing.T) {
	m, _ := prViewModel(t)
	m.onPRActionPicked("comment")
	m.prompt, _ = m.prompt.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.promptOpen {
		t.Fatal("prompt closed")
	}
	if _, cmd := m.prompt.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Error("an empty comment was submitted")
	}
	m.onPRActionPicked("approve")
	if _, cmd := m.prompt.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Error("an empty approval was refused")
	}
}

// gh's refusal (for example approving one's own PR) is shown.
func TestPRReviewError(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr view": prViewText14},
		errs: map[string]error{"pr review": errors.New("exit 1")}, stderr: map[string]string{"pr review": "Can not approve your own pull request"}}
	m, _ := prViewModel(t)
	m.ciW().run = f.run
	m.onPRActionPicked("approve")
	cmd, _ := m.handlePRPromptSubmit("")
	m.onPRDone(cmd().(prDoneMsg))
	if !toastHas(m, "Could not send the review of #14") {
		t.Error("no failure toast")
	}
}

// Other prompts are not taken by the review handler.
func TestPRPromptNotOurs(t *testing.T) {
	m := prModel(&prFakeGH{})
	m.promptKind = promptKindRename
	if _, ok := m.handlePRPromptSubmit("x"); ok {
		t.Error("took a rename prompt")
	}
}

// Merge… opens a card with Cancel first and focused, the three methods,
// and a warning for red checks, conflicts and requested changes.
func TestPRMergeCard(t *testing.T) {
	m, _ := prViewModel(t)
	m.onPRActionPicked("merge")
	if !m.confirmOpen || m.confirmKind != confirmKindPRMerge {
		t.Fatal("merge card not open")
	}
	b := m.confirm.Buttons()
	if len(b) != 4 || b[0].ID != "cancel" || b[1].ID != "squash" || b[2].ID != "merge" || b[3].ID != "rebase" {
		t.Fatalf("buttons %+v", b)
	}
	text := prMergeText(*m.prS().detail)
	for _, want := range []string{`Merge #14 "Add parser" from feat/parser into main?`, "Warning: 1 of 2 checks failed.",
		"Warning: the branch has conflicts with main.", "Warning: a reviewer asked for changes."} {
		if !strings.Contains(text, want) {
			t.Errorf("text misses %q:\n%s", want, text)
		}
	}
}

func TestPRMergeWarningsPendingAndClean(t *testing.T) {
	d := ghci.PRDetail{State: "OPEN", Mergeable: "MERGEABLE", PRItem: ghci.PRItem{Number: 1, Checks: []ghci.Check{{Name: "a", State: ghci.StatePending}}}}
	if w := prMergeWarnings(d); len(w) != 1 || !strings.Contains(w[0], "still running") {
		t.Errorf("pending: %v", w)
	}
	d.Checks = []ghci.Check{{Name: "a", State: ghci.StatePass}}
	if w := prMergeWarnings(d); len(w) != 0 || !strings.Contains(prMergeText(d), "All checks passed.") {
		t.Errorf("clean: %v", w)
	}
}

// Each method calls `gh pr merge` with its flag, never with --admin; the
// card then reopens with the new state.
func TestPRMergeRuns(t *testing.T) {
	for id, flag := range map[string]string{"squash": "--squash", "merge": "--merge", "rebase": "--rebase"} {
		f := &prFakeGH{answers: map[string]string{"pr view": prViewText14, "pr merge": ""}}
		m, _ := prViewModel(t)
		m.ciW().run = f.run
		m.onPRActionPicked("merge")
		cmd, ok := m.handlePRConfirm(id)
		if !ok || cmd == nil {
			t.Fatalf("%s: no command", id)
		}
		done := cmd().(prDoneMsg)
		if done.err != nil || !done.reopen {
			t.Fatalf("%s: %+v", id, done)
		}
		if !f.called("pr merge 14 " + flag) {
			t.Errorf("%s: calls %v", id, f.calls)
		}
		if f.called("pr merge 14 " + flag + " ") {
			t.Errorf("%s: extra flags %v", id, f.calls)
		}
		m.onPRDone(done)
		if !toastHas(m, "Merged #14") {
			t.Errorf("%s: no toast", id)
		}
	}
}

// Cancel does not call gh; a draft, a closed PR or an unknown id never merge.
func TestPRMergeCancelAndRefusals(t *testing.T) {
	m, f := prViewModel(t)
	m.onPRActionPicked("merge")
	if cmd, _ := m.handlePRConfirm("cancel"); cmd != nil {
		t.Error("Cancel ran something")
	}
	if cmd, _ := m.handlePRConfirm("bogus"); cmd != nil {
		t.Error("unknown id ran something")
	}
	m.prS().detail.Draft = true
	m.confirmOpen = false
	m.onPRActionPicked("merge")
	if m.confirmOpen || !toastHas(m, "is a draft") {
		t.Error("draft got a merge card")
	}
	m.prS().detail.Draft = false
	m.prS().detail.State = "MERGED"
	m.onPRActionPicked("merge")
	if m.confirmOpen || !toastHas(m, "is not open") || f.called("pr merge") {
		t.Error("merged PR got a merge card")
	}
}

// gh's refusal (branch rules) is shown; no retry with admin rights.
func TestPRMergeRefused(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr view": prViewText14},
		errs: map[string]error{"pr merge": errors.New("exit 1")}, stderr: map[string]string{"pr merge": "Pull request is not mergeable: the base branch policy prohibits the merge."}}
	m, _ := prViewModel(t)
	m.ciW().run = f.run
	m.onPRActionPicked("merge")
	cmd, _ := m.handlePRConfirm("squash")
	m.onPRDone(cmd().(prDoneMsg))
	if !toastHas(m, "Could not merge #14") {
		t.Error("no failure toast")
	}
	for _, c := range f.calls {
		if strings.Contains(c, "admin") {
			t.Errorf("call %q", c)
		}
	}
}

// Mark Ready and Convert to Draft call `gh pr ready` and reopen the card.
func TestPRSetDraft(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr view": prViewText14, "pr ready": ""}}
	m, _ := prViewModel(t)
	m.ciW().run = f.run
	cmd := m.onPRActionPicked("draft")
	done := cmd().(prDoneMsg)
	if done.err != nil || !done.reopen || !f.called("pr ready 14 --undo") {
		t.Fatalf("draft: %+v calls %v", done, f.calls)
	}
	m.onPRDone(done)
	if !toastHas(m, "Converted #14 to a draft") {
		t.Error("no toast")
	}
	m.prS().detail.Draft = true
	done = m.onPRActionPicked("ready")().(prDoneMsg)
	if !f.called("pr ready 14") || !strings.Contains(done.ok, "ready for review") {
		t.Errorf("ready: %+v calls %v", done, f.calls)
	}
	// Already a draft / already ready: nothing to do.
	if m.onPRActionPicked("draft") != nil {
		t.Error("converted a draft to a draft")
	}
}
