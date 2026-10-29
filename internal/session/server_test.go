//go:build unix

package session

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
)

// The session tests run real servers as separate processes (the test
// binary itself, see TestMain), with a shell command as the UI instead of
// the app.
const (
	envServe        = "TERMOCODE_TEST_SERVE"         // workspace to serve
	envUI           = "TERMOCODE_TEST_UI"            // sh -c script of the UI
	envWriteTimeout = "TERMOCODE_TEST_WRITE_TIMEOUT" // clientWriteTimeout
	envHoldTimeout  = "TERMOCODE_TEST_HOLD_TIMEOUT"  // holdTimeout
	envPanicIn      = "TERMOCODE_TEST_PANIC_IN"      // crash.PanicIn in the server
)

// catUI echoes its input. It ignores RepaintSignal like the app does (a
// Go program ignores SIGUSR1 unless it asks for it).
const catUI = "trap '' USR1; exec cat"

// serverMarker is written to the server log when the UI starts.
const serverMarker = "test-server-started"

func TestMain(m *testing.M) {
	if os.Getenv(envFakeRemote) != "" {
		os.Exit(fakeRemote())
	}
	if ws := os.Getenv(envServe); ws != "" {
		if d, err := time.ParseDuration(os.Getenv(envWriteTimeout)); err == nil {
			clientWriteTimeout = d
		}
		if d, err := time.ParseDuration(os.Getenv(envHoldTimeout)); err == nil {
			holdTimeout = d
		}
		if w := os.Getenv(envPanicIn); w != "" {
			crash.PanicIn(w)
		}
		os.Exit(Serve(ws, func() *exec.Cmd {
			fmt.Fprintln(os.Stderr, serverMarker)
			return exec.Command("sh", "-c", os.Getenv(envUI))
		}))
	}
	os.Exit(m.Run())
}

// rig is one workspace with its own socket dir.
type rig struct {
	t       *testing.T
	p       Paths
	ui      string
	timeout time.Duration
	hold    time.Duration // holdTimeout of the server
	panicIn string        // a goroutine of the server that panics (crash.PanicIn)
	uiPIDs  string        // file each UI appends its pid (= process group) to

	mu    sync.Mutex
	procs []*exec.Cmd
	exits map[*exec.Cmd]chan struct{}
}

func newRig(t *testing.T, ui string) *rig {
	t.Helper()
	// Short: unix socket paths are limited to ~100 bytes.
	rt, err := os.MkdirTemp("", "tcs")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", rt)
	p, err := PathsFor(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, p: p, ui: ui, uiPIDs: filepath.Join(rt, "ui.pids"), exits: map[*exec.Cmd]chan struct{}{}}
	t.Cleanup(func() {
		r.mu.Lock()
		procs := r.procs
		r.mu.Unlock()
		for _, c := range procs {
			// SIGKILL: the master side closes and the UI gets a hangup.
			_ = c.Process.Kill()
			<-r.exits[c]
		}
		// No UI (or child of it) may outlive its server.
		b, _ := os.ReadFile(r.uiPIDs)
		for _, f := range strings.Fields(string(b)) {
			pgid, _ := strconv.Atoi(f)
			deadline := time.Now().Add(3 * time.Second)
			for pgid > 0 && syscall.Kill(-pgid, 0) == nil {
				if time.Now().After(deadline) {
					t.Errorf("UI process group %d survived its server", pgid)
					_ = syscall.Kill(-pgid, syscall.SIGKILL)
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		_ = os.RemoveAll(rt)
	})
	return r
}

// spawn starts a session server process the way the app does: in its
// own session, output to the session log.
func (r *rig) spawn() (*exec.Cmd, error) {
	logf, err := OpenLog(r.p)
	if err != nil {
		return nil, err
	}
	defer logf.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), envServe+"="+r.p.Workspace, envUI+"=echo $$ >>"+r.uiPIDs+"; "+r.ui)
	if r.timeout > 0 {
		cmd.Env = append(cmd.Env, envWriteTimeout+"="+r.timeout.String())
	}
	if r.hold > 0 {
		cmd.Env = append(cmd.Env, envHoldTimeout+"="+r.hold.String())
	}
	if r.panicIn != "" {
		cmd.Env = append(cmd.Env, envPanicIn+"="+r.panicIn)
	}
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	r.mu.Lock()
	r.procs = append(r.procs, cmd)
	r.exits[cmd] = done
	r.mu.Unlock()
	return cmd, nil
}

// waitExit waits until the server process pid (started by spawn) has
// exited and been reaped.
func (r *rig) waitExit(pid int) {
	r.t.Helper()
	r.mu.Lock()
	var done chan struct{}
	for _, c := range r.procs {
		if c.Process.Pid == pid {
			done = r.exits[c]
		}
	}
	r.mu.Unlock()
	if done == nil {
		r.t.Fatalf("server pid %d was not started by this rig", pid)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		r.t.Fatalf("server pid %d did not exit", pid)
	}
}

// ensure connects, starting a server when none runs.
func (r *rig) ensure() *net.UnixConn {
	r.t.Helper()
	c, err := Ensure(r.p, func() error { _, err := r.spawn(); return err })
	if err != nil {
		r.t.Fatal(err)
	}
	return c
}

// hello attaches c as the client.
func hello(t *testing.T, c *net.UnixConn) {
	t.Helper()
	if err := writeFrame(c, frameHello, encodeSize(80, 24)); err != nil {
		t.Fatal(err)
	}
}

// readUntil reads frames until the output contains want or a frame of
// type stop arrives. It returns the type of the last frame.
func readUntil(t *testing.T, c *net.UnixConn, want string, stop byte) byte {
	t.Helper()
	return readUntilMatch(t, c, want, stop, func(out []byte) bool { return bytes.Contains(out, []byte(want)) })
}

// readUntilMatch is readUntil with its own test of the output so far.
func readUntilMatch(t *testing.T, c *net.UnixConn, want string, stop byte, match func([]byte) bool) byte {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer func() { _ = c.SetReadDeadline(time.Time{}) }()
	var out []byte
	for {
		typ, p, err := readFrame(c)
		if err != nil {
			t.Fatalf("waiting for %q: %v (output %q)", want, err, out)
		}
		if typ == stop {
			return typ
		}
		if typ == frameOutput {
			out = append(out, p...)
			if want != "" && match(out) {
				return typ
			}
		}
	}
}

// echo types a line into the UI (cat) and waits for it to come back.
func echo(t *testing.T, c *net.UnixConn, s string) {
	t.Helper()
	if err := writeFrame(c, frameInput, []byte(s+"\r")); err != nil {
		t.Fatal(err)
	}
	// A second client's attach can put an empty line between the echo
	// and cat's answer, so empty lines are skipped before matching.
	want := []byte(s + "\r\n" + s)
	readUntilMatch(t, c, s+"\r\n"+s, 0, func(out []byte) bool {
		for bytes.Contains(out, []byte("\r\n\r\n")) {
			out = bytes.ReplaceAll(out, []byte("\r\n\r\n"), []byte("\r\n"))
		}
		return bytes.Contains(out, want)
	})
}

// serverPID returns the pid of the running server.
func (r *rig) status() Info {
	r.t.Helper()
	in, err := Status(r.p)
	if err != nil {
		r.t.Fatal(err)
	}
	return in
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// dirFiles lists the socket dir.
func (r *rig) dirFiles() []string {
	es, _ := os.ReadDir(filepath.Dir(r.p.Socket))
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

// TestServerKilledStaleSocket: a server killed with SIGKILL leaves its
// socket; its UI gets a hangup, and the next client starts a new server.
func TestServerKilledStaleSocket(t *testing.T) {
	r := newRig(t, catUI)
	c := r.ensure()
	hello(t, c)
	echo(t, c, "one")
	in := r.status()
	if in.UIPID == 0 {
		t.Fatalf("no UI pid in %+v", in)
	}

	if err := syscall.Kill(in.PID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	// Frames the server sent just before the kill may still arrive; the
	// connection must end soon after them.
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, _, err := readFrame(c)
		if err == nil {
			continue
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			t.Fatal("client still connected 5 s after the server was killed")
		}
		break
	}
	_ = c.Close()
	waitFor(t, "the UI to get the hangup", func() bool { return !alive(in.UIPID) })
	// The client's EOF and the UI's hangup do not mean the server is
	// gone: the kernel releases a dead process's files one by one, and
	// its listener may still take a connection for a moment, which it
	// then resets (seen in CI). Wait until the server is reaped; by then
	// all its files are closed and only the stale socket file is left.
	r.waitExit(in.PID)
	if _, err := os.Stat(r.p.Socket); err != nil {
		t.Fatalf("SIGKILL should leave the socket: %v", err)
	}
	if _, err := os.Stat(r.p.Log); err != nil {
		t.Errorf("the log of a crashed server must stay: %v", err)
	}

	c2 := r.ensure()
	defer c2.Close()
	hello(t, c2)
	echo(t, c2, "two")
	if in2 := r.status(); in2.PID == in.PID {
		t.Errorf("same server pid %d", in.PID)
	}
}

// TestConcurrentStart: two clients start at the same moment (over a
// stale socket). Exactly one server runs; one client gets the session,
// the other is cleanly detached by the takeover.
func TestConcurrentStart(t *testing.T) {
	r := newRig(t, catUI)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: r.p.Socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	_ = ln.Close()

	var wg sync.WaitGroup
	conns := make([]*net.UnixConn, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			conns[i], errs[i] = Ensure(r.p, func() error { _, err := r.spawn(); return err })
		}()
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("client %d: %v", i, err)
		}
		defer conns[i].Close()
	}

	// Only one server is left (a second one exits at once).
	r.mu.Lock()
	procs := append([]*exec.Cmd(nil), r.procs...)
	r.mu.Unlock()
	exited := func() (n int) {
		for _, p := range procs {
			select {
			case <-r.exits[p]:
				n++
				if code := p.ProcessState.ExitCode(); code != 0 {
					t.Errorf("extra server exit code %d", code)
				}
			default:
			}
		}
		return n
	}
	waitFor(t, "the extra servers to exit", func() bool { return exited() >= len(procs)-1 })
	if n := exited(); n != len(procs)-1 {
		t.Fatalf("%d of %d servers exited, want all but one", n, len(procs))
	}

	a, b := conns[0], conns[1]
	hello(t, a)
	echo(t, a, "first")
	hello(t, b)
	if typ := readUntil(t, a, "", frameDetached); typ != frameDetached {
		t.Fatalf("first client not detached")
	}
	echo(t, b, "second")
}

// TestFrozenClientDropped: a client that stops reading is dropped after
// the write timeout; the UI keeps running and a new client gets it.
func TestFrozenClientDropped(t *testing.T) {
	r := newRig(t, "trap '' USR1; while :; do echo flood-flood-flood-flood || exit; done")
	r.timeout = 200 * time.Millisecond
	frozen := r.ensure()
	defer frozen.Close()
	hello(t, frozen)
	waitFor(t, "the frozen client to be dropped", func() bool {
		in, err := Status(r.p)
		return err == nil && in.UIPID != 0 && !in.Attached
	})

	c := r.ensure()
	defer c.Close()
	hello(t, c)
	readUntil(t, c, "flood-flood", 0)
	go func() { // keep reading, or this client is dropped too
		for {
			if _, _, err := readFrame(c); err != nil {
				return
			}
		}
	}()
	if !r.status().Attached {
		t.Errorf("new client not attached")
	}
}

// TestReattachPassesClientCaps: a client that attaches to a running UI
// hands its terminal caps to the UI (a file named in its environment, then
// RepaintSignal). A client without caps (an old one) leaves no stale file
// from the client before.
//
// Output the UI writes before it marks the switch (AttachedSeq with the
// attach number from the caps) was drawn for the old terminal and never
// reaches the new client.
func TestReattachPassesClientCaps(t *testing.T) {
	r := newRig(t, `F="$`+EnvCapsFile+`"; trap 'if [ -f "$F" ]; then echo stale-output; `+
		`printf "\033_termocode:attached;%s\033\\\\" "$(sed -n "s/.*\"gen\":\([0-9]*\).*/\1/p" "$F")"; `+
		`cat "$F"; echo; echo caps-shown; else echo caps-none; fi' USR1; echo ui-up; while :; do sleep 0.02; done`)
	first := r.ensure()
	defer first.Close()
	if err := writeFrame(first, frameHello, encodeHello(80, 24, &ClientCaps{Env: map[string]string{"TERM": "xterm-kitty"}})); err != nil {
		t.Fatal(err)
	}
	readUntil(t, first, "ui-up", 0)

	second := r.ensure()
	defer second.Close()
	caps := &ClientCaps{
		Env:      map[string]string{"TERM": "xterm-second", "COLORTERM": "truecolor"},
		Graphics: &GraphicsCaps{Protocol: "sixel", CellW: 8, CellH: 16},
	}
	if err := writeFrame(second, frameHello, encodeHello(100, 30, caps)); err != nil {
		t.Fatal(err)
	}
	var seen strings.Builder
	out, ok := readFramesUntil(t, second, func(typ byte, p []byte) bool {
		if typ == frameOutput {
			seen.Write(p)
		}
		return strings.Contains(seen.String(), "caps-shown")
	})
	if !ok {
		t.Fatalf("no caps-shown; output %q", out)
	}
	if strings.Contains(out, "stale-output") || strings.Contains(out, "termocode:attached") {
		t.Errorf("output from before the switch (or the mark) reached the new client: %q", out)
	}
	got, err := ReadCaps(r.p.Caps)
	if err != nil || got == nil || got.Env["TERM"] != "xterm-second" || got.Graphics == nil || got.Graphics.Protocol != "sixel" {
		t.Fatalf("caps file = %+v, %v", got, err)
	}

	old := r.ensure()
	defer old.Close()
	hello(t, old) // size only, like an old client
	readUntil(t, old, "caps-none", 0)
}

// TestReattachHoldTimeout: a UI that never marks the switch (an older
// one, or one that is stuck) does not leave the new client blank: after
// holdTimeout it gets the output and the UI is asked to redraw again.
func TestReattachHoldTimeout(t *testing.T) {
	r := newRig(t, "n=0; trap 'n=$((n+1)); echo painted-$n' USR1; echo ui-up; while :; do sleep 0.02; done")
	r.hold = 200 * time.Millisecond
	first := r.ensure()
	defer first.Close()
	if err := writeFrame(first, frameHello, encodeHello(80, 24, &ClientCaps{})); err != nil {
		t.Fatal(err)
	}
	readUntil(t, first, "ui-up", 0)
	second := r.ensure()
	defer second.Close()
	start := time.Now()
	if err := writeFrame(second, frameHello, encodeHello(80, 24, &ClientCaps{})); err != nil {
		t.Fatal(err)
	}
	readUntil(t, second, "painted-2", 0)
	if d := time.Since(start); d < r.hold {
		t.Errorf("output after %v, before the hold timeout", d)
	}
}

// TestKillAfterUIExit: kill when the UI has already exited (a child of
// it still holds the PTY) ends the session without hanging, and the
// child does not outlive the session.
func TestKillAfterUIExit(t *testing.T) {
	bg := filepath.Join(t.TempDir(), "bg.pid")
	r := newRig(t, "trap '' USR1; sleep 30 & echo $! >"+bg+"; exit 3")
	c := r.ensure()
	defer c.Close()
	hello(t, c)
	waitFor(t, "the UI to start its child", func() bool {
		b, _ := os.ReadFile(bg)
		return strings.HasSuffix(string(b), "\n")
	})
	_ = Kill(r.p) // the session may already be gone
	if typ := readUntil(t, c, "", frameExit); typ != frameExit {
		t.Fatalf("no exit frame")
	}
	if err := Kill(r.p); !errors.Is(err, ErrNoSession) {
		t.Errorf("second kill: %v, want ErrNoSession", err)
	}
	b, _ := os.ReadFile(bg)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	waitFor(t, "the UI's child to end", func() bool { return !alive(pid) })
}

// TestUIGetsHangupWhenServerDies: the UI must not inherit the server's
// ignored SIGHUP: when the server dies, the hangup ends the UI even if
// it never reads its terminal.
func TestUIGetsHangupWhenServerDies(t *testing.T) {
	r := newRig(t, "trap '' USR1; exec sleep 300")
	c := r.ensure()
	defer c.Close()
	hello(t, c)
	var in Info
	waitFor(t, "the UI to start", func() bool { in = r.status(); return in.UIPID != 0 })
	if err := syscall.Kill(in.PID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the UI to get the hangup", func() bool { return !alive(in.UIPID) })
}

// TestTerminateTwiceBeforeUI: two kills of a server whose UI never
// started must not block.
func TestTerminateTwiceBeforeUI(t *testing.T) {
	s := &Server{done: make(chan int, 1)}
	finished := make(chan struct{})
	go func() {
		s.terminate()
		s.terminate()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("second terminate blocked")
	}
	if code := <-s.done; code != 0 {
		t.Errorf("exit code %d", code)
	}
}

// TestDialNoSessionLeavesNoFiles: `termocode attach` in a folder without
// a session must not leave files behind.
func TestDialNoSessionLeavesNoFiles(t *testing.T) {
	r := newRig(t, catUI)
	if _, err := Dial(r.p); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Dial: %v", err)
	}
	if fs := r.dirFiles(); len(fs) > 0 {
		t.Errorf("files left: %v", fs)
	}
}

// TestStaleCleanupLeavesNoLock: cleaning up after a dead server removes
// its socket, info and lock (the log stays).
func TestStaleCleanupLeavesNoLock(t *testing.T) {
	r := newRig(t, catUI)
	for _, f := range []string{r.p.Lock, r.p.Info} {
		if err := os.WriteFile(f, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: r.p.Socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	_ = ln.Close()
	if _, err := Dial(r.p); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Dial: %v", err)
	}
	if fs := r.dirFiles(); len(fs) > 0 {
		t.Errorf("files left: %v", fs)
	}
}

// TestSessionEndRemovesFiles: a session that ends normally leaves no
// files in the socket dir.
func TestSessionEndRemovesFiles(t *testing.T) {
	r := newRig(t, catUI)
	c := r.ensure()
	defer c.Close()
	hello(t, c)
	echo(t, c, "bye")
	if err := writeFrame(c, frameInput, []byte{4}); err != nil { // ^D: cat exits
		t.Fatal(err)
	}
	readUntil(t, c, "", frameExit)
	r.mu.Lock()
	srv := r.procs[0]
	r.mu.Unlock()
	select {
	case <-r.exits[srv]:
	case <-time.After(10 * time.Second):
		t.Fatal("server still running")
	}
	if fs := r.dirFiles(); len(fs) > 0 {
		t.Errorf("files left: %v", fs)
	}
}

// TestLosingServerKeepsLog: a second server started for a running
// session (two clients at once) must not wipe the running server's log.
func TestLosingServerKeepsLog(t *testing.T) {
	r := newRig(t, catUI)
	c := r.ensure()
	defer c.Close()
	hello(t, c)
	echo(t, c, "x")
	waitFor(t, "the log line", func() bool {
		b, _ := os.ReadFile(r.p.Log)
		return bytes.Contains(b, []byte(serverMarker))
	})
	loser, err := r.spawn()
	if err != nil {
		t.Fatal(err)
	}
	<-r.exits[loser]
	// The winner writes again (at its own offset in the file).
	b, _ := os.ReadFile(r.p.Log)
	if !strings.HasPrefix(string(b), serverMarker) {
		t.Errorf("log after a second spawn: %q", b)
	}
}

// testConn returns both ends of a fresh unix socket connection.
func testConn(t *testing.T) (server, client *net.UnixConn) {
	t.Helper()
	dir, err := os.MkdirTemp("", "tcs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, "s.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	client, err = net.DialUnix("unix", nil, ln.Addr().(*net.UnixAddr))
	if err != nil {
		t.Fatal(err)
	}
	server, err = ln.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close(); _ = client.Close() })
	return server, client
}

// readFramesUntil reads frames from c until want returns true for one of them
// or the connection ends. It returns all output bytes seen.
func readFramesUntil(t *testing.T, c *net.UnixConn, want func(typ byte, p []byte) bool) (string, bool) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	var out strings.Builder
	for {
		typ, p, err := readFrame(c)
		if err != nil {
			return out.String(), false
		}
		if typ == frameOutput {
			out.Write(p)
		}
		if want(typ, p) {
			return out.String(), true
		}
	}
}

// TestServerDetachRequest: the UI writes DetachSeq; the client gets its
// terminal reset and the "detached" message, never the sequence itself,
// and the session keeps running for the next client.
func TestServerDetachRequest(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "detached")
	// The UI: draw, ask for a detach, then wait for a file before drawing
	// again (so the second client sees fresh output).
	script := `trap '' USR1; printf '\033[?1049hhello'; printf '\033_termocode:detach\033\\'; ` +
		`while [ ! -e "` + marker + `" ]; do sleep 0.02; done; printf 'again'; sleep 30`
	s := &Server{
		paths: Paths{Workspace: dir, Info: filepath.Join(dir, "info.json")},
		uiCmd: func() *exec.Cmd { return exec.Command("sh", "-c", script) },
		modes: NewModes(), started: time.Now(), done: make(chan int, 1),
	}
	t.Cleanup(func() {
		s.mu.Lock()
		if s.cmd != nil && s.cmd.Process != nil {
			// The whole group: the UI's sleep must not outlive the test.
			_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		}
		s.mu.Unlock()
	})

	srv, cli := testConn(t)
	if err := s.attach(srv, 80, 24, nil); err != nil {
		t.Fatal(err)
	}
	var msg string
	out, ok := readFramesUntil(t, cli, func(typ byte, p []byte) bool {
		msg = string(p)
		return typ == frameDetached
	})
	if !ok {
		t.Fatalf("no detached frame; output %q", out)
	}
	if !strings.Contains(msg, "termocode attach") {
		t.Errorf("detached message %q does not say how to go back", msg)
	}
	if strings.Contains(out, "termocode:detach") {
		t.Errorf("detach request reached the client terminal: %q", out)
	}
	if !strings.Contains(out, "hello") || !strings.HasSuffix(out, "\x1b[?1049l") {
		t.Errorf("want UI output then a terminal reset, got %q", out)
	}
	s.mu.Lock()
	gone := s.client == nil
	s.mu.Unlock()
	if !gone {
		t.Errorf("client still attached after a detach request")
	}

	// A new client attaches to the same, still running UI.
	srv2, cli2 := testConn(t)
	if err := s.attach(srv2, 80, 24, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out, ok = readFramesUntil(t, cli2, func(typ byte, p []byte) bool {
		return typ == frameOutput && strings.Contains(string(p), "again")
	})
	if !ok {
		t.Fatalf("second client got no output from the same UI: %q", out)
	}
	if !strings.HasPrefix(out, "\x1b[?1049h") {
		t.Errorf("modes not replayed to the second client: %q", out)
	}
}

// TestServerSurvivesConnectionPanic: a panic while the server serves one
// connection writes a crash report; the server lives on, and the next
// client starts the UI and works as usual.
func TestServerSurvivesConnectionPanic(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	r := newRig(t, catUI)
	r.panicIn = "a session connection"
	c := r.ensure()
	// A status request: the handler answers, then panics.
	if err := writeFrame(c, frameStatus, nil); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	if typ, _, err := readFrame(c); err != nil || typ != frameStatus {
		t.Fatalf("status reply %q, %v", typ, err)
	}
	_ = c.Close()
	waitFor(t, "the crash report", func() bool {
		m, _ := filepath.Glob(filepath.Join(state, "termocode", "crash", "crash-*.txt"))
		return len(m) > 0
	})
	c2 := r.ensure()
	hello(t, c2)
	echo(t, c2, "still-here")
	reports, _ := filepath.Glob(filepath.Join(state, "termocode", "crash", "crash-*.txt"))
	if len(reports) != 1 {
		t.Fatalf("crash reports %v, want one", reports)
	}
	// (The goroutine's name itself is masked: it is the value of
	// envPanicIn, an environment value.)
	if b, _ := os.ReadFile(reports[0]); !strings.Contains(string(b), "test panic in") {
		t.Errorf("report does not hold the panic:\n%s", b)
	}
}
