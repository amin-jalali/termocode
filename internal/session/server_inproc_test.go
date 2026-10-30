//go:build unix

package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// inprocServer is a Server in the test process with sh -c script as its
// UI. Its files live in a short temp dir.
func inprocServer(t *testing.T, script string) *Server {
	t.Helper()
	dir, err := os.MkdirTemp("", "tcs")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		paths: Paths{Workspace: dir, Info: filepath.Join(dir, "i.json"), Caps: filepath.Join(dir, "c.json")},
		uiCmd: func() *exec.Cmd { return exec.Command("sh", "-c", script) },
		modes: NewModes(), started: time.Now(), done: make(chan int, 1),
	}
	t.Cleanup(func() {
		s.mu.Lock()
		if s.cmd != nil && s.cmd.Process != nil {
			_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		}
		s.mu.Unlock()
		_ = os.RemoveAll(dir)
	})
	return s
}

// served runs s.handle on a fresh connection and returns the client end
// and a channel closed when handle returns.
func served(t *testing.T, s *Server) (*net.UnixConn, <-chan struct{}) {
	t.Helper()
	srv, cli := testConn(t)
	done := make(chan struct{})
	go func() { s.handle(srv); close(done) }()
	return cli, done
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("handle did not return")
	}
}

func TestHandleStatus(t *testing.T) {
	s := inprocServer(t, "exit 0")
	cli, done := served(t, s)
	if err := writeFrame(cli, frameStatus, nil); err != nil {
		t.Fatal(err)
	}
	typ, b, err := readFrame(cli)
	if err != nil || typ != frameStatus {
		t.Fatalf("reply %q %v", typ, err)
	}
	var in Info
	if err := json.Unmarshal(b, &in); err != nil {
		t.Fatal(err)
	}
	if in.Workspace != s.paths.Workspace || in.PID != os.Getpid() || in.Attached || in.UIPID != 0 {
		t.Fatalf("info %+v", in)
	}
	waitDone(t, done)
}

// Connections that are not a proper client are closed and do nothing.
func TestHandleRefusesOddClients(t *testing.T) {
	cases := map[string]func(c *net.UnixConn){
		"hangs up at once": func(c *net.UnixConn) { _ = c.Close() },
		"unknown frame":    func(c *net.UnixConn) { _ = writeFrame(c, 'z', nil) },
		"short hello":      func(c *net.UnixConn) { _ = writeFrame(c, frameHello, []byte{1}) },
		"frame too big": func(c *net.UnixConn) {
			_, _ = c.Write([]byte{frameHello, 0xff, 0xff, 0xff, 0xff})
		},
	}
	for name, send := range cases {
		t.Run(name, func(t *testing.T) {
			s := inprocServer(t, "exit 0")
			cli, done := served(t, s)
			send(cli)
			waitDone(t, done)
			if s.cmd != nil {
				t.Fatal("the UI was started")
			}
			_ = cli.SetReadDeadline(time.Now().Add(5 * time.Second))
			if _, _, err := readFrame(cli); err == nil {
				t.Fatal("the server answered")
			}
		})
	}
}

func TestHandleKillBeforeUI(t *testing.T) {
	s := inprocServer(t, "exit 0")
	cli, done := served(t, s)
	if err := writeFrame(cli, frameKill, nil); err != nil {
		t.Fatal(err)
	}
	waitDone(t, done)
	select {
	case code := <-s.done:
		if code != 0 {
			t.Fatalf("code %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the session did not end")
	}
}

// A connection from another user is refused (here: a uid that is not
// ours), and a connection that is gone has no credentials.
func TestCheckPeerRefusesOtherUID(t *testing.T) {
	srv, cli := testConn(t)
	err := checkPeer(srv, os.Getuid()+1)
	if err == nil || !strings.Contains(err.Error(), "peer uid") {
		t.Fatalf("err = %v", err)
	}
	if err := CheckPeer(cli); err != nil {
		t.Fatalf("own uid refused: %v", err)
	}
	_ = srv.Close()
	if err := checkPeer(srv, os.Getuid()); err == nil || !strings.Contains(err.Error(), "peer credentials") {
		t.Fatalf("closed conn: err = %v", err)
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if err := CheckPeer(a); err == nil {
		t.Fatal("a non-unix conn: want an error")
	}
}

func hello2(t *testing.T, c *net.UnixConn, caps *ClientCaps) {
	t.Helper()
	if err := writeFrame(c, frameHello, encodeHello(80, 24, caps)); err != nil {
		t.Fatal(err)
	}
}

func TestHandleAttachRefusedWhileEnding(t *testing.T) {
	s := inprocServer(t, "exit 0")
	s.exiting = true
	cli, done := served(t, s)
	hello2(t, cli, nil)
	var msg string
	if _, ok := readFramesUntil(t, cli, func(typ byte, p []byte) bool { msg = string(p); return typ == frameDetached }); !ok {
		t.Fatal("no detached frame")
	}
	if !strings.Contains(msg, "session is ending") {
		t.Fatalf("message %q", msg)
	}
	waitDone(t, done)
}

func TestHandleAttachUIDoesNotStart(t *testing.T) {
	s := inprocServer(t, "")
	s.uiCmd = func() *exec.Cmd { return exec.Command(filepath.Join(s.paths.Workspace, "no-such-binary")) }
	cli, done := served(t, s)
	hello2(t, cli, nil)
	var msg string
	if _, ok := readFramesUntil(t, cli, func(typ byte, p []byte) bool { msg = string(p); return typ == frameDetached }); !ok {
		t.Fatal("no detached frame")
	}
	if !strings.HasPrefix(msg, "termocode: start UI") {
		t.Fatalf("message %q", msg)
	}
	waitDone(t, done)
}

// collect reads output frames of c in the background.
type collector struct {
	out    chan string
	frames chan byte
}

func collect(c *net.UnixConn) *collector {
	col := &collector{out: make(chan string, 1024), frames: make(chan byte, 1024)}
	go func() {
		for {
			typ, p, err := readFrame(c)
			if err != nil {
				close(col.out)
				return
			}
			col.frames <- typ
			if typ == frameOutput {
				col.out <- string(p)
			}
		}
	}()
	return col
}

func (col *collector) until(t *testing.T, want string) string {
	t.Helper()
	var b strings.Builder
	deadline := time.After(10 * time.Second)
	for !strings.Contains(b.String(), want) {
		select {
		case s, ok := <-col.out:
			if !ok {
				t.Fatalf("connection ended before %q; got %q", want, b.String())
			}
			b.WriteString(s)
		case <-deadline:
			t.Fatalf("no %q; got %q", want, b.String())
		}
	}
	return b.String()
}

// Two clients: the second takes over. The first is told why; the second
// gets nothing the UI drew before it switched to the new terminal's caps.
func TestTakeoverWaitsForTheUIMark(t *testing.T) {
	s := inprocServer(t, `trap 'printf stale-frame; printf "\033_termocode:attached;2\033\\\\"; printf fresh-frame' USR1; `+
		`printf '\033[?1049hui-up'; while :; do sleep 0.02; done`)
	first, done1 := served(t, s)
	hello2(t, first, &ClientCaps{Env: map[string]string{"TERM": "xterm-a"}})
	c1 := collect(first)
	c1.until(t, "ui-up")
	if got, err := ReadCaps(s.paths.Caps); err != nil || got == nil || got.Gen != 1 {
		t.Fatalf("caps after the first attach: %+v %v", got, err)
	}

	second, _ := served(t, s)
	hello2(t, second, &ClientCaps{Env: map[string]string{"TERM": "xterm-b"}})
	c2 := collect(second)
	out := c2.until(t, "fresh-frame")
	if strings.Contains(out, "stale-frame") || strings.Contains(out, "termocode:attached") {
		t.Fatalf("output from before the switch reached the new client: %q", out)
	}
	if !strings.HasPrefix(out, "\x1b[?1049h") || !strings.Contains(out, "\x1b[?1049h\x1b[H") {
		t.Fatalf("want the modes replayed and the cursor home before the frame: %q", out)
	}
	waitDone(t, done1)
	gotDetached := false
	for len(c1.frames) > 0 {
		if <-c1.frames == frameDetached {
			gotDetached = true
		}
	}
	if !gotDetached {
		t.Fatal("the first client was not told it was taken over")
	}
	if got, _ := ReadCaps(s.paths.Caps); got == nil || got.Gen != 2 || got.Env["TERM"] != "xterm-b" {
		t.Fatalf("caps after takeover: %+v", got)
	}
}

// The UI never marks the switch: after holdTimeout the client gets the
// output anyway and the UI is asked once more to redraw.
func TestTakeoverHoldExpires(t *testing.T) {
	old := holdTimeout
	holdTimeout = 100 * time.Millisecond
	t.Cleanup(func() { holdTimeout = old })
	s := inprocServer(t, `n=0; trap 'n=$((n+1)); echo painted-$n' USR1; echo ui-up; while :; do sleep 0.02; done`)
	first, _ := served(t, s)
	hello2(t, first, &ClientCaps{})
	collect(first).until(t, "ui-up")
	second, _ := served(t, s)
	start := time.Now()
	hello2(t, second, &ClientCaps{})
	// The first paint the second client sees must come after the hold.
	// Usually it is painted-2 (painted-1 was dropped during the hold); on
	// a slow machine (macOS CI) the attach's signal and the hold's merge
	// and the only paint is painted-1, still after the hold.
	collect(second).until(t, "painted-")
	if time.Since(start) < holdTimeout {
		t.Fatal("output before the hold timed out")
	}
	// An old timer for an attach that is not held any more does nothing.
	s.holdExpired(99)
}

// A resize from the client reaches the UI's terminal; a broken resize
// frame is ignored; a paste far bigger than a terminal line arrives whole.
func TestClientResizeAndHugePaste(t *testing.T) {
	s := inprocServer(t, `trap '' USR1; stty raw -echo; echo ui-up; `+
		`while :; do c=$(dd bs=1 count=1 2>/dev/null); case "$c" in s) stty size;; p) head -c 300000 | wc -c;; esac; done`)
	cli, _ := served(t, s)
	hello2(t, cli, nil)
	col := collect(cli)
	col.until(t, "ui-up")
	if err := writeFrame(cli, frameResize, []byte{1}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ { // a resize storm; the last one counts
		if err := writeFrame(cli, frameResize, encodeSize(100+i, 30+i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeFrame(cli, frameInput, []byte("s")); err != nil {
		t.Fatal(err)
	}
	col.until(t, "59 129")
	if err := writeFrame(cli, frameInput, []byte("p")); err != nil {
		t.Fatal(err)
	}
	paste := bytes.Repeat([]byte("0123456789"), 30000)
	for len(paste) > 0 {
		n := min(len(paste), 64<<10)
		if err := writeFrame(cli, frameInput, paste[:n]); err != nil {
			t.Fatal(err)
		}
		paste = paste[n:]
	}
	col.until(t, "300000")
}

// `termocode kill` with a UI: the UI gets SIGTERM (to save its work) and
// its exit code ends the session; the client gets it too.
func TestTerminateWithUI(t *testing.T) {
	s := inprocServer(t, `trap 'echo saving; exit 5' TERM; trap '' USR1; echo ui-up; while :; do sleep 0.02; done`)
	cli, _ := served(t, s)
	hello2(t, cli, nil)
	col := collect(cli)
	col.until(t, "ui-up")
	s.terminate()
	col.until(t, "saving")
	select {
	case code := <-s.done:
		if code != 5 {
			t.Fatalf("session code %d, want 5", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not end")
	}
	// The session can end before the client's reader has the exit frame
	// off the socket: wait for it, do not only look at what came so far.
	deadline := time.After(10 * time.Second)
	for gotExit := false; !gotExit; {
		select {
		case typ := <-col.frames:
			gotExit = typ == frameExit
		case <-deadline:
			t.Fatal("the client got no exit frame")
		}
	}
}

func TestProtoLimits(t *testing.T) {
	if err := writeFrame(io.Discard, frameInput, make([]byte, maxFrame+1)); !errors.Is(err, errFrameTooBig) {
		t.Fatalf("err = %v", err)
	}
	if got := decodeCode([]byte{1}); got != 1 {
		t.Fatalf("short exit code: %d, want 1", got)
	}
	if got := decodeCode(encodeCode(-1)); got != -1 {
		t.Fatalf("code -1: %d", got)
	}
}

// locked is false for a lock file nobody holds (a server that crashed
// left it behind), and true while a server holds it.
func TestLockedStaleFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.lock")
	if locked(p) {
		t.Fatal("missing file: locked")
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if locked(p) {
		t.Fatal("a lock file nobody holds: locked")
	}
	f, err := lockFile(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if !locked(p) {
		t.Fatal("held lock: not locked")
	}
	if _, err := lockFile(p); err == nil {
		t.Fatal("a second lock: want an error")
	}
	if _, err := flockPath(filepath.Join(dir, "no", "such"), true); err == nil {
		t.Fatal("lock in a missing dir: want an error")
	}
}

func TestServeRefusesBadPlaces(t *testing.T) {
	// No session dir can be made.
	rt := shortRuntime(t)
	if err := os.WriteFile(filepath.Join(rt, "termocode"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := Serve(t.TempDir(), func() *exec.Cmd { t.Error("UI started"); return nil }); code != 1 {
		t.Fatalf("code %d, want 1", code)
	}
	// The socket cannot be made: a folder is in its place.
	shortRuntime(t)
	ws := t.TempDir()
	p, err := PathsFor(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(p.Socket, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if code := Serve(ws, func() *exec.Cmd { t.Error("UI started"); return nil }); code != 1 {
		t.Fatalf("code %d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(p.Socket, "x")); err != nil {
		t.Errorf("the folder in the socket's place was touched: %v", err)
	}
}
