package testkit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// K4 rows of TestAIToolsTable: git_log, git_blame, git_unstage,
// git_fetch, git_hunk and git_stash through the real app with a git
// workspace (Options.Git). A separate block so other Group K steps add
// their rows without merge conflicts.
func init() {
	aiToolCases = append(aiToolCases, aiGitToolCases...)
}

// gitOut runs git in the workspace and returns its trimmed output.
func gitOut(d *Driver, args ...string) string {
	d.T.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = d.Dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		d.T.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// writeWS writes a workspace file on disk (not through the editor).
func writeWS(d *Driver, rel, body string) {
	d.T.Helper()
	if err := os.WriteFile(filepath.Join(d.Dir, rel), []byte(body), 0o644); err != nil {
		d.T.Fatal(err)
	}
}

// showSCM opens Source Control and waits until rel is listed in section.
func showSCM(d *Driver, section, rel string) {
	d.T.Helper()
	d.Action("view:git")
	if !until(d, func() bool { return inSection(d, section, rel) }) {
		d.T.Fatalf("%s not in %s:%s", rel, section, screen(d))
	}
}

// reloadA makes the open a.txt tab show what is on disk.
func reloadA(d *Driver, want string) {
	d.T.Helper()
	lua(d, `vim.cmd('silent! checktime'); return ''`)
	if !d.WaitFor(func() bool { return strings.Contains(bufLines(d), want) }, 3*time.Second) {
		d.T.Fatalf("a.txt buffer %q, want %q", bufLines(d), want)
	}
}

var aiGitToolCases = []aiToolCase{
	{
		name: "git_log lists commits without asking",
		call: toolCall{"git_log", map[string]any{"n": 5}},
		git:  true,
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "Merge branch 'feature/x'") || !strings.Contains(r[0], "Test") {
				t.Errorf("results %q", r)
			}
			if n := strings.Count(r[0], "\n") + 1; n > 5 {
				t.Errorf("%d lines for n=5: %q", n, r[0])
			}
		},
	},
	{
		name: "git_log of a path lists only its commits",
		call: toolCall{"git_log", map[string]any{"path": "CHANGELOG.md"}},
		git:  true,
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "docs: changelog") || strings.Contains(r[0], "Merge branch") {
				t.Errorf("results %q", r)
			}
		},
	},
	{
		name: "git_blame names the author of each line without asking",
		call: toolCall{"git_blame", map[string]any{"path": "a.txt"}},
		git:  true,
		setup: func(d *Driver) {
			writeWS(d, "a.txt", "alpha\nBRAVO\ncharlie\n")
		},
		check: func(t *testing.T, d *Driver, r []string) {
			sha := gitOut(d, "log", "-1", "--format=%h", "--", "a.txt")
			if len(r) != 1 || !strings.Contains(r[0], "1 "+sha[:7]+" Test ") ||
				!strings.Contains(r[0], "2 uncommitted") || !strings.Contains(r[0], "3 "+sha[:7]+" Test ") {
				t.Errorf("results %q (sha %s)", r, sha)
			}
		},
	},
	{
		name:    "git_hunk stage asks, then the file shows as staged in Source Control",
		call:    toolCall{"git_hunk", map[string]any{"op": "stage", "path": "b.txt", "line": 2}},
		git:     true,
		confirm: true,
		setup: func(d *Driver) {
			writeWS(d, "b.txt", "one\nTWO\nthree\n")
			showSCM(d, "CHANGES", "b.txt")
		},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "staged") {
				t.Errorf("results %q", r)
			}
			if !until(d, func() bool { return inSection(d, "STAGED", "b.txt") && !inSection(d, "CHANGES", "b.txt") }) {
				t.Errorf("b.txt not staged on screen:\n%s", screen(d))
			}
			if got := gitOut(d, "diff", "--cached", "--name-only"); got != "b.txt" {
				t.Errorf("index: %q", got)
			}
		},
	},
	{
		name:    "git_hunk discard asks, then the open tab shows the old text",
		call:    toolCall{"git_hunk", map[string]any{"op": "discard", "path": "a.txt", "line": 2}},
		git:     true,
		confirm: true,
		setup: func(d *Driver) {
			writeWS(d, "a.txt", "alpha\nBRAVO\ncharlie\n")
			reloadA(d, "BRAVO")
		},
		check: func(t *testing.T, d *Driver, r []string) {
			if !eventually(d, func() bool { return bufLines(d) == "alpha\nbravo\ncharlie" }) {
				t.Errorf("buffer %q; results %q", bufLines(d), r)
			}
			if diskFile(d, "a.txt") != "alpha\nbravo\ncharlie\n" {
				t.Errorf("disk %q", diskFile(d, "a.txt"))
			}
		},
	},
	{
		name: "git_hunk discard on a file with unsaved edits is refused without a dialog",
		call: toolCall{"git_hunk", map[string]any{"op": "discard", "path": "a.txt", "line": 2}},
		git:  true,
		setup: func(d *Driver) {
			writeWS(d, "a.txt", "alpha\nBRAVO\ncharlie\n")
			reloadA(d, "BRAVO")
			lua(d, `vim.api.nvim_buf_set_lines(0, 0, 1, false, { "mine" }); return ''`)
		},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "unsaved") {
				t.Errorf("results %q", r)
			}
			if diskFile(d, "a.txt") != "alpha\nBRAVO\ncharlie\n" || !strings.HasPrefix(bufLines(d), "mine\nBRAVO") {
				t.Errorf("disk %q buffer %q", diskFile(d, "a.txt"), bufLines(d))
			}
		},
	},
	{
		name:    "git_unstage asks, then the file is back under CHANGES",
		call:    toolCall{"git_unstage", map[string]any{"paths": []any{"b.txt"}}},
		git:     true,
		confirm: true,
		setup: func(d *Driver) {
			writeWS(d, "b.txt", "one\nTWO\nthree\n")
			gitOut(d, "add", "b.txt")
			showSCM(d, "STAGED", "b.txt")
		},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "unstaged b.txt") {
				t.Errorf("results %q", r)
			}
			if !until(d, func() bool { return inSection(d, "CHANGES", "b.txt") && !inSection(d, "STAGED", "b.txt") }) {
				t.Errorf("b.txt not unstaged on screen:\n%s", screen(d))
			}
		},
	},
	{
		name: "git_stash list runs without asking",
		call: toolCall{"git_stash", map[string]any{"op": "list"}},
		git:  true,
		setup: func(d *Driver) {
			writeWS(d, "b.txt", "one\nTWO\nthree\n")
			gitOut(d, "stash", "push", "-m", "keep-this")
		},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "stash@{0}") || !strings.Contains(r[0], "keep-this") {
				t.Errorf("results %q", r)
			}
		},
	},
	{
		name:    "git_fetch asks, then fetches",
		call:    toolCall{"git_fetch", nil},
		git:     true,
		confirm: true,
		setup: func(d *Driver) {
			bare := filepath.Join(d.T.TempDir(), "origin.git")
			gitOut(d, "init", "-q", "--bare", bare)
			gitOut(d, "remote", "add", "origin", bare)
			gitOut(d, "push", "-q", "origin", "feature/x")
		},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "fetched") {
				t.Errorf("results %q", r)
			}
			if gitOut(d, "branch", "-r") != "origin/feature/x" {
				t.Errorf("remote branches: %q", gitOut(d, "branch", "-r"))
			}
		},
	},
}

// Spec: git_hunk discard is data loss, so its dialog has no "Always
// allow" button even when the git class is auto-approved.
func TestAIGitHunkDiscardHasNoAlwaysAllow(t *testing.T) {
	_, srv := fakeToolModel(t, toolCall{"git_hunk", map[string]any{"op": "discard", "path": "a.txt", "line": 2}})
	d := aiDriver(t, srv.URL, nil, true, "git")
	open(d, "a.txt")
	writeWS(d, "a.txt", "alpha\nBRAVO\ncharlie\n")
	reloadA(d, "BRAVO")
	askAI(d, "drop it")
	if !d.WaitForText("AI wants to", 5*time.Second) {
		t.Fatalf("no confirm dialog (git auto-approve must not cover discard):\n%s", screen(d))
	}
	if strings.Contains(screen(d), "Always allow") {
		t.Errorf("discard offers Always allow:\n%s", screen(d))
	}
	clickAIAllow(d)
	if !d.WaitForText("fake-done", 5*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
	if !eventually(d, func() bool { return bufLines(d) == "alpha\nbravo\ncharlie" }) {
		t.Errorf("buffer %q", bufLines(d))
	}
}

// Spec: git_stash push then pop (git auto-approved) round-trips a change:
// push makes the file clean and the open tab shows the committed text;
// pop brings the change back on disk and in the tab.
func TestAIGitStashPushPopRoundTrip(t *testing.T) {
	s, srv := fakeToolModel(t,
		toolCall{"git_stash", map[string]any{"op": "push", "message": "wip-a"}},
		toolCall{"git_stash", map[string]any{"op": "pop"}})
	d := aiDriver(t, srv.URL, nil, true, "git")
	open(d, "a.txt")
	writeWS(d, "a.txt", "alpha\nBRAVO\ncharlie\n")
	reloadA(d, "BRAVO")
	reached, release := s.holdAt(1)
	askAI(d, "stash and pop")
	waitReached(d, reached, "stash push")
	if !eventually(d, func() bool { return bufLines(d) == "alpha\nbravo\ncharlie" }) {
		t.Errorf("after push, buffer %q", bufLines(d))
	}
	if got := gitOut(d, "stash", "list"); !strings.Contains(got, "wip-a") {
		t.Errorf("stash list %q", got)
	}
	release()
	if !d.WaitForText("fake-done", 5*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "AI wants to") {
		t.Errorf("asked although git is auto-approved:\n%s", screen(d))
	}
	if diskFile(d, "a.txt") != "alpha\nBRAVO\ncharlie\n" {
		t.Errorf("after pop, disk %q", diskFile(d, "a.txt"))
	}
	if !eventually(d, func() bool { return bufLines(d) == "alpha\nBRAVO\ncharlie" }) {
		t.Errorf("after pop, buffer %q", bufLines(d))
	}
	if got := gitOut(d, "stash", "list"); got != "" {
		t.Errorf("stash not dropped by pop: %q", got)
	}
	r := s.toolResults()
	if len(r) != 2 || !strings.Contains(r[0], "stashed") || !strings.Contains(r[1], "popped") {
		t.Errorf("results %q", r)
	}
}
