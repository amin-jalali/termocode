package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// gitMultiRepoFolder is a folder that is no repository with two repos
// inside: alpha (clean) and beta (one changed file). The active repo is
// reset when the test ends.
func gitMultiRepoFolder(t *testing.T) (m *Model, root, alpha, beta string) {
	t.Helper()
	covGitEnv(t)
	root = t.TempDir()
	alpha, beta = filepath.Join(root, "alpha"), filepath.Join(root, "beta")
	for _, d := range []string{alpha, beta} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		covGit(t, d, "init", "-q", "-b", "main")
		covGit(t, d, "config", "commit.gpgsign", "false")
		covWrite(t, d, "a.txt", "one\n")
		covCommit(t, d, "first")
	}
	covWrite(t, beta, "a.txt", "changed\n")
	t.Cleanup(func() { setGitActiveRepo("") })
	setGitActiveRepo("")
	return covModelAt(t, root), root, alpha, beta
}

func gitRepoRows(m *Model) []gitPanelRow {
	var out []gitPanelRow
	for _, r := range m.gitPanelRows() {
		if r.kind == gitRowRepo {
			out = append(out, r)
		}
	}
	return out
}

// GITX-31 (M6, M7, M15, M16): a workspace folder with several repos.
// The scan finds them; when the folder is no repo itself the first one
// becomes the active repo, so Source Control shows it at once. The
// REPOSITORIES section lists all of them (branch and changes); Enter on
// one makes it the active repo and every git action follows.
func TestGitMultiRepoPanel(t *testing.T) {
	m, _, alpha, beta := gitMultiRepoFolder(t)
	if m.gitIsRepo {
		t.Fatal("the folder is no repo, but Source Control shows one")
	}
	msg := covMsg[gitReposMsg](t, gitScanReposCmd(m.workspace(), false))
	if len(msg.repos) != 2 || msg.repos[0].dir != alpha || msg.repos[1].dir != beta {
		t.Fatalf("scan: %+v", msg.repos)
	}
	if msg.repos[1].changes != 1 || msg.repos[1].branch != "main" {
		t.Errorf("beta info: %+v", msg.repos[1])
	}
	m.onGitRepos(msg)
	if got, _ := m.gitRootDir(); got != alpha {
		t.Fatalf("active repo %q, want alpha", got)
	}
	covRefresh(m)
	if !m.gitIsRepo || m.gitBranch.Name != "main" || len(m.gitFiles) != 0 {
		t.Fatalf("alpha snapshot: repo %v branch %q files %+v", m.gitIsRepo, m.gitBranch.Name, m.gitFiles)
	}

	rows := gitRepoRows(m)
	if len(rows) != 2 {
		t.Fatalf("repo rows %+v", rows)
	}
	view := ansi.Strip(m.renderGitSidebar(60, 40))
	if !strings.Contains(view, "REPOSITORIES") || !strings.Contains(view, "alpha") || !strings.Contains(view, "beta") {
		t.Fatalf("no REPOSITORIES section:\n%s", view)
	}

	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowRepo && r.repo == 1 })
	cmd, _ := covKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if got, _ := m.gitRootDir(); got != beta {
		t.Fatalf("Enter on beta: active %q", got)
	}
	if cmd == nil {
		t.Error("switching does not read the new repo")
	}
	covRefresh(m)
	if len(m.gitFiles) != 1 || m.gitFiles[0].Path != "a.txt" {
		t.Fatalf("beta files %+v", m.gitFiles)
	}
	// Paths of the panel are in the active repo.
	if got := gitFileAbsPath("a.txt"); got != filepath.Join(beta, "a.txt") {
		t.Errorf("file path %q", got)
	}
}

// GITX-31 (M7): Git: Switch Repository… scans, then lists the repos;
// picking one makes it active. An active repo outside the workspace is
// never used.
func TestGitMultiRepoPicker(t *testing.T) {
	m, root, alpha, beta := gitMultiRepoFolder(t)
	if _, ok := aiActionPolicies["git-repo-pick"]; !ok {
		t.Error("git-repo-pick has no AI policy")
	}
	msg := covMsg[gitReposMsg](t, m.dispatchPaletteAction("git-repo-pick"))
	m.onGitRepos(msg)
	if !m.pickerOpen || m.pickerKind != pickerKindGitx {
		t.Fatalf("picker open %v kind %v", m.pickerOpen, m.pickerKind)
	}
	var ids, titles []string
	for _, it := range m.gitx.items {
		ids, titles = append(ids, it.ID), append(titles, it.Title)
	}
	if !slices.Equal(ids, []string{alpha, beta}) || !slices.Contains(titles, "beta") {
		t.Fatalf("picker rows %q %q", ids, titles)
	}
	m.handleGitxPickerSelect(beta)
	if got, _ := m.gitRootDir(); got != beta {
		t.Fatalf("picked beta: active %q", got)
	}

	// An active repo outside the workspace falls back to the workspace.
	setGitActiveRepo(t.TempDir())
	if got, _ := m.gitRootDir(); got != root {
		t.Errorf("outside repo used: %q", got)
	}
}

// gitSubmoduleRepo is a repo with the submodule libs/lib (a second
// repo, added from a local path).
func gitSubmoduleRepo(t *testing.T) (m *Model, dir, sub string) {
	t.Helper()
	covGitEnv(t)
	lib := t.TempDir()
	covGit(t, lib, "init", "-q", "-b", "main")
	covWrite(t, lib, "lib.txt", "1\n")
	covCommit(t, lib, "lib one")
	m, dir = covGitRepo(t)
	t.Cleanup(func() { setGitActiveRepo("") })
	covGit(t, dir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "libs/lib")
	covCommit(t, dir, "add lib")
	covRefresh(m)
	return m, dir, filepath.Join(dir, "libs", "lib")
}

func gitSubRows(m *Model) []gitPanelRow {
	var out []gitPanelRow
	for _, r := range m.gitPanelRows() {
		if r.kind == gitRowSubmodule {
			out = append(out, r)
		}
	}
	return out
}

// GITX-31 (M8): the SUBMODULES section lists the submodules and their
// state. Enter on one offers Open (as the active repo) and Update;
// Update asks on a Cancel-first card, then checks out the recorded
// commit.
func TestGitSubmodulesPanel(t *testing.T) {
	m, dir, sub := gitSubmoduleRepo(t)
	if len(gitSubRows(m)) != 1 || len(m.gitSubmodules) != 1 || m.gitSubmodules[0].Path != "libs/lib" {
		t.Fatalf("submodules %+v", m.gitSubmodules)
	}
	view := ansi.Strip(m.renderGitSidebar(60, 40))
	if !strings.Contains(view, "SUBMODULES") || !strings.Contains(view, "libs/lib") {
		t.Fatalf("no SUBMODULES section:\n%s", view)
	}

	// Move the submodule's HEAD: it shows as changed.
	covWrite(t, sub, "lib.txt", "2\n")
	covCommit(t, sub, "lib two")
	covRefresh(m)
	view = ansi.Strip(m.renderGitSidebar(60, 40))
	if !strings.Contains(view, "other commit") {
		t.Fatalf("changed state not shown:\n%s", view)
	}

	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowSubmodule })
	covKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.pickerOpen || m.pickerKind != pickerKindGitx {
		t.Fatal("Enter on a submodule: no actions")
	}
	var ids []string
	for _, it := range m.gitx.items {
		ids = append(ids, it.ID)
	}
	if !slices.Equal(ids, []string{"open", "update"}) {
		t.Fatalf("actions %q", ids)
	}
	m.pickerOpen = false
	m.handleGitxPickerSelect("update")
	if !m.confirmOpen || m.confirmKind != confirmKindGitx || m.confirm.SafeID() != "cancel" {
		t.Fatalf("update: card open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	done := covMsg[gitxDoneMsg](t, func() tea.Cmd { c, _ := m.handleGitxConfirm("update"); return c }())
	done.apply(m)
	covRefresh(m)
	if len(m.gitSubmodules) != 1 || m.gitSubmodules[0].State != ' ' {
		t.Fatalf("after update: %+v", m.gitSubmodules)
	}

	// Open makes the submodule the active repo.
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowSubmodule })
	covKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.pickerOpen = false
	m.handleGitxPickerSelect("open")
	if got, _ := m.gitRootDir(); got != sub {
		t.Fatalf("open: active %q, want %q", got, sub)
	}
	setGitActiveRepo(dir)
}

// GITX-31 (M8): Git: Update Submodules… asks on a Cancel-first card;
// Cancel changes nothing, Update all inits and updates every submodule.
func TestGitSubmoduleUpdateAll(t *testing.T) {
	m, dir, _ := gitSubmoduleRepo(t)
	for _, id := range []string{"git-submodule-update"} {
		if _, ok := aiActionPolicies[id]; !ok {
			t.Errorf("%s has no AI policy", id)
		}
	}
	covGit(t, dir, "submodule", "deinit", "-q", "-f", "libs/lib")
	covRefresh(m)
	if len(m.gitSubmodules) != 1 || m.gitSubmodules[0].State != '-' {
		t.Fatalf("after deinit: %+v", m.gitSubmodules)
	}
	view := ansi.Strip(m.renderGitSidebar(60, 40))
	if !strings.Contains(view, "not initialized") {
		t.Fatalf("not-initialized state not shown:\n%s", view)
	}

	m.dispatchPaletteAction("git-submodule-update")
	if !m.confirmOpen || m.confirm.SafeID() != "cancel" {
		t.Fatal("no Cancel-first card")
	}
	if c, _ := m.handleGitxConfirm("cancel"); c != nil {
		if _, ok := c().(gitxDoneMsg); ok {
			t.Fatal("Cancel ran the update")
		}
	}
	m.dispatchPaletteAction("git-submodule-update")
	done := covMsg[gitxDoneMsg](t, func() tea.Cmd { c, _ := m.handleGitxConfirm("update"); return c }())
	done.apply(m)
	covRefresh(m)
	if len(m.gitSubmodules) != 1 || m.gitSubmodules[0].State != ' ' {
		t.Fatalf("after update all: %+v", m.gitSubmodules)
	}

	// No submodules: the palette says so and opens no card.
	m2, _ := covGitRepo(t)
	m2.dispatchPaletteAction("git-submodule-update")
	if m2.confirmOpen {
		t.Error("card opened with no submodules")
	}
}
