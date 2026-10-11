//go:build e2e

package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// sessionRig is one workspace + HOME + runtime dir shared by several
// termocode clients, as when a user reconnects over SSH.
type sessionRig struct {
	t       *testing.T
	home    string
	ws      string
	runtime string
	env     []string
}

// newSessionRig: SSH_CONNECTION makes the default (auto) use a
// persistent session; XDG_RUNTIME_DIR is short because unix socket paths
// are limited to ~100 bytes.
func newSessionRig(t *testing.T) *sessionRig {
	t.Helper()
	rt, err := os.MkdirTemp("", "tce2e")
	if err != nil {
		t.Fatal(err)
	}
	r := &sessionRig{
		t: t, home: t.TempDir(), ws: NewRepo(t), runtime: rt,
		env: []string{"XDG_RUNTIME_DIR=" + rt, "SSH_CONNECTION=10.0.0.1 50000 10.0.0.2 22"},
	}
	t.Cleanup(func() {
		// Never leave a server behind, whatever the test did.
		if _, err := os.Stat(r.socket()); err == nil {
			_ = r.run("kill")
		}
		_ = os.RemoveAll(rt)
	})
	return r
}

// socket is where the server of r.ws listens (session.Key: first 8 bytes
// of sha256 of the absolute path, hex).
func (r *sessionRig) socket() string {
	sum := sha256.Sum256([]byte(filepath.Clean(r.ws)))
	return filepath.Join(r.runtime, "termocode", hex.EncodeToString(sum[:8])+".sock")
}

// start runs one termocode client and waits for the editor frame.
func (r *sessionRig) start(args ...string) *Term {
	r.t.Helper()
	tm := Start(r.t, StartOpts{Home: r.home, Workspace: r.ws, Env: r.env, Args: args})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	return tm
}

// run runs a termocode subcommand (no terminal) in the workspace.
func (r *sessionRig) run(args ...string) error {
	_, err := r.output(args...)
	return err
}

func (r *sessionRig) output(args ...string) (string, error) {
	cmd := exec.Command(binPath, args...)
	cmd.Dir = r.ws
	cmd.Env = append(isolatedEnv(r.home), r.env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *sessionRig) waitSocket(exists bool) {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err := os.Stat(r.socket())
		if (err == nil) == exists {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("socket %s exists=%v, want %v", r.socket(), err == nil, exists)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Disconnect is a dropped SSH connection: the client gets SIGHUP and its
// terminal goes away. (Closing our PTY end alone does not hang up while
// the reader goroutine still holds it.)
func (tm *Term) Disconnect() {
	tm.t.Helper()
	_ = tm.cmd.Process.Signal(syscall.SIGHUP)
	_ = tm.ptmx.Close()
	select {
	case <-tm.exited:
	case <-time.After(10 * time.Second):
		tm.t.Fatalf("client did not exit after the PTY closed")
	}
}

// ClickText clicks the first cell of text on screen.
func (tm *Term) ClickText(text string) {
	tm.t.Helper()
	for y, l := range tm.Lines() {
		if i := strings.Index(l, text); i >= 0 {
			tm.Click(ansi.StringWidth(l[:i]), y)
			return
		}
	}
	tm.t.Fatalf("%q not on screen:\n%s", text, tm.Screen())
}

// quitDiscard quits with Ctrl+Q and answers Don't Save to the unsaved
// question.
func (tm *Term) quitDiscard() {
	tm.t.Helper()
	tm.Send(KeyCtrlQ)
	tm.WaitText("Save changes before quitting?", uiTimeout)
	tm.settle()
	tm.mu.Lock()
	tm.lastApp = tm.linesLocked()
	tm.mu.Unlock()
	tm.ClickText("Don't Save")
	select {
	case <-tm.exited:
	case <-time.After(10 * time.Second):
		tm.t.Fatalf("termocode did not exit after Don't Save\n%s", tm.Screen())
	}
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// TestSessionSurvivesDisconnect is the SSH drop: unsaved text, the
// terminal panel's output and a background job are all still there after
// the client dies and a new one attaches.
func TestSessionSurvivesDisconnect(t *testing.T) {
	r := newSessionRig(t)
	tm := r.start()
	r.waitSocket(true)

	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Type("ounsaved-marker-42")
	tm.WaitText("unsaved-marker-42", uiTimeout)

	tm.OpenTerminal()
	tm.Type("sleep 1000 & echo job-$! term-marker-7\r")
	tm.WaitText("term-marker-7", uiTimeout)
	m := regexp.MustCompile(`job-(\d+) term-marker-7`).FindStringSubmatch(tm.Screen())
	if m == nil {
		t.Fatalf("no job pid on screen:\n%s", tm.Screen())
	}
	pid, _ := strconv.Atoi(m[1])
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	tm.settle()

	tm.Disconnect()
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(r.socket()); err != nil {
		t.Fatalf("session gone after the client died: %v", err)
	}
	if !alive(pid) {
		t.Fatalf("background job %d died with the client", pid)
	}

	// Reconnect: a new terminal, a different size.
	tm2 := Start(t, StartOpts{Home: r.home, Workspace: r.ws, Env: r.env, Cols: 110, Rows: 34})
	tm2.WaitText("unsaved-marker-42", bootTimeout)
	tm2.WaitText("term-marker-7", uiTimeout)
	tm2.settle()
	tm2.CheckScreen("reattached", mainFrame)
	if !strings.Contains(tm2.Lines()[0], "notes.txt") {
		t.Errorf("notes.txt tab not shown after reattach: %q", tm2.Lines()[0])
	}
	for _, mode := range []string{"?1003", "?1006", "?2004"} {
		tm2.mu.Lock()
		on := tm2.modes[mode]
		tm2.mu.Unlock()
		if !on {
			t.Errorf("mode %s not enabled on the new terminal", mode)
		}
	}
	if !alive(pid) {
		t.Errorf("background job %d died", pid)
	}

	// The UI still works: type more.
	tm2.Send(KeyCtrlT) // focus back to the editor (toggle terminal off)
	tm2.settle()

	// Quit ends the session for real.
	tm2.quitDiscard()
	r.waitSocket(false)
	if tm2.exitErr != nil {
		t.Errorf("client exit: %v", tm2.exitErr)
	}
	tm2.mu.Lock()
	alt := tm2.em.IsAltScreen()
	tm2.mu.Unlock()
	if alt {
		t.Errorf("still on the alt screen after quit")
	}
}

// TestSessionTakeover: a second client takes the session; the first one
// is told and exits cleanly with its terminal restored.
func TestSessionTakeover(t *testing.T) {
	r := newSessionRig(t)
	a := r.start()
	a.OpenFile("notes.txt")
	a.WaitText("first note", uiTimeout)

	b := Start(t, StartOpts{Home: r.home, Workspace: r.ws, Env: r.env})
	b.WaitText("first note", bootTimeout)
	select {
	case <-a.exited:
	case <-time.After(10 * time.Second):
		t.Fatalf("first client still running after takeover\n%s", a.Screen())
	}
	if a.exitErr != nil {
		t.Errorf("first client exit: %v", a.exitErr)
	}
	a.WaitText("attached from another terminal", uiTimeout)
	a.mu.Lock()
	alt, mouse := a.em.IsAltScreen(), a.modes["?1003"]
	a.mu.Unlock()
	if alt || mouse {
		t.Errorf("old terminal not restored: alt=%v mouse=%v", alt, mouse)
	}

	out, err := r.output("sessions")
	if err != nil || !strings.Contains(out, r.ws) || !strings.Contains(out, "attached") {
		t.Errorf("sessions: %v\n%s", err, out)
	}

	b.Quit()
	r.waitSocket(false)
}

// TestSessionNoSessionFlag: --no-session runs the UI directly.
func TestSessionNoSessionFlag(t *testing.T) {
	r := newSessionRig(t)
	tm := r.start("--no-session")
	if _, err := os.Stat(r.socket()); err == nil {
		t.Fatalf("--no-session started a session server")
	}
	tm.Quit()
}

// TestSessionStaleSocket: a socket left by a dead server is cleaned up
// and a new session starts.
func TestSessionStaleSocket(t *testing.T) {
	r := newSessionRig(t)
	if err := os.MkdirAll(filepath.Dir(r.socket()), 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: r.socket(), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	_ = ln.Close() // the file stays, nobody listens: a dead server
	if out, _ := r.output("sessions"); strings.Contains(out, r.ws) {
		t.Errorf("stale session listed:\n%s", out)
	}

	tm := r.start()
	r.waitSocket(true)
	if out, err := r.output("kill"); err != nil {
		t.Fatalf("kill: %v\n%s", err, out)
	}
	r.waitSocket(false)
	select {
	case <-tm.exited:
	case <-time.After(10 * time.Second):
		t.Fatalf("client still running after kill")
	}
	if out, err := r.output("attach"); err == nil || !strings.Contains(out, "no session") {
		t.Errorf("attach without a session: %v\n%s", err, out)
	}
}
