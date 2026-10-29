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

	"github.com/amin-jalali/termocode/internal/crash"
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

// AttachOpts is the terminal side of an attach.
type AttachOpts struct {
	In, Out *os.File
	// Caps (may be nil) describe this terminal; the UI switches to them
	// when it already runs.
	Caps *ClientCaps
	// Reconnect: when the server drops this client (it stopped reading,
	// e.g. a frozen SSH link) but still runs, keep the screen and attach
	// again by itself instead of leaving (setting auto_reconnect).
	Reconnect bool
	// Link: this client runs at the far end of `termocode ssh`; it
	// announces itself and answers its heartbeat (link.go).
	Link bool
}

// errOffline is a send while the client has no connection.
var errOffline = errors.New("session: offline")

// attachFrame is one frame read from the server.
type attachFrame struct {
	typ byte
	p   []byte
	err error
}

// readFrames reads the frames of c until an error, which is the last
// one sent. stop ends it early.
func readFrames(c *net.UnixConn, stop <-chan struct{}) <-chan attachFrame {
	ch := make(chan attachFrame, 16)
	crash.GoWith("the session reader", func() {
		for {
			typ, p, err := readFrame(c)
			select {
			case ch <- attachFrame{typ, p, err}:
			case <-stop:
				return
			}
			if err != nil {
				return
			}
		}
	}, func() { // end the attach as if the connection broke
		select {
		case ch <- attachFrame{err: errors.New("session reader failed")}:
		case <-stop:
		}
	})
	return ch
}

// Attach relays the terminal (o.In, o.Out) to the session of p over c
// until the UI exits (its exit code is returned), another client takes
// over, or this client is told to stop. The session keeps running in the
// last two cases.
func Attach(c *net.UnixConn, p Paths, o AttachOpts) int {
	in, caps := o.In, o.Caps
	// out is shared with the heartbeat answers of the key reader.
	out := &lockedWriter{w: o.Out}
	cols, rows, err := term.GetSize(int(o.Out.Fd()))
	if err != nil || cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	stop := make(chan struct{})
	defer close(stop)
	var wmu sync.Mutex
	conn := c // nil while offline
	defer func() {
		wmu.Lock()
		if conn != nil {
			_ = conn.Close()
		}
		wmu.Unlock()
	}()
	send := func(typ byte, p []byte) error {
		wmu.Lock()
		defer wmu.Unlock()
		if conn == nil {
			return errOffline
		}
		return writeFrame(conn, typ, p)
	}
	setConn := func(nc *net.UnixConn) {
		wmu.Lock()
		if conn != nil {
			_ = conn.Close()
		}
		conn = nc
		wmu.Unlock()
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

	if o.Link {
		_, _ = out.WriteString(linkHello)
	}

	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGWINCH, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(sigs)

	inDone := make(chan struct{})
	keys := make(chan []byte, 16)
	pings := newSeqFilter(linkPing)
	crash.Go("the session input", func() {
		defer close(inDone)
		buf := make([]byte, 4096)
		for {
			n, err := in.Read(buf)
			k := buf[:n]
			if o.Link && n > 0 {
				var found []int
				k, found = pings.Filter(k)
				for range found {
					_, _ = out.WriteString(linkPong)
				}
			}
			if len(k) > 0 {
				select {
				case keys <- append([]byte(nil), k...):
				case <-stop:
					return
				}
			}
			if err != nil {
				return
			}
		}
	})

	// seen follows the UI's output like the server does, so a client that
	// leaves without the server's reset (killed, server gone) can still
	// undo what plainReset cannot know about (the font size).
	seen := NewModes()
	frames := readFrames(c, stop)

	// Offline state: since when, the next attempt, its timer, and whether
	// keys were dropped.
	var (
		offline  bool
		since    time.Time
		dropped  bool
		attempt  int
		retry    <-chan time.Time
		unicode  = clientUnicode(caps)
		redrawLn = func() { _, _ = out.WriteString(offlineLine(attempt, cols, rows, unicode)) }
	)
	goOffline := func() {
		setConn(nil)
		frames = nil
		offline, attempt, since, dropped = true, 1, time.Now(), false
		redrawLn()
		retry = time.After(0)
	}

	for {
		select {
		case <-inDone:
			// No more keys (the terminal is gone): leave, the session stays.
			return 0
		case k := <-keys:
			if offline {
				if offlineQuit(k) {
					_, _ = out.Write(append(seen.Reset(), plainReset+msgDetached+"\r\n"...))
					return 0
				}
				dropped = dropped || offlineKeys(k)
				continue
			}
			_ = send(frameInput, k) // a dead link shows up as a read error
		case s := <-sigs:
			switch s {
			case syscall.SIGWINCH:
				if w, h, err := term.GetSize(int(o.Out.Fd())); err == nil {
					cols, rows = w, h
					if offline {
						redrawLn()
					}
					_ = send(frameResize, encodeSize(w, h))
				}
			case syscall.SIGHUP:
				return 0 // the terminal is gone; nothing to restore
			default:
				_, _ = out.Write(append(seen.Reset(), plainReset...))
				return 0
			}
		case <-retry:
			retry = nil
			if !locked(p.Lock) {
				_, _ = out.Write(append(seen.Reset(), plainReset+lostMessage(p)+"\r\n"...))
				return 1
			}
			nc, err := Dial(p)
			if err == nil {
				err = writeFrame(nc, frameHello, encodeHello(cols, rows, withNote(caps, ReconnectNote(time.Since(since), dropped))))
				if err != nil {
					_ = nc.Close()
				}
			}
			if err != nil {
				attempt++
				redrawLn()
				retry = time.After(reconnectDelay(attempt - 1))
				continue
			}
			setConn(nc)
			frames = readFrames(nc, stop)
			offline = false
		case f := <-frames:
			if f.err != nil {
				if o.Reconnect && locked(p.Lock) {
					goOffline()
					continue
				}
				_, _ = out.Write(append(seen.Reset(), plainReset+lostMessage(p)+"\r\n"...))
				return 1
			}
			switch f.typ {
			case frameOutput:
				seen.Feed(f.p)
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

// withNote is a copy of caps with a note for the user. Without caps (an
// old caller) there is nothing to carry it: nil caps keep the UI's own.
func withNote(caps *ClientCaps, note string) *ClientCaps {
	if caps == nil {
		return nil
	}
	c := *caps
	c.Note = note
	return &c
}

// lockedWriter serializes writes to the client terminal.
type lockedWriter struct {
	mu sync.Mutex
	w  *os.File
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (l *lockedWriter) WriteString(s string) (int, error) { return l.Write([]byte(s)) }

// clientUnicode reports whether the client's terminal takes UTF-8 (its
// locale says so; no caps: assume it does).
func clientUnicode(caps *ClientCaps) bool {
	if caps == nil {
		return true
	}
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := caps.Env[k]; v != "" {
			v = strings.ToUpper(v)
			return strings.Contains(v, "UTF-8") || strings.Contains(v, "UTF8")
		}
	}
	return false
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
