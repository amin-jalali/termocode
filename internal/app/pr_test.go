package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

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
