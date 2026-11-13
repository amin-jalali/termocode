//go:build unix

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentctl"
	"github.com/amin-jalali/termocode/internal/session"
)

// The real serve hooks work on a folder's session paths; a bad folder is
// an error, and the web hook runs `termocode web` here.
func TestDefaultServeDepsWithoutAnyHome(t *testing.T) {
	for _, k := range []string{"HOME", "XDG_RUNTIME_DIR", "XDG_STATE_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(k, "")
	}
	d := defaultServeDeps()
	if _, err := session.PathsFor("."); err == nil {
		t.Skip("session paths do not need a home here")
	}
	if _, err := d.start("."); err == nil {
		t.Error("start without a home")
	}
	if err := d.waitHub(".", time.Now()); err == nil {
		t.Error("waitHub without a home")
	}
}

func TestDefaultServeWebHook(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if code := defaultServeDeps().web([]string{"--help"}); code != 0 && code != 2 {
		t.Errorf("web hook: %d", code)
	}
}

func TestServeRelativeFolder(t *testing.T) {
	cwd := t.TempDir()
	if err := os.Mkdir(filepath.Join(cwd, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	var got string
	var out, errw bytes.Buffer
	d := serveDeps{
		start:   func(dir string) (bool, error) { got = dir; return true, nil },
		waitHub: func(string, time.Time) error { return nil },
		web:     func([]string) int { return 0 },
	}
	if code := runServeIn([]string{"proj"}, &out, &errw, cwd, d); code != 0 || got != filepath.Join(cwd, "proj") {
		t.Fatalf("%d %q %s", code, got, errw.String())
	}
}

func TestWebCommandFolderArgs(t *testing.T) {
	var out, errw bytes.Buffer
	if code := runWebIn([]string{"status", "--dir=" + t.TempDir()}, &out, &errw, t.TempDir()); code == 0 && errw.Len() > 0 {
		t.Logf("status: %s", errw.String())
	}
	errw.Reset()
	if code := runWebIn([]string{"status", "--dir", filepath.Join(t.TempDir(), "missing")}, &out, &errw, t.TempDir()); code != webExitUsage || !strings.Contains(errw.String(), "is not a folder") {
		t.Errorf("missing folder: %d %q", code, errw.String())
	}
}

// The tray list asks every session's agent socket; a session that does not
// answer is marked unreachable.
func TestBarSessionsListsWhatIsRunning(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	old := agentCall
	t.Cleanup(func() { agentCall = old })
	agentCall = func(string, agentctl.Request, time.Duration) (agentctl.Reply, error) {
		return agentctl.Reply{OK: true, Tabs: []agentctl.Tab{{ID: "t1"}}}, nil
	}
	list, err := barSessions()
	if err != nil {
		t.Skip(err)
	}
	if len(list) != 0 {
		t.Fatalf("sessions in a fresh state: %v", list)
	}
	_ = errors.New
	_ = session.Dir
}

// The tray list marks a session whose agent socket does not answer.
func TestBarSessionsWithARunningSession(t *testing.T) {
	dir := covSessionEnv(t)
	p, err := session.PathsFor(dir)
	if err != nil {
		t.Skip(err)
	}
	go session.Serve(dir, func() *exec.Cmd { return exec.Command("sh", "-c", "exec cat") })
	for i := 0; i < 500; i++ {
		if c, err := session.Dial(p); err == nil {
			c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Cleanup(func() { _ = session.Kill(p); time.Sleep(200 * time.Millisecond) })
	old := agentCall
	t.Cleanup(func() { agentCall = old })
	agentCall = func(string, agentctl.Request, time.Duration) (agentctl.Reply, error) {
		return agentctl.Reply{OK: true, Tabs: []agentctl.Tab{{ID: "t1"}}}, nil
	}
	list, err := barSessions()
	if err != nil || len(list) != 1 || len(list[0].Tabs) != 1 {
		t.Fatalf("%v %v", list, err)
	}
	agentCall = func(string, agentctl.Request, time.Duration) (agentctl.Reply, error) {
		return agentctl.Reply{}, errors.New("no answer")
	}
	if list, _ := barSessions(); len(list) != 1 || !list[0].Unreachable {
		t.Fatalf("unreachable: %v", list)
	}
}
