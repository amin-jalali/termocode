package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
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
