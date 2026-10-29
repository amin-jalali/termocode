//go:build unix

package session

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// guardSignals keeps a stray hangup or terminate signal from ending the
// test binary while a test sends them to itself.
func guardSignals(t *testing.T) {
	t.Helper()
	sink := make(chan os.Signal, 8)
	signal.Notify(sink, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	t.Cleanup(func() { signal.Stop(sink) })
}

// serveOutput is a fake server step: send out, then wait for the client
// to go (reading its frames into got).
func serveOutput(out string, got chan<- []byte) func(c *net.UnixConn) {
	return func(c *net.UnixConn) {
		_ = writeFrame(c, frameOutput, []byte(out))
		for {
			typ, p, err := readFrame(c)
			if err != nil {
				close(got)
				return
			}
			got <- append([]byte{typ}, p...)
		}
	}
}

func waitOut(t *testing.T, path, want string) {
	t.Helper()
	waitFor(t, "output "+want, func() bool { return strings.Contains(readOut(path), want) })
}

// The terminal closes (the ssh session of the client ended): Attach
// leaves without a word and the session keeps running.
func TestAttachInputClosedLeaves(t *testing.T) {
	got := make(chan []byte, 16)
	s := newFakeServer(t, serveOutput("ready", got))
	inW, out, code := attachWith(t, s, AttachOpts{})
	waitOut(t, out, "ready")
	_ = inW.Close()
	if c := waitCode(t, code); c != 0 {
		t.Fatalf("code %d", c)
	}
}

// Keys go to the server as input frames; a detached frame ends the
// client with the server's message.
func TestAttachKeysAndDetachedMessage(t *testing.T) {
	keys := make(chan string, 4)
	s := newFakeServer(t, func(c *net.UnixConn) {
		_ = writeFrame(c, frameOutput, []byte("ready"))
		typ, p, err := readFrame(c)
		if err == nil && typ == frameInput {
			keys <- string(p)
		}
		_ = writeFrame(c, frameDetached, []byte(msgTakenOver))
	})
	inW, out, code := attachWith(t, s, AttachOpts{})
	waitOut(t, out, "ready")
	if _, err := inW.WriteString("ls\r"); err != nil {
		t.Fatal(err)
	}
	select {
	case k := <-keys:
		if k != "ls\r" {
			t.Errorf("server got %q", k)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no input frame")
	}
	if c := waitCode(t, code); c != 0 {
		t.Fatalf("code %d", c)
	}
	if o := readOut(out); !strings.Contains(o, msgTakenOver) || !strings.Contains(o, "\x1b[?1049l") {
		t.Fatalf("output %q: want the reset and the message", o)
	}
}

// SIGHUP (the terminal is gone) leaves at once; SIGTERM puts the
// terminal back first.
func TestAttachSignals(t *testing.T) {
	guardSignals(t)
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			got := make(chan []byte, 16)
			s := newFakeServer(t, serveOutput("\x1b[?1049hready", got))
			_, out, code := attachWith(t, s, AttachOpts{})
			waitOut(t, out, "ready") // the main loop runs: the signal is caught
			if err := syscall.Kill(os.Getpid(), sig); err != nil {
				t.Fatal(err)
			}
			if c := waitCode(t, code); c != 0 {
				t.Fatalf("code %d", c)
			}
			reset := strings.Contains(readOut(out), "\x1b[?1049l")
			if reset != (sig == syscall.SIGTERM) {
				t.Fatalf("reset written = %v for %v: %q", reset, sig, readOut(out))
			}
		})
	}
}

// openTerm is a pseudo terminal for a client: tty is the client's side.
// What the client writes is drained from the other side.
func openTerm(t *testing.T, cols, rows int) *os.File {
	t.Helper()
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	t.Cleanup(func() { _ = ptmx.Close(); _ = tty.Close() })
	if err := pty.Setsize(tty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := ptmx.Read(buf); err != nil {
				return
			}
		}
	}()
	return tty
}

// A window that is resized many times in a row: the server ends up with
// the last size, and the hello carried the first.
func TestAttachResizeStorm(t *testing.T) {
	guardSignals(t)
	tty := openTerm(t, 100, 30)
	sizes := make(chan [2]int, 256)
	s := newFakeServer(t, func(c *net.UnixConn) {
		_ = writeFrame(c, frameOutput, []byte("ready"))
		for {
			typ, p, err := readFrame(c)
			if err != nil {
				return
			}
			if typ == frameResize {
				cols, rows, _ := decodeSize(p)
				sizes <- [2]int{cols, rows}
			}
		}
	})
	c, err := Dial(s.p)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(s.p.Workspace, "out")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	code := make(chan int, 1)
	// Out is the tty (the size is read from it); output is checked
	// through the hello and the resize frames.
	go func() { code <- Attach(c, s.p, AttachOpts{In: tty, Out: tty}) }()
	h := <-s.hello
	if cols, rows, _ := decodeSize(h); cols != 100 || rows != 30 {
		t.Fatalf("hello size %dx%d, want 100x30", cols, rows)
	}
	waitFor(t, "the client loop", func() bool {
		// Each resize is caught once Attach listens; keep resizing
		// until one comes through.
		_ = pty.Setsize(tty, &pty.Winsize{Cols: 101, Rows: 31})
		_ = syscall.Kill(os.Getpid(), syscall.SIGWINCH)
		select {
		case <-sizes:
			return true
		case <-time.After(20 * time.Millisecond):
			return false
		}
	})
	for i := 0; i < 50; i++ {
		_ = pty.Setsize(tty, &pty.Winsize{Cols: uint16(60 + i), Rows: uint16(20 + i%5)})
		_ = syscall.Kill(os.Getpid(), syscall.SIGWINCH)
	}
	want := [2]int{109, 24}
	waitFor(t, "the last size", func() bool {
		for {
			select {
			case got := <-sizes:
				if got == want {
					return true
				}
			default:
				_ = syscall.Kill(os.Getpid(), syscall.SIGWINCH) // signals merge; ask again
				time.Sleep(5 * time.Millisecond)
				return false
			}
		}
	})
	_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
	if c := waitCode(t, code); c != 0 {
		t.Fatalf("code %d", c)
	}
}

// While offline (reconnecting), a resize redraws the offline line for the
// new width.
func TestAttachOfflineResizeRedraws(t *testing.T) {
	guardSignals(t)
	tty := openTerm(t, 80, 24)
	var s *fakeServer
	s = newFakeServer(t, func(c *net.UnixConn) {
		_ = s.ln.Close() // every reconnect fails
		_ = c.Close()
	})
	c, err := Dial(s.p)
	if err != nil {
		t.Fatal(err)
	}
	code := make(chan int, 1)
	// The test reads what the client draws through a second pty.
	ptmx, tty2, err := pty.Open()
	if err != nil {
		t.Skip(err)
	}
	defer ptmx.Close()
	defer tty2.Close()
	_ = pty.Setsize(tty2, &pty.Winsize{Cols: 80, Rows: 24})
	drawn := make(chan string, 64)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				drawn <- string(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { code <- Attach(c, s.p, AttachOpts{In: tty, Out: tty2, Reconnect: true}) }()
	var seen strings.Builder
	wait := func(want string) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for !strings.Contains(seen.String(), want) {
			select {
			case d := <-drawn:
				seen.WriteString(d)
			case <-deadline:
				t.Fatalf("no %q in %q", want, seen.String())
			}
		}
	}
	wait("Reconnecting")
	seen.Reset()
	_ = pty.Setsize(tty2, &pty.Winsize{Cols: 120, Rows: 40})
	_ = syscall.Kill(os.Getpid(), syscall.SIGWINCH)
	wait("\x1b[40;1H") // the line moved to the new last row
	_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
	waitCode(t, code)
}

// The server dies while the client is reconnecting: the client stops
// trying and says the session is gone.
func TestAttachServerCrashWhileOffline(t *testing.T) {
	var s *fakeServer
	s = newFakeServer(t, func(c *net.UnixConn) {
		_ = s.ln.Close()
		_ = c.Close()
	})
	lock := s.p.Lock
	_, out, code := attachWith(t, s, AttachOpts{Reconnect: true})
	waitOut(t, out, "Reconnecting")
	// The server is gone: its lock file goes with it (what locked()
	// sees after a server's cleanup).
	_ = os.Remove(lock)
	if c := waitCode(t, code); c != 1 {
		t.Fatalf("code %d, want 1", c)
	}
	if o := readOut(out); !strings.Contains(o, "lost the session server") {
		t.Fatalf("output %q", o)
	}
}

// A reconnect without caps (an old caller) carries no note.
func TestWithNoteNilCaps(t *testing.T) {
	if withNote(nil, "back") != nil {
		t.Fatal("nil caps must stay nil")
	}
	c := &ClientCaps{Env: map[string]string{"TERM": "x"}}
	if got := withNote(c, "back"); got.Note != "back" || c.Note != "" {
		t.Fatalf("got %+v, original %+v", got, c)
	}
}

func shortRuntime(t *testing.T) string {
	t.Helper()
	rt, err := os.MkdirTemp("", "tcs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(rt) })
	t.Setenv("XDG_RUNTIME_DIR", rt)
	return rt
}

// statusServer answers status requests with in until stop.
func statusServer(t *testing.T, sock string, in Info, onKill func()) {
	t.Helper()
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.AcceptUnix()
			if err != nil {
				return
			}
			typ, _, err := readFrame(c)
			switch {
			case err != nil:
			case typ == frameStatus:
				b, _ := json.Marshal(in)
				_ = writeFrame(c, frameStatus, b)
			case typ == frameKill && onKill != nil:
				onKill()
			}
			_ = c.Close()
		}
	}()
}

func TestListSessions(t *testing.T) {
	shortRuntime(t)
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	statusServer(t, filepath.Join(dir, "bbbb.sock"), Info{Workspace: "/w/new", Started: now}, nil)
	statusServer(t, filepath.Join(dir, "aaaa.sock"), Info{Workspace: "/w/old", Started: now.Add(-time.Hour)}, nil)
	// A socket a crashed server left behind.
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, "cccc.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	_ = stale.Close()

	list, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Workspace != "/w/old" || list[1].Workspace != "/w/new" {
		t.Fatalf("list %+v, want old then new", list)
	}
	if _, err := os.Stat(filepath.Join(dir, "cccc.sock")); err == nil {
		t.Error("stale socket not removed")
	}
}

func TestListNoSessionDir(t *testing.T) {
	rt := shortRuntime(t)
	if err := os.WriteFile(filepath.Join(rt, "termocode"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := List(); err == nil {
		t.Fatal("a file where the dir goes: want an error")
	}
}

func TestStatusOddServers(t *testing.T) {
	dir, err := os.MkdirTemp("", "tcs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	p := Paths{Socket: filepath.Join(dir, "s.sock"), Lock: filepath.Join(dir, "s.lock")}
	if _, err := Status(p); !errors.Is(err, ErrNoSession) {
		t.Fatalf("no server: %v", err)
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: p.Socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	replies := []func(c *net.UnixConn){
		func(c *net.UnixConn) { _ = writeFrame(c, frameOutput, []byte("hi")) }, // an odd reply
		func(c *net.UnixConn) {}, // hangs up
		func(c *net.UnixConn) { _ = writeFrame(c, frameStatus, []byte("{bad")) }, // bad JSON
	}
	go func() {
		for _, r := range replies {
			c, err := ln.AcceptUnix()
			if err != nil {
				return
			}
			_, _, _ = readFrame(c)
			r(c)
			_ = c.Close()
		}
	}()
	for i, want := range []string{"unexpected reply", "EOF", "invalid character"} {
		if _, err := Status(p); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("reply %d: err = %v, want %q", i, err, want)
		}
	}
}

func TestKillWaitsForTheSocketToGo(t *testing.T) {
	dir, err := os.MkdirTemp("", "tcs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	p := Paths{Socket: filepath.Join(dir, "s.sock"), Lock: filepath.Join(dir, "s.lock")}
	if err := Kill(p); !errors.Is(err, ErrNoSession) {
		t.Fatalf("no session: %v", err)
	}
	killed := make(chan struct{})
	statusServer(t, p.Socket, Info{}, func() {
		close(killed)
		go func() {
			time.Sleep(100 * time.Millisecond) // the session takes a moment to end
			_ = os.Remove(p.Socket)
		}()
	})
	if err := Kill(p); err != nil {
		t.Fatal(err)
	}
	select {
	case <-killed:
	default:
		t.Fatal("the server got no kill")
	}
	if _, err := os.Stat(p.Socket); err == nil {
		t.Fatal("Kill returned before the session ended")
	}
}

func TestEnsureErrors(t *testing.T) {
	dir, err := os.MkdirTemp("", "tcs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	p := Paths{Socket: filepath.Join(dir, "s.sock"), Lock: filepath.Join(dir, "s.lock"), Log: filepath.Join(dir, "s.log")}
	_, err = Ensure(p, func() error { return errors.New("no binary") })
	if err == nil || !strings.Contains(err.Error(), "start session server: no binary") {
		t.Fatalf("err = %v", err)
	}
	// A socket path the system cannot use is reported as it is.
	long := Paths{Socket: filepath.Join(dir, strings.Repeat("x", 200)+".sock"), Lock: p.Lock}
	if _, err := Ensure(long, func() error { t.Error("spawned"); return nil }); err == nil || errors.Is(err, ErrNoSession) {
		t.Fatalf("long path: err = %v", err)
	}
}
