//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// E2E-AGX: agents A1–A4 on the real binary with a fake Claude Code (A2,
// the race, is TestAgentRace). The PATH has git, nvim, sh, the system
// folders and the fake only: no real agent CLI can be found or run, and
// the fake never talks to a network or a model.

// fakeClaudeSession is the session id the fake writes its file under.
const fakeClaudeSession = "3f9a2c1e-0000-4000-8000-0000000000e2"

// claudeSlug is Claude Code's projects/ folder name for dir.
func claudeSlug(dir string) string {
	b := []byte(dir)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			b[i] = '-'
		}
	}
	return string(b)
}

// fakeClaude makes a PATH folder whose "claude" answers `auth status`
// (signed in) and `--help` (has --resume), prints `fake claude resumed
// <id>` for --resume, shows Claude Code's input box and, per line typed:
// logs the line to log; "limit" prints the usage-limit message with a
// reset 10 s away; anything else is written to the session file in the
// fake HOME (as Claude Code does, under projects/<slug of ws>) and
// answered with "Done: <line>".
func fakeClaude(t *testing.T, home, ws, log string) string {
	t.Helper()
	bin := t.TempDir()
	for _, p := range []string{"git", "nvim", "sh"} {
		full, err := exec.LookPath(p)
		if err != nil {
			t.Skip(p + " not found")
		}
		if err := os.Symlink(full, filepath.Join(bin, p)); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(home, ".claude", "projects", claudeSlug(ws))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A temp folder behind a link (macOS /var → /private/var): the same
	// files under the real path's slug too.
	if real, err := filepath.EvalSymlinks(ws); err == nil && real != ws {
		_ = os.Symlink(dir, filepath.Join(home, ".claude", "projects", claudeSlug(real)))
	}
	file := filepath.Join(dir, fakeClaudeSession+".jsonl")
	script := `#!/bin/sh
case "$1" in
auth) echo '{"loggedIn": true}'; exit 0 ;;
--help) echo 'Usage: claude [options]'; echo '  -r, --resume [sessionId]  Resume a conversation'; exit 0 ;;
esac
prev=
for a in "$@"; do
	if [ "$prev" = --resume ]; then echo "fake claude resumed $a"; fi
	prev=$a
done
box() { printf '╭────────────╮\n│ >          │\n╰────────────╯\n  ? for shortcuts\n'; }
echo "fake claude (a banner, like the real CLI's)"
box
n=0
while IFS= read -r line; do
	printf '%s\n' "$line" >>'` + log + `'
	if [ "$line" = limit ]; then
		printf '  ⎿  Claude usage limit reached · try again in 10 seconds\n'
		box
		continue
	fi
	n=$((n + 1))
	ts=$(date -u +%Y-%m-%dT%H:%M:%S.000Z)
	printf '{"type":"user","cwd":"%s","sessionId":"%s","message":{"role":"user","content":"%s"},"uuid":"u%d","timestamp":"%s"}\n' '` + ws + `' '` + fakeClaudeSession + `' "$line" "$n" "$ts" >>'` + file + `'
	printf '{"type":"assistant","cwd":"%s","sessionId":"%s","message":{"id":"m%d","type":"message","role":"assistant","model":"model-a","content":[{"type":"text","text":"Done: %s"}],"usage":{"input_tokens":1200,"output_tokens":80}},"uuid":"a%d","timestamp":"%s"}\n' '` + ws + `' '` + fakeClaudeSession + `' "$n" "$line" "$n" "$ts" >>'` + file + `'
	printf '⏺ Done: %s\n' "$line"
	box
done
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// typedLines is the lines the fake got, in order.
func typedLines(log string) []string {
	b, _ := os.ReadFile(log)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func agentStart(t *testing.T, cfg string) (*Term, string) {
	t.Helper()
	home, ws := t.TempDir(), NewRepo(t)
	if cfg != "" {
		d := filepath.Join(home, ".config", "termocode")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "config.json"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(t.TempDir(), "typed.txt")
	bin := fakeClaude(t, home, ws, log)
	tm := Start(t, StartOpts{Home: home, Workspace: ws, Cols: 150, Rows: 42,
		Env: []string{"PATH=" + bin + string(os.PathListSeparator) + "/usr/bin:/bin"}})
	tm.WaitText("README.md", uiTimeout)
	tm.settle()
	return tm, log
}

// pickClaude runs Agent: Start… and starts Claude Code from the
// INSTALLED group (A1): Codex, not on PATH, is under NOT INSTALLED.
func pickClaude(t *testing.T, tm *Term) {
	t.Helper()
	tm.Palette("Agent: Start…")
	tm.WaitText("Start an agent", uiTimeout)
	tm.WaitText("INSTALLED", uiTimeout)
	tm.WaitText("NOT INSTALLED", uiTimeout)
	tm.WaitFor("claude signed in", uiTimeout, func(ls []string) bool { return lineWith(ls, "Claude Code", "claude · signed in") })
	if !lineWith(tm.Lines(), "Codex", "codex · ") || lineWith(tm.Lines(), "Codex", "signed in") {
		t.Fatalf("Codex is not under NOT INSTALLED:\n%s", tm.Screen())
	}
	tm.step("agent picker")
	tm.Type("Claude Code")
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitGone("Start an agent", uiTimeout)
	tm.WaitText("fake claude (a banner", 20*time.Second)
	tm.WaitText("? for shortcuts", uiTimeout)
}

// Spec (AGX-1, AGX-7…9): Agent: Start… lists the fake Claude Code as
// installed and signed in and starts it in a terminal tab. Agent: Chat
// View first asks to read the session files (Not now first: Enter keeps
// the setting off); Turn on shows the session as a chat that follows the
// growing file. Agent: Sessions… lists the session with its first prompt;
// Enter resumes it in a new tab with claude --resume <id>.
func TestAgentStartChatViewSessions(t *testing.T) {
	tm, log := agentStart(t, "")
	pickClaude(t, tm)
	tm.Type("add the pinned row\r")
	tm.WaitText("Done: add the pinned row", uiTimeout)

	tm.Palette("Agent: Chat View")
	tm.WaitText("session files?", uiTimeout)
	tm.step("transcripts card")
	tm.Send(KeyEnter) // Not now: the focused, safe answer
	tm.WaitGone("session files?", uiTimeout)
	cfg := filepath.Join(tm.home, ".config", "termocode", "config.json")
	if b, _ := os.ReadFile(cfg); strings.Contains(string(b), "agent_transcripts") {
		t.Fatalf("Not now wrote the setting: %s", b)
	}

	tm.Palette("Agent: Chat View")
	tm.WaitText("session files?", uiTimeout)
	tm.Send(keyRight) // Not now → Turn on
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitGone("session files?", uiTimeout)
	tm.WaitText("Done: add the pinned row", uiTimeout)
	if b, _ := os.ReadFile(cfg); !strings.Contains(string(b), `"agent_transcripts": "read"`) {
		t.Errorf("Turn on did not write agent_transcripts: %s", b)
	}
	tm.step("chat view")

	// Typed in the chat view, the text goes to the agent's tab; the view
	// follows the growing file.
	tm.Type("now run the tests")
	tm.Send(KeyEnter)
	tm.WaitText("Done: now run the tests", 20*time.Second)
	if got := strings.Join(typedLines(log), " "); got != "add the pinned row now run the tests" {
		t.Fatalf("the fake got %q", got)
	}

	tm.Palette("Agent: Sessions…")
	tm.WaitText("Agent sessions", uiTimeout)
	tm.WaitFor("the session row", 20*time.Second, func(ls []string) bool {
		return lineWith(ls, "add the pinned row", "Claude Code", "2 prompts")
	})
	tm.step("sessions picker")
	tm.Send(KeyEnter)
	tm.WaitGone("Agent sessions", uiTimeout)
	tm.WaitText("fake claude resumed "+fakeClaudeSession, 20*time.Second)
	tm.Quit()
}

// Spec (AGX-11, AGX-12): with agent_auto_continue on, the fake's usage
// limit message (reset 10 s away) names the tab limit ↻<time> and toasts
// once; a minute after the reset termocode types "continue" into the
// tab, once, and says so. The status bar shows the plan pill.
func TestAgentLimitAutoContinue(t *testing.T) {
	tm, log := agentStart(t, `{"agent_auto_continue": true}`)
	pickClaude(t, tm)
	tm.Type("limit\r")
	tm.WaitText("Claude usage limit reached", uiTimeout)
	tm.WaitText("hit its usage limit", uiTimeout)
	tm.WaitText("limit ↻", uiTimeout)
	tm.WaitText("✦ Claude", uiTimeout)
	tm.step("limit on")

	// Reset in 10 s + the one-minute margin.
	tm.WaitText("Typed continue into Claude Code", 100*time.Second)
	tm.WaitText("Done: continue", uiTimeout)
	tm.WaitStable(2*time.Second, uiTimeout) // a second typing would come now
	if got := typedLines(log); strings.Join(got, ",") != "limit,continue" {
		t.Fatalf("the fake got %q, want limit then one continue", got)
	}
	tm.Quit()
}
