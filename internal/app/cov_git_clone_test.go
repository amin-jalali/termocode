package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
)

// covCloneRun feeds the clone's progress messages back until it ends and
// returns the done message (already handled).
func covCloneRun(t *testing.T, m *Model, cmd tea.Cmd) (cloneDoneMsg, int) {
	t.Helper()
	progress := 0
	for {
		msg := covMatch(t, cmd, func(msg tea.Msg) bool {
			switch msg.(type) {
			case cloneProgressMsg, cloneDoneMsg:
				return true
			}
			return false
		})
		switch msg := msg.(type) {
		case cloneProgressMsg:
			progress++
			cmd = m.handleCloneProgress(msg)
		case cloneDoneMsg:
			m.handleCloneDone(msg)
			return msg, progress
		}
	}
}

// Clone: a URL prompt (a bad URL is refused), a folder prompt (a folder
// that is not empty is refused), a progress toast, then the new folder
// opens (in tests: the headless note). A second clone while one runs is
// refused; Esc on a prompt cancels.
func TestCovGitCloneFlow(t *testing.T) {
	m, dir := covGitRepo(t)
	src := "file://" + dir

	m.dispatchPaletteAction("git-clone")
	if !m.promptOpen || m.promptKind != promptKindCloneURL {
		t.Fatal("no URL prompt")
	}
	m.promptOpen = false
	m.handlePromptSubmit("  /  ")
	if !toastHas(m, "does not look like a repository URL") {
		t.Error("bad URL accepted")
	}
	m.handlePromptSubmit(src)
	if !m.promptOpen || m.promptKind != promptKindCloneDest {
		t.Fatal("no folder prompt")
	}
	base := filepath.Base(dir)
	if want := filepath.Join(filepath.Dir(dir), base); !strings.Contains(m.prompt.View(), want) {
		t.Errorf("folder not prefilled next to the repo (%s): %q", want, m.prompt.View())
	}
	m.promptOpen = false
	m.handlePromptSubmit(dir) // the repo itself: not empty
	if !toastHas(m, "Cannot clone there") {
		t.Error("non-empty folder accepted")
	}

	cmd := m.handlePromptSubmit("copy") // relative: below the workspace
	if !m.clone.running {
		t.Fatal("clone did not start")
	}
	if m.openCloneURLPrompt(); !toastHas(m, "A clone is already running") || m.promptOpen {
		t.Error("second clone not refused")
	}
	done, progress := covCloneRun(t, m, cmd)
	if done.err != nil {
		t.Fatalf("clone: %v", done.err)
	}
	if progress == 0 {
		t.Error("no progress shown")
	}
	if _, err := os.Stat(filepath.Join(dir, "copy", "a.txt")); err != nil {
		t.Errorf("clone has no a.txt: %v", err)
	}
	if m.clone.running || m.clone.url != "" || !toastHas(m, "Exec failed") {
		t.Error("clone state kept or the new folder was not opened")
	}

	// A source that does not exist.
	m.clone = cloneState{url: "file://" + filepath.Join(dir, "missing"), dest: filepath.Join(t.TempDir(), "x")}
	if d, _ := covCloneRun(t, m, m.startClone("", "")); d.err == nil || !toastHas(m, "Clone failed") {
		t.Errorf("missing source: %v", d.err)
	}

	for _, k := range []promptKindEnum{promptKindCloneURL, promptKindCloneDest, promptKindCloneToken} {
		m.toast = m.toast.Dismiss("x")
		m.promptKind = k
		m.handleClonePromptClose()
		if !toastHas(m, "Clone cancelled") {
			t.Errorf("Esc on prompt %d: no cancel note", k)
		}
	}
	m.promptKind = promptKindContainerRunName
	m.pendingRunName = "web"
	m.handleClonePromptClose()
	if m.pendingRunName != "" || !toastHas(m, "Run cancelled") {
		t.Error("Esc on the run prompt kept the name")
	}
	m.promptKind = promptKindCommit
	if m.handleClonePromptClose() != nil {
		t.Error("Esc on another prompt was taken")
	}
}

// An HTTPS host that wants a password: a masked token prompt, one retry
// with the token, then a plain authentication error.
func TestCovGitCloneAuth(t *testing.T) {
	covGitEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	for _, k := range []string{"NO_PROXY", "no_proxy"} {
		t.Setenv(k, "127.0.0.1,localhost")
	}
	m := covModelAt(t, t.TempDir())
	m.clone = cloneState{url: srv.URL + "/o/r.git", dest: filepath.Join(t.TempDir(), "r")}
	d, _ := covCloneRun(t, m, m.startClone("", ""))
	if !strings.Contains(d.err.Error(), git.ErrCloneAuth.Error()) {
		t.Fatalf("not an auth error: %v", d.err)
	}
	if !m.promptOpen || m.promptKind != promptKindCloneToken || !strings.Contains(m.prompt.View(), "Sign in to 127.0.0.1") {
		t.Fatalf("no token prompt: %q", m.prompt.View())
	}
	m.promptOpen = false
	cmd := m.handlePromptSubmit("ann:secret-token")
	if d, _ := covCloneRun(t, m, cmd); !d.withToken || !toastHas(m, "Clone failed: authentication") {
		t.Fatalf("retry: %+v", d)
	}
	if m.promptOpen {
		t.Error("asked for the token twice")
	}

	// A URL with no host still gets a prompt.
	m.clone.url = "file-without-host"
	m.openCloneTokenPrompt()
	if !strings.Contains(m.prompt.View(), "Sign in to this host") {
		t.Errorf("prompt %q", m.prompt.View())
	}
}

// The dashboard's clone and workspace cards; a card out of range does
// nothing. Progress without a percent shows the phase only.
func TestCovGitWelcomeCards(t *testing.T) {
	m := Model{w: 160, h: 40}
	for i, a := range welcomeQuickActions() {
		switch a.id {
		case welcomeCardClone:
			m2, _ := m.runWelcomeCard(i)
			if !m2.promptOpen || m2.promptKind != promptKindCloneURL {
				t.Error("clone card opened no prompt")
			}
		case welcomeCardWorkspace:
			m2, _ := m.runWelcomeCard(i)
			if !toastHas(&m2, "No recent workspaces yet") && !m2.pickerOpen {
				t.Error("workspace card did nothing")
			}
		}
	}
	if _, cmd := m.runWelcomeCard(-1); cmd != nil {
		t.Error("card -1 did something")
	}
	if got := cloneProgressText(git.CloneProgress{Phase: "Counting", Percent: -1}); got != "Counting…" {
		t.Errorf("progress %q", got)
	}
}
