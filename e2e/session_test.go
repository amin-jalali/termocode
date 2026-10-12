//go:build e2e

package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	return filepath.Join(r.runtime, "termocode", hashKey(r.ws)+".sock")
}

// hashKey is session.Key: the workspace id used for sockets and backups.
func hashKey(ws string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(ws)))
	return hex.EncodeToString(sum[:8])
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

// ClickText clicks the first cell of the lowest text on screen (a button
// sits below a title that may contain the same word).
func (tm *Term) ClickText(text string) {
	tm.t.Helper()
	ls := tm.Lines()
	for y := len(ls) - 1; y >= 0; y-- {
		l := ls[y]
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
	jobRe := regexp.MustCompile(`job-(\d+) term-marker-7`)
	// Not the echoed command line: its output.
	tm.WaitFor("job output", uiTimeout, func(ls []string) bool { return jobRe.MatchString(strings.Join(ls, "\n")) })
	m := jobRe.FindStringSubmatch(tm.Screen())
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
	tm2.WaitText("job-"+m[1]+" term-marker-7", uiTimeout)
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

// rawOutput is everything the client's terminal received so far.
func (tm *Term) rawOutput() string {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.raw.String()
}

// TestSessionReattachNewTerminalCaps: the UI follows the terminal that
// attaches, not the one it started in. A truecolor terminal starts the
// session; a 256-colour terminal attaches next and must get 256-colour
// SGR only; a kitty terminal then gets the welcome logo as a kitty image.
//
// TERM=xterm-256color without COLORTERM alone still means truecolor
// (theme.DetectProfile: COLORTERM is rarely forwarded over SSH), so the
// 256-colour terminal also sets TERMOCODE_COLORS=256, as a user of such
// a terminal does.
func TestSessionReattachNewTerminalCaps(t *testing.T) {
	r := newSessionRig(t)
	truecolor := regexp.MustCompile(`\x1b\[[0-9;]*[34]8;2;`)
	color256 := regexp.MustCompile(`\x1b\[[0-9;]*[34]8;5;`)
	// A kitty image put on screen (not the client's own graphics probe).
	const kittyImage = "\x1b_Ga=T"

	a := r.start() // TERM=xterm-256color COLORTERM=truecolor
	r.waitSocket(true)
	if !truecolor.MatchString(a.rawOutput()) {
		t.Fatalf("first terminal: no truecolor SGR")
	}
	a.Disconnect()

	env := append(append([]string(nil), r.env...), "COLORTERM=", "TERM=xterm-256color", "TERMOCODE_COLORS=256")
	b := Start(t, StartOpts{Home: r.home, Workspace: r.ws, Env: env})
	b.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	b.settle()
	b.CheckScreen("256-colour reattach", mainFrame)
	out := b.rawOutput()
	if truecolor.MatchString(out) {
		t.Errorf("24-bit SGR sent to a 256-colour terminal: %q", truecolor.FindString(out))
	}
	if !color256.MatchString(out) {
		t.Errorf("no 256-colour SGR after the 256-colour terminal attached")
	}
	if strings.Contains(out, kittyImage) {
		t.Errorf("kitty graphics sent to a terminal without them")
	}
	b.Disconnect()

	env = append(append([]string(nil), r.env...), "TERM=xterm-kitty")
	c := Start(t, StartOpts{Home: r.home, Workspace: r.ws, Env: env})
	c.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	c.settle()
	out = c.rawOutput()
	if !truecolor.MatchString(out) {
		t.Errorf("no truecolor SGR on the kitty terminal")
	}
	if !strings.Contains(out, kittyImage) {
		t.Errorf("welcome logo not drawn as a kitty image after attach")
	}
	c.Quit()
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
// and a new session starts; `termocode kill` ends it and keeps unsaved
// text in the hot-exit backup.
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
	// Unsaved text: kill must keep it in the hot-exit backup.
	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Type("okilled-marker")
	tm.WaitText("killed-marker", uiTimeout)
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
	found := false
	for _, f := range backupFiles(r.home) {
		if b, _ := os.ReadFile(f); strings.Contains(string(b), "killed-marker") {
			found = true
		}
	}
	if !found {
		t.Errorf("kill lost the unsaved text: backups %v", backupFiles(r.home))
	}
}

// TestSessionDetachCommand: "Session: Detach" gives the terminal back and
// keeps the session; the next attach finds the same unsaved text.
func TestSessionDetachCommand(t *testing.T) {
	r := newSessionRig(t)
	tm := r.start()
	r.waitSocket(true)
	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Type("odetach-marker-9")
	tm.WaitText("detach-marker-9", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()

	tm.Palette("Session: Detach")
	select {
	case <-tm.exited:
	case <-time.After(10 * time.Second):
		t.Fatalf("client still running after Session: Detach\n%s", tm.Screen())
	}
	if tm.exitErr != nil {
		t.Errorf("client exit: %v", tm.exitErr)
	}
	tm.WaitText("the session keeps running", uiTimeout)
	if tm.Has("termocode:detach") {
		t.Errorf("the detach request reached the terminal:\n%s", tm.Screen())
	}
	tm.mu.Lock()
	alt, mouse := tm.em.IsAltScreen(), tm.modes["?1003"]
	tm.mu.Unlock()
	if alt || mouse {
		t.Errorf("terminal not restored after detach: alt=%v mouse=%v", alt, mouse)
	}
	out, err := r.output("sessions")
	if err != nil || !strings.Contains(out, r.ws) || !strings.Contains(out, "detached") {
		t.Errorf("sessions after detach: %v\n%s", err, out)
	}

	tm2 := Start(t, StartOpts{Home: r.home, Workspace: r.ws, Env: r.env, Args: []string{"attach"}})
	tm2.WaitText("detach-marker-9", bootTimeout)
	tm2.settle()
	tm2.quitDiscard()
	r.waitSocket(false)
}

// TestSessionAttachInsideRefused: `termocode attach` in the session's own
// terminal panel must not attach the session to itself.
func TestSessionAttachInsideRefused(t *testing.T) {
	r := newSessionRig(t)
	tm := r.start()
	r.waitSocket(true)
	tm.OpenTerminal()
	tm.Type(binPath + " attach; echo rc-$?\r")
	tm.WaitText("already inside this session", uiTimeout)
	tm.WaitText("rc-1", uiTimeout)
	select {
	case <-tm.exited:
		t.Fatalf("the outer client was taken over\n%s", tm.Screen())
	default:
	}
	tm.Send(KeyCtrlT)
	tm.settle()
	tm.Quit()
	r.waitSocket(false)
}

// TestSessionServerKilledRestore: the session server dies hard (SIGKILL,
// no cleanup). The UI gets a hangup with its PTY, and the next start
// offers the hot-exit backup and restores the unsaved text.
func TestSessionServerKilledRestore(t *testing.T) {
	r := newSessionRig(t)
	tm := r.start()
	r.waitSocket(true)
	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Type("osigkill-marker-5")
	tm.WaitText("sigkill-marker-5", uiTimeout)
	deadline := time.Now().Add(5 * time.Second)
	for len(backupFiles(r.home)) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no hot-exit backup written")
		}
		time.Sleep(50 * time.Millisecond)
	}

	var in struct {
		PID   int `json:"pid"`
		UIPID int `json:"ui_pid"`
	}
	b, err := os.ReadFile(strings.TrimSuffix(r.socket(), ".sock") + ".json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &in); err != nil || in.PID == 0 || in.UIPID == 0 {
		t.Fatalf("session info %s: %v", b, err)
	}
	if err := syscall.Kill(in.PID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	select {
	case <-tm.exited:
	case <-time.After(10 * time.Second):
		t.Fatalf("client still running after the server died")
	}
	deadline = time.Now().Add(10 * time.Second)
	for alive(in.UIPID) {
		if time.Now().After(deadline) {
			t.Fatalf("UI %d still running after the server died", in.UIPID)
		}
		time.Sleep(20 * time.Millisecond)
	}

	tm2 := Start(t, StartOpts{Home: r.home, Workspace: r.ws, Env: r.env})
	tm2.WaitText("Restore unsaved changes?", bootTimeout)
	tm2.WaitText("notes.txt", uiTimeout)
	tm2.settle()
	tm2.ClickText("Restore")
	tm2.WaitText("sigkill-marker-5", uiTimeout)
	tm2.WaitFor("notes.txt tab, modified", uiTimeout, func(ls []string) bool {
		return strings.Contains(ls[0], "notes.txt ●")
	})
	tm2.quitDiscard()
	r.waitSocket(false)
}
