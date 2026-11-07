package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// CMP-35: the PR pane of a worktree. P on a card whose worktree is
// linked to a pull request opens the PR view (checks, threads, merge);
// Checks… lists every check; a failed GitHub Actions check opens its
// failed log or fills the chat with it (the user still presses Enter).

const prViewText14Checks = `{"number":14,"title":"Add parser","body":"Adds a parser.","author":{"login":"ann"},
 "state":"OPEN","isDraft":false,"url":"https://github.com/o/r/pull/14","reviewDecision":"",
 "headRefName":"feat/parser","baseRefName":"main","mergeable":"MERGEABLE",
 "statusCheckRollup":[{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"FAILURE",
                       "detailsUrl":"https://github.com/o/r/actions/runs/777/job/9"},
                      {"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"SUCCESS",
                       "detailsUrl":"https://github.com/o/r/actions/runs/778/job/10"},
                      {"__typename":"StatusContext","context":"deploy","state":"FAILURE","targetUrl":"https://ci.example/9"}],
 "files":[{"path":"parser.go","additions":4,"deletions":1}],"reviews":[],"comments":[]}`

const prFailedLog777 = "test\tRun go test\t2026-11-01T10:00:00.0000000Z --- FAIL: TestParse\n" +
	"test\tRun go test\t2026-11-01T10:00:01.0000000Z FAIL\n"

// wtPRDrive runs cmd and every command it leads to, feeding the PR and CI
// handlers; toast timers are skipped.
func wtPRDrive(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for n := 0; len(queue) > 0 && n < 50; n++ {
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
		if next, ok := m.handlePRMsg(msg); ok {
			queue = append(queue, next)
			continue
		}
		if next, ok := m.handleCIMsg(msg); ok {
			queue = append(queue, next)
		}
	}
}

// wtPRModel is the worktrees view with the toast worktree selected and
// linked to PR 14; gh is the fake.
func wtPRModel(t *testing.T, linked bool) (*Model, *prFakeGH) {
	t.Helper()
	m := wtLinkModel(t)
	if linked {
		for i := range m.wt.entries {
			if m.wt.entries[i].Path == m.wt.cursorPath {
				m.wt.entries[i].Meta.PR = 14
			}
		}
	}
	f := &prFakeGH{answers: map[string]string{"pr view": prViewText14Checks, "run view": prFailedLog777}}
	m.ciW().run = f.run
	m.gitIsRepo, m.gitLoaded = true, true
	return m, f
}

// P opens the PR view of the linked pull request.
func TestWorktreePROpensPane(t *testing.T) {
	m, f := wtPRModel(t, true)
	next, cmd := m.handleWTSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	if cmd == nil {
		t.Fatal("P did nothing")
	}
	m2 := &next
	wtPRDrive(t, m2, cmd)
	if !f.called("pr view 14") {
		t.Fatalf("gh not asked for PR 14: %v", f.calls)
	}
	if !m2.prViewOpen() {
		t.Fatal("PR view not open")
	}
	ids := []string{}
	for _, b := range m2.confirm.Buttons() {
		ids = append(ids, b.ID)
	}
	if strings.Join(ids, " ") != "actions checks files close" {
		t.Errorf("buttons %v", ids)
	}
}

// P without a link says how to link one and calls no gh.
func TestWorktreePRNoLink(t *testing.T) {
	m, f := wtPRModel(t, false)
	cmd := m.worktreePR()
	if cmd == nil {
		t.Fatal("no hint")
	}
	cmd()
	if len(f.calls) > 0 || m.prViewOpen() {
		t.Errorf("gh called without a link: %v", f.calls)
	}
	if !toastHas(m, wtPRNoLinkHint) {
		t.Error("no hint toast")
	}
}

// Checks… lists every check with its state; a failed Actions check has
// Show Log and Fix with AI rows; other checks open their page.
func TestPRChecksPicker(t *testing.T) {
	m, _ := wtPRModel(t, true)
	wtPRDrive(t, m, m.openPRView(14))
	if cmd, ok := m.handlePRConfirm("checks"); !ok || cmd != nil || !m.pickerOpen || m.pickerKind != pickerKindPRChecks {
		t.Fatalf("checks picker not open (%v %v)", m.pickerOpen, m.pickerKind)
	}
	box := ansi.Strip(m.picker.Box())
	for _, want := range []string{"#14 Checks", "test", "build", "deploy", "Show Log", "Fix with AI"} {
		if !strings.Contains(box, want) {
			t.Errorf("picker misses %q:\n%s", want, box)
		}
	}
	items := prCheckItems(m.prS().detail.Checks)
	var logs, fixes int
	for _, it := range items {
		switch {
		case strings.HasPrefix(it.ID, "check-log-"):
			logs++
		case strings.HasPrefix(it.ID, "check-fix-"):
			fixes++
		}
	}
	if logs != 1 || fixes != 1 {
		t.Errorf("log rows %d, fix rows %d (want 1, 1): %+v", logs, fixes, items)
	}
}

// Show Log reads `gh run view <id> --log-failed` of that check's run.
func TestPRCheckShowLog(t *testing.T) {
	m, f := wtPRModel(t, true)
	wtPRDrive(t, m, m.openPRView(14))
	m.handlePRConfirm("checks")
	m.pickerOpen = false
	cmd, ok := m.handlePRPickerSelect("check-log-0")
	if !ok || cmd == nil {
		t.Fatal("Show Log did nothing")
	}
	msg := prCheckLogFetch(t, cmd)
	if !f.called("run view 777 --log-failed") {
		t.Fatalf("calls %v", f.calls)
	}
	if msg.fix || msg.run.ID != 777 || msg.log.Job != "test" {
		t.Errorf("log msg %+v", msg)
	}
	cmd, _ = m.handlePRPickerSelect("check-fix-0")
	if msg := prCheckLogFetch(t, cmd); !msg.fix || msg.branch != "feat/parser" {
		t.Errorf("fix msg %+v", msg)
	}
}

// prCheckLogFetch runs cmd (maybe a batch) and returns its ciLogMsg.
func prCheckLogFetch(t *testing.T, cmd tea.Cmd) ciLogMsg {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case ciLogMsg:
			return msg
		}
	}
	t.Fatal("no log was read")
	return ciLogMsg{}
}

// The palette row exists, its AI policy is set, and P is on the ? card.
func TestWorktreePRPaletteAndKeys(t *testing.T) {
	found := false
	for _, it := range worktreePaletteItems() {
		if it.ID == wtPRPaletteID {
			found = true
		}
	}
	if !found {
		t.Error("no palette row")
	}
	if _, ok := aiActionPolicyFor("worktree-pr"); !ok {
		t.Error("no AI policy")
	}
	keys := false
	for _, r := range worktreeKeyRows() {
		if r.Key == "P" {
			keys = true
		}
	}
	if !keys {
		t.Error("P missing on the key card")
	}
}
