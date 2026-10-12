//go:build unix

package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"
)

// ErrNoSession means no server runs for the workspace.
var ErrNoSession = errors.New("no session for this folder")

// plainReset puts a terminal back to normal when the client leaves without
// the server's help (killed, or the server died): mouse, paste and focus
// reports off, cursor shown, default shape and colour, main screen.
const plainReset = "\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?2004l\x1b[?1004l" +
	"\x1b[0 q\x1b]111\x07\x1b[?25h\x1b[?1049l"

// Dial connects to the session of a workspace. A socket whose server is
// gone is removed and reported as ErrNoSession.
func Dial(p Paths) (*net.UnixConn, error) {
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: p.Socket, Net: "unix"})
	if err == nil {
		return c, nil
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		removeStale(p)
		return nil, ErrNoSession
	}
	if errors.Is(err, syscall.ENOENT) {
		return nil, ErrNoSession
	}
	return nil, err
}

// removeStale deletes the files a dead server left behind (its log
// stays, for the crash). It holds the lock while it does, so it cannot
// remove the socket of a server that starts at the same moment; a live
// or starting server holds the lock and nothing is removed.
func removeStale(p Paths) {
	lock, err := lockFile(p.Lock)
	if err != nil {
		return
	}
	defer lock.Close()
	_ = os.Remove(p.Socket)
	_ = os.Remove(p.Info)
	_ = os.Remove(p.Caps)
	_ = os.Remove(p.Lock)
}

// Ensure connects to the workspace session, starting a server with spawn
// when none runs.
func Ensure(p Paths, spawn func() error) (*net.UnixConn, error) {
	spawned := false
	deadline := time.Now().Add(10 * time.Second)
	for {
		c, err := Dial(p)
		if err == nil {
			return c, nil
		}
		if !errors.Is(err, ErrNoSession) {
			return nil, err
		}
		// A server that holds the lock is starting (or ending): wait.
		// Once it is gone, start our own.
		if !spawned && !locked(p.Lock) {
			if err := spawn(); err != nil {
				return nil, fmt.Errorf("start session server: %w", err)
			}
			spawned = true
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("session server did not start (see %s): %w", p.Log, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Attach relays the terminal (in, out) to the session of p over c until
// the UI exits (its exit code is returned), another client takes over, or
// this client is told to stop. The session keeps running in the last
// two cases. caps (may be nil) describe this terminal; the UI switches to
// them when it already runs.
func Attach(c *net.UnixConn, p Paths, in, out *os.File, caps *ClientCaps) int {
	defer c.Close()
	cols, rows, err := term.GetSize(int(out.Fd()))
	if err != nil || cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	var wmu sync.Mutex
	send := func(typ byte, p []byte) error {
		wmu.Lock()
		defer wmu.Unlock()
		return writeFrame(c, typ, p)
	}
	if err := send(frameHello, encodeHello(cols, rows, caps)); err != nil {
		fmt.Fprintln(os.Stderr, "termocode:", err)
		return 1
	}
	if term.IsTerminal(int(in.Fd())) {
		if st, err := term.MakeRaw(int(in.Fd())); err == nil {
			defer func() { _ = term.Restore(int(in.Fd()), st) }()
		}
	}

	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGWINCH, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(sigs)

	inDone := make(chan struct{})
	go func() {
		defer close(inDone)
		buf := make([]byte, 4096)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				if send(frameInput, buf[:n]) != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	type frame struct {
		typ byte
		p   []byte
		err error
	}
	frames := make(chan frame, 16)
	go func() {
		for {
			typ, p, err := readFrame(c)
			frames <- frame{typ, p, err}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-inDone:
			// No more keys (the terminal is gone): leave, the session stays.
			return 0
		case s := <-sigs:
			switch s {
			case syscall.SIGWINCH:
				if w, h, err := term.GetSize(int(out.Fd())); err == nil {
					_ = send(frameResize, encodeSize(w, h))
				}
			case syscall.SIGHUP:
				return 0 // the terminal is gone; nothing to restore
			default:
				_, _ = out.WriteString(plainReset)
				return 0
			}
		case f := <-frames:
			if f.err != nil {
				_, _ = out.WriteString(plainReset + lostMessage(p) + "\r\n")
				return 1
			}
			switch f.typ {
			case frameOutput:
				_, _ = out.Write(f.p)
			case frameExit:
				return decodeCode(f.p)
			case frameDetached:
				_, _ = out.WriteString(plainReset + string(f.p) + "\r\n")
				return 0
			}
		}
	}
}

// lostMessage says why the connection to the server ended without a
// word: the server is gone, or it is alive and dropped us (it does that
// to a client that stops reading, e.g. a frozen SSH link).
func lostMessage(p Paths) string {
	if locked(p.Lock) {
		return "termocode: disconnected, the session keeps running. Run `termocode attach` to go back."
	}
	return "termocode: lost the session server"
}

// Status asks a running session for its Info.
func Status(p Paths) (Info, error) {
	c, err := Dial(p)
	if err != nil {
		return Info{}, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	if err := writeFrame(c, frameStatus, nil); err != nil {
		return Info{}, err
	}
	typ, b, err := readFrame(c)
	if err != nil {
		return Info{}, err
	}
	if typ != frameStatus {
		return Info{}, fmt.Errorf("unexpected reply %q", typ)
	}
	var in Info
	err = json.Unmarshal(b, &in)
	return in, err
}

// List returns the running sessions of this user, oldest first. Stale
// sockets found on the way are removed.
func List() ([]Info, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	socks, _ := filepath.Glob(filepath.Join(dir, "*.sock"))
	var out []Info
	for _, s := range socks {
		base := strings.TrimSuffix(s, ".sock")
		p := Paths{Key: filepath.Base(base), Socket: s, Lock: base + ".lock", Info: base + ".json", Log: base + ".log", Caps: base + ".caps.json"}
		in, err := Status(p)
		if err != nil {
			continue
		}
		out = append(out, in)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out, nil
}

// Kill ends the workspace session (like Quit, but unsaved buffers go to
// the hot-exit backup instead of a question) and waits for it to go.
func Kill(p Paths) error {
	c, err := Dial(p)
	if err != nil {
		return err
	}
	err = writeFrame(c, frameKill, nil)
	_ = c.Close()
	if err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(p.Socket); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("the session did not end in time")
}
