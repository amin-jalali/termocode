package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/worktreeform"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// wtFormModel is a model with Worktree: New… open on the demo form.
func wtFormModel(t *testing.T) *Model {
	t.Helper()
	withConfigDir(t)
	m := &Model{w: 150, h: 42, wt: newWTState()}
	m.wt.common = t.TempDir()
	f := wtDemoForm()
	f.common = m.wt.common
	m.wt.form = f
	m.connect = connectcard.New(m.wtFormCard())
	m.connect.SetSize(m.w, m.h)
	m.connectOpen = true
	return m
}

func wtFormKey(t *testing.T, m *Model, k tea.KeyMsg) tea.Cmd {
	t.Helper()
	nm, cmd := m.routeToWTForm(k)
	*m = nm
	return cmd
}

func wtType(t *testing.T, m *Model, s string) {
	t.Helper()
	for _, r := range s {
		wtFormKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// runCmd runs a cmd and its batch and feeds every message to the app's
// worktree handlers; returns the messages.
func wtDrive(t *testing.T, m *Model, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	var out []tea.Msg
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		switch v := msg.(type) {
		case tea.BatchMsg:
			queue = append(queue, v...)
			continue
		case nil:
			continue
		}
		out = append(out, msg)
		switch msg.(type) {
		case connectcard.ButtonMsg, connectcard.CloseMsg, wtCreatedMsg, wtNewPrepMsg, wtSetupDoneMsg:
			next, _ := m.handleWTMsg(msg)
			queue = append(queue, next)
		}
	}
	return out
}

// The branch is drafted from the name; a typed branch wins; the folder
// follows the branch (<repo>.worktrees/<branch with ->).
func TestWTFormDraft(t *testing.T) {
	f := &wtForm{base: "/src/app.worktrees"}
	if b, d := f.draft(wtFormVals{name: "Add a pinned row"}); b != "add-a-pinned-row" || d != "/src/app.worktrees/add-a-pinned-row" {
		t.Errorf("draft %q %q", b, d)
	}
	if b, d := f.draft(wtFormVals{name: "x", branch: "feat/pin"}); b != "feat/pin" || d != "/src/app.worktrees/feat-pin" {
		t.Errorf("typed branch %q %q", b, d)
	}
	if b, d := f.draft(wtFormVals{name: "!!"}); b != "" || d != "" {
		t.Errorf("empty %q %q", b, d)
	}
}

// The card: Cancel is the first button, nothing is lit until the user
// moves there, the folder follows the typed name, and what the config
// adds is listed by name only.
func TestWTFormCardSafeFirst(t *testing.T) {
	m := wtFormModel(t)
	c := m.connect.Card()
	if c.Title != wtFormTitle || len(c.Buttons) != 4 || c.Buttons[0].ID != "cancel" {
		t.Fatalf("buttons %+v", c.Buttons)
	}
	for _, b := range c.Buttons {
		if b.Primary {
			t.Errorf("%s is primary: Enter in a field would press it", b.ID)
		}
	}
	wtType(t, m, "feat/pin row")
	box := ansi.Strip(m.connect.Box())
	for _, want := range []string{"Name", "From", "main", "Branch", "feat/pin-row", "Agent", "Claude Code", "Task", "/src/app.worktrees/feat-pin-row",
		"copy .env, .env.local (2 files)", "link node_modules", "both sides see writes", "run setup: pnpm install", "asks first", "skip secrets.env (not there)",
		"Cancel", "Start point…", "Create + Start"} {
		if !strings.Contains(box, want) {
			t.Errorf("card lacks %q:\n%s", want, box)
		}
	}
	// A low window: the Also rows are one line.
	m.h = 24
	m.refreshWTForm()
	box = ansi.Strip(m.connect.Box())
	for _, want := range []string{"Also: copy 2", "link 1", "setup asks first", "skip 1"} {
		if !strings.Contains(box, want) || strings.Contains(box, ".env.local") {
			t.Errorf("short card lacks %q (or lists names):\n%s", want, box)
		}
	}
}

// Enter in a field goes to the next one and makes nothing; Esc closes
// the card and makes nothing.
func TestWTFormEnterNeverCreates(t *testing.T) {
	m := wtFormModel(t)
	calls := 0
	old := wtCreateRun
	wtCreateRun = func(*wtForm, string, string, string) ([]worktreeform.File, error) { calls++; return nil, nil }
	t.Cleanup(func() { wtCreateRun = old })
	wtType(t, m, "feat/x")
	for range 6 {
		if cmd := wtFormKey(t, m, tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
			wtDrive(t, m, cmd)
		}
		if m.connect.FocusedID() == "" {
			break
		}
	}
	if calls != 0 || !m.wtFormOpen() {
		t.Fatalf("Enter in the fields made a worktree (%d) or closed the card", calls)
	}
	// Now on the buttons: Cancel is lit first.
	wtDrive(t, m, wtFormKey(t, m, tea.KeyMsg{Type: tea.KeyEnter}))
	if calls != 0 || m.wtFormOpen() || m.wt.form != nil {
		t.Fatalf("Enter on the first button: calls %d open %v", calls, m.wtFormOpen())
	}

	m = wtFormModel(t)
	wtType(t, m, "feat/y")
	wtDrive(t, m, wtFormKey(t, m, tea.KeyMsg{Type: tea.KeyEsc}))
	if calls != 0 || m.connectOpen || m.wt.form != nil {
		t.Fatalf("Esc: calls %d open %v", calls, m.connectOpen)
	}
}

// Create: the worktree is made from the From ref on the drafted branch,
// the card closes, the name is saved; an error stays on the card.
func TestWTFormCreate(t *testing.T) {
	m := wtFormModel(t)
	var got []string
	fail := true
	old := wtCreateRun
	wtCreateRun = func(f *wtForm, folder, branch, ref string) ([]worktreeform.File, error) {
		got = append(got, folder+"|"+branch+"|"+ref)
		if fail {
			return nil, os.ErrExist
		}
		return []worktreeform.File{{Rel: ".env"}, {Rel: "x", Skip: "not there"}}, nil
	}
	t.Cleanup(func() { wtCreateRun = old })
	wtType(t, m, "Pin row")
	wtDrive(t, m, func() tea.Msg { return connectcard.ButtonMsg{ID: "create"} })
	if len(got) != 1 || got[0] != "/src/app.worktrees/pin-row|pin-row|main" {
		t.Fatalf("create %v", got)
	}
	if !m.wtFormOpen() || !strings.Contains(ansi.Strip(m.connect.Box()), "file already exists") {
		t.Fatalf("the error is not on the card:\n%s", ansi.Strip(m.connect.Box()))
	}
	fail = false
	wtDrive(t, m, func() tea.Msg { return connectcard.ButtonMsg{ID: "create"} })
	if m.connectOpen || m.wt.form != nil {
		t.Fatal("the card stayed open after a good create")
	}
	if m.wt.cursorPath != "/src/app.worktrees/pin-row" {
		t.Errorf("cursor %q", m.wt.cursorPath)
	}
	metas, err := worktrees.Load(m.wt.common)
	if err != nil || metas["/src/app.worktrees/pin-row"].Created.IsZero() {
		t.Errorf("meta %+v %v", metas, err)
	}
	if tt := wtLastToast(m); !strings.Contains(tt, "pin-row is ready") || !strings.Contains(tt, "x (not there)") {
		t.Errorf("toast %q", tt)
	}
	// The setup of the demo config asks first (not trusted).
	if !m.confirmOpen || m.confirmKind != confirmKindWTSetupTrust {
		t.Error("no setup trust card after create")
	}
}

// Create + Start without an agent says so; nothing is made. A name is
// needed.
func TestWTFormNeedsNameAndAgent(t *testing.T) {
	m := wtFormModel(t)
	calls := 0
	old := wtCreateRun
	wtCreateRun = func(*wtForm, string, string, string) ([]worktreeform.File, error) { calls++; return nil, nil }
	t.Cleanup(func() { wtCreateRun = old })
	wtDrive(t, m, func() tea.Msg { return connectcard.ButtonMsg{ID: "create"} })
	if !strings.Contains(ansi.Strip(m.connect.Box()), "Type a name") {
		t.Error("no name error")
	}
	wtType(t, m, "x")
	wtDrive(t, m, func() tea.Msg { return connectcard.ButtonMsg{ID: "start"} })
	if !strings.Contains(ansi.Strip(m.connect.Box()), "Pick an agent") || calls != 0 {
		t.Errorf("start without an agent: calls %d", calls)
	}
}

// Start point… opens the ref picker over the card; a pick comes back
// into From, Esc keeps it; the typed name stays.
func TestWTFormRefPicker(t *testing.T) {
	m := wtFormModel(t)
	wtType(t, m, "feat/a")
	m.onWTFormButton("refs")
	if m.connectOpen || !m.pickerOpen || m.pickerKind != pickerKindWTRef {
		t.Fatal("no ref picker")
	}
	m.pickerOpen = false
	m.reopenWTForm("v1.4.0")
	if !m.wtFormOpen() || m.connect.InputValue("from") != "v1.4.0" || m.connect.InputValue("name") != "feat/a" {
		t.Fatalf("after pick: from %q name %q", m.connect.InputValue("from"), m.connect.InputValue("name"))
	}
	m.onWTFormButton("refs")
	nm, _ := m.Update(picker.CloseMsg{})
	*m = nm.(Model)
	if !m.wtFormOpen() || m.connect.InputValue("from") != "v1.4.0" {
		t.Fatalf("Esc lost the start point: %q", m.connect.InputValue("from"))
	}
}

func wtGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// The real path: a repository with .termocode/worktrees.json; the new
// worktree is next to it, on its branch, with .env copied and
// node_modules linked.
func TestWTNewRealRepo(t *testing.T) {
	withConfigDir(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(root, "app")
	if err := os.MkdirAll(filepath.Join(repo, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	wtGit(t, repo, "init", "-q", "-b", "main")
	for p, body := range map[string]string{"README.md": "x\n", ".gitignore": ".env\nnode_modules\n",
		".termocode/worktrees.json": `{"include": [".env"], "shared": ["node_modules"]}`} {
		if err := os.WriteFile(filepath.Join(repo, p), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wtGit(t, repo, "add", "-A")
	wtGit(t, repo, "commit", "-qm", "init")
	_ = os.WriteFile(filepath.Join(repo, ".env"), []byte("K=v\n"), 0o600)
	_ = os.MkdirAll(filepath.Join(repo, "node_modules", "x"), 0o755)

	prep := wtNewPrep(repo, "")
	if prep.err != nil || prep.f.from != "main" || prep.f.base != repo+".worktrees" {
		t.Fatalf("prep %+v %v", prep.f, prep.err)
	}
	m := &Model{w: 150, h: 42, wt: newWTState()}
	m.wt.off = true // no list reload from the real disk in the test
	old := wtNewAgents
	wtNewAgents = func() []agents.Agent { return nil }
	t.Cleanup(func() { wtNewAgents = old })
	m.onWTNewPrep(prep)
	if !m.wtFormOpen() {
		t.Fatal("the card did not open")
	}
	m.connect.SetInputValue("name", "feat/real")
	wtDrive(t, m, func() tea.Msg { return connectcard.ButtonMsg{ID: "create"} })
	wt := filepath.Join(root, "app.worktrees", "feat-real")
	if b, err := os.ReadFile(filepath.Join(wt, ".env")); err != nil || string(b) != "K=v\n" {
		t.Fatalf(".env %q %v (card open %v, toast %q)", b, err, m.connectOpen, wtLastToast(m))
	}
	if fi, err := os.Lstat(filepath.Join(wt, "node_modules")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("node_modules not a link: %v %v", fi, err)
	}
	if out, _ := exec.Command("git", "-C", wt, "branch", "--show-current").Output(); strings.TrimSpace(string(out)) != "feat/real" {
		t.Errorf("branch %q", out)
	}
}
