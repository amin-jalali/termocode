package app

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/worktreeform"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// wtFakeGH answers gh pr view / issue view from a table keyed by
// "pr 12" / "issue 412"; it records every call.
type wtFakeGH struct {
	answers map[string]string
	calls   []string
}

func (f *wtFakeGH) run(_ context.Context, _ string, args ...string) (string, string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if len(args) >= 3 {
		if out, ok := f.answers[args[0]+" "+args[2]]; ok {
			return out, "", nil
		}
	}
	return "", "GraphQL: Could not resolve to a PullRequest with the number of 1.", wtErrString("exit status 1")
}

type wtErrString string

func (e wtErrString) Error() string { return string(e) }

func wtFakeGHSeam(t *testing.T, answers map[string]string) *wtFakeGH {
	t.Helper()
	f := &wtFakeGH{answers: answers}
	old := wtGHRun
	wtGHRun = func() ghci.Runner { return f.run }
	t.Cleanup(func() { wtGHRun = old })
	return f
}

// wtLinkDrive runs the link card's commands and messages.
func wtLinkDrive(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if b, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, b...)
			continue
		}
		if next, ok := m.handleWTMsg(msg); ok {
			queue = append(queue, next)
		}
	}
}

func wtLinkModel(t *testing.T) *Model {
	t.Helper()
	withConfigDir(t)
	st, _ := wtFixture(t)
	st.common = t.TempDir()
	st.root = "/src/app"
	m := &Model{w: 150, h: 42, wt: st, focus: FocusExplorer, explorerWidth: 40}
	st.cursorPath = "/src/app.worktrees/toast"
	return m
}

// WTX-9: i opens the link card (Cancel first); a PR number links it:
// gh gives the title, state and CI; the card line shows "PR #12 open ·
// CI ✓"; Unlink takes it off.
func TestWorktreeLinkPR(t *testing.T) {
	gh := wtFakeGHSeam(t, map[string]string{"pr 12": `{"number":12,"title":"Fix the toast race","state":"OPEN","url":"https://github.com/o/r/pull/12",
		"statusCheckRollup":[{"__typename":"CheckRun","name":"CI","status":"COMPLETED","conclusion":"SUCCESS"}]}`})
	m := wtLinkModel(t)
	wtKey(t, m, "i")
	if !m.connectOpen || m.connect.Card().Title != wtLinkTitle {
		t.Fatalf("no link card: open %v", m.connectOpen)
	}
	if b := m.connect.Card().Buttons; len(b) == 0 || b[0].ID != "cancel" {
		t.Fatalf("Cancel is not first: %+v", b)
	}
	m.connect.SetInputValue("ref", "#12")
	wtLinkDrive(t, m, func() tea.Msg { return connectcard.ButtonMsg{ID: "link"} })
	if m.connectOpen {
		t.Fatalf("card still open: %s", ansi.Strip(m.connect.View()))
	}
	md, _ := worktrees.Load(m.wt.common)
	got := md["/src/app.worktrees/toast"]
	if got.PR != 12 || got.Issue != 0 || got.Link.Title != "Fix the toast race" || got.Link.State != "open" || got.Link.CI != "pass" || got.Link.Checked.IsZero() {
		t.Fatalf("meta %+v", got)
	}
	if len(gh.calls) != 1 || !strings.HasPrefix(gh.calls[0], "pr view 12 ") {
		t.Errorf("gh calls %v", gh.calls)
	}
	var it wtItem
	items, _ := m.wtList()
	for _, x := range items {
		if x.e.Path == "/src/app.worktrees/toast" {
			it = x
		}
	}
	lines := ansi.Strip(strings.Join(wtCardLines(it, wtStatus{known: true}, false, false, 40, wtTestNow), "\n"))
	if !strings.Contains(lines, "PR #12 open · CI ✓") {
		t.Errorf("card:\n%s", lines)
	}
	// Unlink.
	wtKey(t, m, "i")
	wtLinkDrive(t, m, func() tea.Msg { return connectcard.ButtonMsg{ID: "unlink"} })
	if md, _ := worktrees.Load(m.wt.common); md["/src/app.worktrees/toast"].PR != 0 || md["/src/app.worktrees/toast"].Link.Title != "" {
		t.Errorf("unlink left %+v", md["/src/app.worktrees/toast"])
	}
}

// A bad URL stays on the card and gh never runs; without gh the number
// is still linked and a toast says why there is no title.
func TestWorktreeLinkBadInputAndNoGH(t *testing.T) {
	gh := wtFakeGHSeam(t, nil)
	m := wtLinkModel(t)
	wtKey(t, m, "i")
	m.connect.SetInputValue("ref", "https://evil.example/o/r/pull/1")
	wtLinkDrive(t, m, func() tea.Msg { return connectcard.ButtonMsg{ID: "link"} })
	if !m.connectOpen || !strings.Contains(ansi.Strip(m.connect.View()), "not this repository's GitHub host") {
		t.Fatalf("no error on the card:\n%s", ansi.Strip(m.connect.View()))
	}
	if len(gh.calls) != 0 {
		t.Errorf("gh ran for a refused URL: %v", gh.calls)
	}
	old := wtGHRun
	wtGHRun = func() ghci.Runner { return ciNoGH }
	defer func() { wtGHRun = old }()
	m.connect.SetInputValue("ref", "412")
	wtLinkDrive(t, m, func() tea.Msg { return connectcard.ButtonMsg{ID: "link"} })
	if md, _ := worktrees.Load(m.wt.common); md["/src/app.worktrees/toast"].Issue != 412 {
		t.Errorf("number not kept without gh: %+v", md["/src/app.worktrees/toast"])
	}
	if !strings.Contains(wtLastToast(m), "#412") {
		t.Errorf("toast %q", wtLastToast(m))
	}
	if p, ok := aiActionPolicyFor(wtLinkPaletteID); !ok || p.Gate == aiGateAuto {
		t.Errorf("the AI may set a link: %+v", p)
	}
}

// The card line of a link, and when a link is read again: open pull
// requests with pending CI on the fast cadence, the rest slowly, merged
// and closed ones never.
func TestWorktreeLinkLineAndRefresh(t *testing.T) {
	for _, c := range []struct {
		md   worktrees.Meta
		want string
	}{
		{worktrees.Meta{Issue: 412, Link: worktrees.LinkInfo{State: "open"}}, "#412 open"},
		{worktrees.Meta{PR: 12, Link: worktrees.LinkInfo{State: "merged", CI: "pass"}}, "PR #12 merged · CI ✓"},
		{worktrees.Meta{PR: 9, Link: worktrees.LinkInfo{State: "open", CI: "fail"}}, "PR #9 open · CI ✗"},
		{worktrees.Meta{PR: 9, Link: worktrees.LinkInfo{State: "open", CI: "pending"}}, "PR #9 open · CI ●"},
		{worktrees.Meta{Issue: 3}, "#3"},
		{worktrees.Meta{}, ""},
	} {
		if got := wtLinkLine(c.md); got != c.want {
			t.Errorf("wtLinkLine(%+v) = %q, want %q", c.md, got, c.want)
		}
	}
	now := clock.Now()
	due := func(md worktrees.Meta) bool { return wtLinkDue(md, now) }
	if !due(worktrees.Meta{Issue: 1}) {
		t.Error("a link never read is not due")
	}
	if due(worktrees.Meta{PR: 1, Link: worktrees.LinkInfo{State: "open", CI: "pass", Checked: now.Add(-time.Minute)}}) {
		t.Error("an idle PR read a minute ago is due")
	}
	if !due(worktrees.Meta{PR: 1, Link: worktrees.LinkInfo{State: "open", CI: "pending", Checked: now.Add(-time.Minute)}}) {
		t.Error("a PR with CI running read a minute ago is not due")
	}
	if due(worktrees.Meta{PR: 1, Link: worktrees.LinkInfo{State: "merged", Checked: now.Add(-48 * time.Hour)}}) {
		t.Error("a merged PR is read again")
	}
}

// WTX-9: Worktree: New… with an issue and no name: the branch comes
// from the issue title and the new worktree is linked to it.
func TestWorktreeNewFromIssue(t *testing.T) {
	wtFakeGHSeam(t, map[string]string{"issue 412": `{"number":412,"title":"Pinned row in WORKTREES","state":"OPEN","url":"u"}`})
	var made []string
	old := wtCreateRun
	wtCreateRun = func(_ *wtForm, folder, branch, _ string) ([]worktreeform.File, error) {
		made = append(made, branch+" "+folder)
		return nil, nil
	}
	defer func() { wtCreateRun = old }()
	m := wtFormModel(t)
	if !strings.Contains(ansi.Strip(m.connect.View()), "Issue") {
		t.Fatalf("no Issue row:\n%s", ansi.Strip(m.connect.View()))
	}
	m.connect.SetInputValue("issue", "412")
	m.refreshWTForm()
	wtDrive(t, m, func() tea.Msg { return connectcard.ButtonMsg{ID: "create"} })
	if len(made) != 1 || made[0] != "412-pinned-row-in-worktrees /src/app.worktrees/412-pinned-row-in-worktrees" {
		t.Fatalf("made %v (card: %s)", made, ansi.Strip(m.connect.View()))
	}
	if md, _ := worktrees.Load(m.wt.common); md["/src/app.worktrees/412-pinned-row-in-worktrees"].Issue != 412 {
		t.Errorf("not linked: %+v", md)
	}
}
