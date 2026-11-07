package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// CMP-81: `termocode task add|list|claim|note|status|show` work on the
// task files directly; the owner comes from --as, then the tab.
func TestRunTask(t *testing.T) {
	dir := t.TempDir()
	oldDir, oldHere := taskDir, taskHere
	taskDir = func(string) string { return dir }
	taskHere = func(string) string { return "/src/app.worktrees/x" }
	t.Cleanup(func() { taskDir, taskHere = oldDir, oldHere })
	env := map[string]string{"TERMOCODE_TAB_ID": "t3", "USER": "amin"}
	now := func() time.Time { return time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC) }
	run := func(args ...string) (int, string, string) {
		var out, errw bytes.Buffer
		code := runTaskIn(args, &out, &errw, func(k string) string { return env[k] }, func() (string, error) { return "/src/app.worktrees/x/web", nil }, now)
		return code, out.String(), errw.String()
	}
	if code, out, _ := run("list"); code != 0 || !strings.Contains(out, "No open tasks") {
		t.Errorf("empty list: %d %q", code, out)
	}
	if code, out, e := run("add", "Fix", "the", "login"); code != 0 || strings.TrimSpace(out) != "1" {
		t.Fatalf("add: %d %q %q", code, out, e)
	}
	run("add", "Dark mode")
	if code, out, _ := run("claim", "#1"); code != 0 || !strings.Contains(out, "#1 is yours") {
		t.Errorf("claim: %d %q", code, out)
	}
	if code, _, e := run("claim", "1", "--as", "codex"); code == 0 || !strings.Contains(e, "taken by agent t3") {
		t.Errorf("claim of a taken task: %d %q", code, e)
	}
	if code, _, _ := run("note", "1", "found", "it"); code != 0 {
		t.Errorf("note: %d", code)
	}
	if code, out, _ := run("show", "1"); code != 0 || !strings.Contains(out, "owner: agent t3") || !strings.Contains(out, "worktree: /src/app.worktrees/x") || !strings.Contains(out, "agent t3: found it") {
		t.Errorf("show: %d\n%s", code, out)
	}
	if code, _, e := run("status", "1", "waiting"); code == 0 || !strings.Contains(e, "todo, doing, blocked, done") {
		t.Errorf("bad status: %d %q", code, e)
	}
	if code, _, _ := run("status", "1", "done"); code != 0 {
		t.Errorf("status: %d", code)
	}
	if code, out, _ := run("list"); code != 0 || strings.Contains(out, "Fix the login") || !strings.Contains(out, "Dark mode") {
		t.Errorf("list hides done: %d\n%s", code, out)
	}
	if code, out, _ := run("list", "--all", "--json"); code != 0 || !strings.Contains(out, `"status":"done"`) || !strings.Contains(out, `"title":"Dark mode"`) {
		t.Errorf("list --all --json: %d %s", code, out)
	}
	if code, _, _ := run("show", "x"); code != 2 {
		t.Errorf("show without an id: %d", code)
	}
	if code, out, _ := run("--help"); code != 0 || !strings.Contains(out, "termocode task claim") {
		t.Errorf("help: %d %s", code, out)
	}
}
