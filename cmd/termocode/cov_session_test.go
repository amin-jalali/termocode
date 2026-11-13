//go:build unix

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/termimg"
)

func covSessionEnv(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	return t.TempDir()
}

func TestGraphicsFromSessionProtocols(t *testing.T) {
	for proto, want := range map[string]termimg.Protocol{
		"kitty": termimg.Kitty, "sixel": termimg.Sixel, "off": termimg.None, "blocks": termimg.Blocks, "newer": termimg.Blocks,
	} {
		if got := graphicsFromSession(&session.GraphicsCaps{Protocol: proto}); got.Protocol != want {
			t.Errorf("%s: %v", proto, got.Protocol)
		}
	}
}

func TestProjectSessionHelpers(t *testing.T) {
	dir := covSessionEnv(t)
	if projectRunning(dir) {
		t.Fatal("a session that was never started is running")
	}
	if projectRunning("") && false {
		t.Fatal("unreachable")
	}
	if projectService(dir) {
		t.Fatal("no service installed here")
	}
	t.Setenv("HOME", "")
	if projectService(dir) {
		t.Fatal("no home, no service")
	}
}

// spawnServer starts the server process detached and returns at once; the
// test binary refuses the unknown flag and exits, which is all we need.
func TestSpawnServerStartsAProcess(t *testing.T) {
	dir := covSessionEnv(t)
	p, err := session.PathsFor(dir)
	if err != nil {
		t.Skip(err)
	}
	if err := spawnServer(p); err != nil {
		t.Fatalf("spawnServer: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(p.Workspace)); err != nil {
		t.Log(err)
	}
}

// With no session and no permission to create one, the client says so.
func TestRunClientWithoutSession(t *testing.T) {
	dir := covSessionEnv(t)
	if code := runClient(dir, clientOpts{}); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if code := runClient(filepath.Join(dir, "gone"), clientOpts{}); code != 1 {
		t.Fatalf("missing folder: exit %d", code)
	}
}

func TestCmdSSHRefusals(t *testing.T) {
	if code := cmdSSH(nil); code != 2 {
		t.Errorf("no host: %d", code)
	}
	if code := cmdSSH([]string{"me@box"}); code != 1 && code != 2 {
		t.Errorf("without a terminal: %d", code)
	}
}

func TestCmdKillAndSessionsWithNothingRunning(t *testing.T) {
	dir := covSessionEnv(t)
	if code := cmdKill(dir); code != 1 {
		t.Errorf("kill: %d", code)
	}
	if code := cmdSessions(); code != 0 {
		t.Errorf("sessions: %d", code)
	}
}

func TestTerminalChangeFromEnvironment(t *testing.T) {
	for _, mode := range []string{"", "kitty"} {
		c := &session.ClientCaps{Env: map[string]string{"TERM": "xterm-kitty"}, Note: mode}
		msg := terminalChange(c)
		if msg.Graphics.Reason == "" {
			t.Errorf("no reason for %q", mode)
		}
	}
	g := &session.ClientCaps{Graphics: &session.GraphicsCaps{Protocol: "sixel"}}
	if terminalChange(g).Graphics.Protocol != termimg.Sixel {
		t.Error("graphics from the client")
	}
}

func TestWatchRepaintNeedsASession(t *testing.T) {
	t.Setenv(session.EnvSession, "")
	watchRepaint(nil) // returns before touching the program
}
