package worktrees

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CMP-11: the handoff note says what a new agent needs: where, the
// branch and its base, the commits, the changed and uncommitted files,
// the agent notes and the user's own line; long lists are cut; every
// line is one line of plain text and the redactor runs over the note.
func TestHandoffText(t *testing.T) {
	at := time.Date(2026, 11, 7, 10, 30, 0, 0, time.UTC)
	h := Handoff{
		Title: "toast", Branch: "fix/toast-race", Base: "main", Path: "/src/app.worktrees/toast",
		Commits:    []string{"fix the race", "add a test"},
		Changed:    []string{"internal/toast/toast.go", "internal/toast/toast_test.go"},
		Dirty:      []string{" M internal/toast/toast.go"},
		Notes:      []Note{{Text: "found the race in Push", At: at}},
		Checkpoint: "blocked",
		Link:       "PR #12 Fix the toast race",
		Extra:      "keep the old API\nsecret sk-" + "test123",
	}
	redact := func(s string) string { return strings.ReplaceAll(s, "sk-"+"test123", "[redacted]") }
	got := h.Text(redact)
	for _, want := range []string{
		"# Handoff: toast",
		"Worktree: /src/app.worktrees/toast",
		"Branch: fix/toast-race (from main)",
		"Status: blocked",
		"Link: PR #12 Fix the toast race",
		"## From the user",
		"keep the old API secret [redacted]",
		"## Commits since main (2)",
		"- fix the race",
		"## Changed files (2)",
		"- internal/toast/toast_test.go",
		"## Not committed (1)",
		"- M internal/toast/toast.go",
		"## Agent notes",
		"- 2026-11-07 10:30 found the race in Push",
		"## Next step",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "sk-"+"test123") {
		t.Errorf("a secret is in the note:\n%s", got)
	}

	// Long lists are cut; empty parts are left out.
	var many []string
	for i := 0; i < HandoffMaxFiles+5; i++ {
		many = append(many, "f"+strings.Repeat("x", i%3)+".go")
	}
	got = Handoff{Title: "x", Path: "/x", Changed: many}.Text(nil)
	if !strings.Contains(got, "…and 5 more") {
		t.Errorf("long list not cut:\n%s", got)
	}
	for _, gone := range []string{"## Commits", "## Not committed", "## Agent notes", "## From the user", "Status:"} {
		if strings.Contains(got, gone) {
			t.Errorf("empty part %q shown:\n%s", gone, got)
		}
	}
	// A detached worktree has no branch line base.
	if got := (Handoff{Title: "d", Path: "/d"}).Text(nil); !strings.Contains(got, "Branch: (detached)") {
		t.Errorf("detached:\n%s", got)
	}
}

// The prompt typed into the new agent is one line that names the note;
// the note file lives in termocode's state folder, never in the project.
func TestHandoffPromptAndFile(t *testing.T) {
	p := HandoffPrompt("/repo/.git/termocode/handoff/toast-20261107-103000.md", "toast\nrace")
	if strings.ContainsAny(p, "\n\r") || !strings.Contains(p, "/repo/.git/termocode/handoff/toast-20261107-103000.md") || !strings.Contains(p, `"toast race"`) {
		t.Errorf("prompt %q", p)
	}
	at := time.Date(2026, 11, 7, 10, 30, 0, 0, time.UTC)
	f := HandoffFile("/repo/.git/termocode", "/src/app.worktrees/toast", at)
	if want := filepath.Join("/repo/.git/termocode", "handoff", "toast-20261107-103000.md"); f != want {
		t.Errorf("file %q, want %q", f, want)
	}
}
