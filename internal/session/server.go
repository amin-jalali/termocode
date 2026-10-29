//go:build unix

package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"

	"github.com/amin-jalali/termocode/internal/crash"
)

// RepaintSignal asks the UI inside the session to redraw everything after
// a client attached (cmd/termocode handles it).
const RepaintSignal = syscall.SIGUSR1

// clientWriteTimeout drops a client whose connection stopped reading (a
// frozen SSH link) instead of letting the UI block on its output. A
// variable so tests can make it short.
var clientWriteTimeout = 5 * time.Second

// holdTimeout bounds how long a new client waits for the UI to switch to
// its caps (a UI that does not mark it, or is busy); then it gets the
// output as it comes and the UI is asked to redraw once more.
var holdTimeout = 2 * time.Second

// Messages a client prints when the server lets it go.
const (
	msgTakenOver = "termocode: detached, the session was attached from another terminal"
	msgDetached  = "termocode: detached, the session keeps running. Run `termocode attach` to go back."
)

// Info describes a running session (`termocode sessions`).
type Info struct {
	Workspace string    `json:"workspace"`
	PID       int       `json:"pid"`
	UIPID     int       `json:"ui_pid,omitempty"`
	Started   time.Time `json:"started"`
	Attached  bool      `json:"attached"`
}

// Server owns the PTY the UI runs in and relays it to one client at a
// time.
type Server struct {
	paths Paths
	// uiCmd builds the UI command; the server starts it on the PTY when
	// the first client says hello.
	uiCmd func() *exec.Cmd

	mu     sync.Mutex
	client *net.UnixConn
	cmd    *exec.Cmd
	ptmx   *os.File
	modes  *Modes
	detach detachFilter
	marks  attachedFilter
	// gen counts attaches with caps; while hold is not 0 the current
	// client gets no output until the UI marks its switch to the caps of
	// attach hold (AttachedSeq), or holdTimer fires.
	gen       int
	hold      int
	holdTimer *time.Timer
	started   time.Time
	exiting   bool
	done      chan int // UI exit code
}

// Serve runs a session server for the workspace until the UI exits. It
// returns the process exit code. A second server for the same workspace
// (two clients started at once) exits at once with 0.
func Serve(workspace string, uiCmd func() *exec.Cmd) int {
	p, err := PathsFor(workspace)
	if err != nil {
		log.Printf("session: %v", err)
		return 1
	}
	lock, err := lockFile(p.Lock)
	if err != nil {
		return 0 // another server owns this workspace
	}
	defer lock.Close()
	truncateOwnLog(p)
	// We hold the lock, so a socket left on disk is stale.
	_ = os.Remove(p.Socket)
	old := unix.Umask(0o177)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: p.Socket, Net: "unix"})
	unix.Umask(old)
	if err != nil {
		log.Printf("session: listen: %v", err)
		return 1
	}
	_ = os.Chmod(p.Socket, 0o600)
	ln.SetUnlinkOnClose(true)
	s := &Server{paths: p, uiCmd: uiCmd, modes: NewModes(), started: time.Now(), done: make(chan int, 1)}
	defer s.cleanup(ln)

	// No terminal of our own (setsid); SIGTERM ends the session the same
	// way `termocode kill` does.
	// Caught and dropped, not signal.Ignore: an ignored signal stays
	// ignored in the UI and everything it starts (a UI deaf to SIGHUP
	// outlives a dead server; SIGPIPE breaks pipes in its shells).
	drop := make(chan os.Signal, 1)
	signal.Notify(drop, syscall.SIGHUP, syscall.SIGINT, syscall.SIGPIPE)
	crash.Go("the session signal drain", func() {
		for range drop {
		}
	})
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)
	crash.Go("the session signal handler", func() {
		for range sigs {
			s.terminate()
		}
	})

	crash.Go("the session listener", func() { s.acceptLoop(ln) })
	code := <-s.done
	// Children the UI left behind (it leads its own process group on the
	// PTY) end with the session. nvim is not in that group (own session);
	// it ends when the UI dies (EOF of its embed pipe, Pdeathsig, its
	// watchdog: internal/nvim/watchdog.go).
	s.mu.Lock()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
	}
	s.mu.Unlock()
	return code
}

// cleanup removes the session files when the session ends normally.
// The lock goes too: we still hold it, and lockFile checks that the file
// it locked is still the one on disk. Only a crashed server leaves its
// log (and lock) behind.
func (s *Server) cleanup(ln *net.UnixListener) {
	s.mu.Lock()
	s.exiting = true // no more writeInfo
	_ = ln.Close()
	_ = os.Remove(s.paths.Socket)
	_ = os.Remove(s.paths.Info)
	_ = os.Remove(s.paths.Caps)
	_ = os.Remove(s.paths.Log)
	_ = os.Remove(s.paths.Lock)
	s.mu.Unlock()
}

func (s *Server) acceptLoop(ln *net.UnixListener) {
	for {
		c, err := ln.AcceptUnix()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("session: accept: %v", err)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		crash.GoWith("a session connection", func() { s.handle(c) }, func() { _ = c.Close() })
	}
}

func (s *Server) handle(c *net.UnixConn) {
	if err := checkPeer(c, os.Getuid()); err != nil {
		log.Printf("session: refused: %v", err)
		_ = c.Close()
		return
	}
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	typ, payload, err := readFrame(c)
	_ = c.SetReadDeadline(time.Time{})
	if err != nil {
		_ = c.Close()
		return
	}
	switch typ {
	case frameStatus:
		b, _ := json.Marshal(s.info())
		_ = writeFrame(c, frameStatus, b)
		_ = c.Close()
	case frameKill:
		s.terminate()
		_ = c.Close()
	case frameHello:
		cols, rows, caps, err := decodeHello(payload)
		if err != nil {
			_ = c.Close()
			return
		}
		if err := s.attach(c, cols, rows, caps); err != nil {
			log.Printf("session: attach: %v", err)
			_ = writeFrame(c, frameDetached, []byte("termocode: "+err.Error()))
			_ = c.Close()
			return
		}
		s.readClient(c)
	default:
		_ = c.Close()
	}
}

func (s *Server) info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.infoLocked()
}

func (s *Server) infoLocked() Info {
	in := Info{Workspace: s.paths.Workspace, PID: os.Getpid(), Started: s.started, Attached: s.client != nil}
	if s.cmd != nil && s.cmd.Process != nil {
		in.UIPID = s.cmd.Process.Pid
	}
	return in
}

// writeInfo writes the info file, unless the session is ending (a late
// write would leave the file behind after cleanup).
func (s *Server) writeInfo() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exiting {
		return
	}
	b, _ := json.MarshalIndent(s.infoLocked(), "", "  ")
	_ = os.WriteFile(s.paths.Info, b, 0o600)
}

// attach makes c the client. The first client starts the UI; a later one
// takes over from the current client, gets the terminal modes replayed
// and the UI redraws everything — with the caps of the new client's
// terminal, which wait in the caps file for the UI (nil caps, from an old
// client, remove it: the UI then keeps what it has).
func (s *Server) attach(c *net.UnixConn, cols, rows int, caps *ClientCaps) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exiting {
		return errors.New("session is ending")
	}
	if caps != nil {
		s.gen++
		caps.Gen = s.gen
	}
	if s.paths.Caps != "" {
		if err := writeCaps(s.paths.Caps, caps); err != nil {
			log.Printf("session: caps: %v", err)
		}
	}
	s.stopHoldLocked()
	size := &pty.Winsize{Cols: uint16(max(cols, 1)), Rows: uint16(max(rows, 1))}
	if s.cmd == nil {
		cmd := s.uiCmd()
		if s.paths.Caps != "" {
			if cmd.Env == nil {
				cmd.Env = os.Environ()
			}
			cmd.Env = append(cmd.Env, EnvCapsFile+"="+s.paths.Caps)
		}
		ptmx, err := pty.StartWithSize(cmd, size)
		if err != nil {
			return fmt.Errorf("start UI: %w", err)
		}
		s.cmd, s.ptmx, s.client = cmd, ptmx, c
		crash.GoWith("the session pump", s.pump, s.pumpFailed)
		crash.Go("the session info", s.writeInfo)
		return nil
	}
	s.dropLocked(msgTakenOver)
	s.client = c
	s.sendLocked(frameOutput, s.modes.Replay())
	_ = pty.Setsize(s.ptmx, size)
	if caps != nil && s.paths.Caps != "" {
		// Until the UI has switched to these caps, its output was drawn
		// for the old terminal: keep it from this one.
		s.hold = caps.Gen
		gen := caps.Gen
		s.holdTimer = time.AfterFunc(holdTimeout, func() { s.holdExpired(gen) })
	}
	// The size may not have changed, so no SIGWINCH: ask for the redraw.
	_ = s.cmd.Process.Signal(RepaintSignal)
	crash.Go("the session info", s.writeInfo)
	return nil
}

// holdBack feeds UI output to the mode tracker and returns the part the
// client may get: all of it, or during a hold only what follows the mark
// of the held attach (then the hold ends). Holds s.mu.
func (s *Server) holdBack(out []byte, marks []attachedMark) []byte {
	if s.hold == 0 {
		s.modes.Feed(out)
		return out
	}
	for _, m := range marks {
		if m.gen == s.hold {
			s.modes.Feed(out[:m.pos])
			s.releaseLocked()
			out = out[m.pos:]
			s.modes.Feed(out)
			return out
		}
	}
	s.modes.Feed(out)
	return nil
}

// stopHoldLocked ends a hold without a release. Holds s.mu.
func (s *Server) stopHoldLocked() {
	s.hold = 0
	if s.holdTimer != nil {
		s.holdTimer.Stop()
		s.holdTimer = nil
	}
}

// releaseLocked ends the hold: the client gets the terminal modes as they
// are now and the cursor home, where the UI's frames start. Holds s.mu.
func (s *Server) releaseLocked() {
	s.stopHoldLocked()
	s.sendLocked(frameOutput, append(s.modes.Replay(), "\x1b[H"...))
}

// holdExpired: the UI did not mark the switch in time. Send the output
// as it comes and ask for one more full redraw.
func (s *Server) holdExpired(gen int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hold != gen || s.exiting {
		return
	}
	log.Printf("session: the UI did not confirm attach %d; redrawing", gen)
	s.releaseLocked()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(RepaintSignal)
	}
}

// dropLocked lets the current client go: its terminal is put back to
// normal and it prints msg. The session keeps running. Holds s.mu.
func (s *Server) dropLocked(msg string) {
	old := s.client
	if old == nil {
		return
	}
	s.sendLocked(frameOutput, s.modes.Reset())
	s.sendLocked(frameDetached, []byte(msg))
	_ = old.Close()
	s.client = nil
	crash.Go("the session info", s.writeInfo)
}

// sendLocked writes a frame to the current client; a client that fails
// or stalls is dropped (the session stays). Holds s.mu.
func (s *Server) sendLocked(typ byte, p []byte) {
	if s.client == nil {
		return
	}
	_ = s.client.SetWriteDeadline(time.Now().Add(clientWriteTimeout))
	if err := writeFrame(s.client, typ, p); err != nil {
		_ = s.client.Close()
		s.client = nil
		crash.Go("the session info", s.writeInfo)
	}
}

// readClient relays one client's input until it disconnects.
func (s *Server) readClient(c *net.UnixConn) {
	defer func() {
		s.mu.Lock()
		if s.client == c {
			s.client = nil
			crash.Go("the session info", s.writeInfo)
		}
		s.mu.Unlock()
		_ = c.Close()
	}()
	for {
		typ, p, err := readFrame(c)
		if err != nil {
			return
		}
		s.mu.Lock()
		current := s.client == c
		ptmx := s.ptmx
		s.mu.Unlock()
		if !current {
			return
		}
		switch typ {
		case frameInput:
			_, _ = ptmx.Write(p)
		case frameResize:
			if cols, rows, err := decodeSize(p); err == nil {
				_ = pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(max(cols, 1)), Rows: uint16(max(rows, 1))})
			}
		}
	}
}

// pump relays UI output to the client (or drops it while detached; the
// UI redraws on the next attach) and ends the session when the UI exits.
// A DetachSeq in the output detaches the client.
func (s *Server) pump() {
	readDone := make(chan struct{})
	crash.Go("the session output", func() {
		defer close(readDone)
		buf := make([]byte, 32*1024)
		for {
			n, err := s.ptmx.Read(buf)
			if n > 0 {
				s.relay(buf[:n])
			}
			if err != nil {
				return
			}
		}
	})
	err := s.cmd.Wait()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		code = 1
	}
	// The last frame (terminal restore) may still be in the PTY. A child
	// that kept the PTY open must not hold the exit forever.
	select {
	case <-readDone:
	case <-time.After(time.Second):
	}
	s.mu.Lock()
	s.exiting = true
	s.sendLocked(frameExit, encodeCode(code))
	if s.client != nil {
		_ = s.client.Close()
		s.client = nil
	}
	s.mu.Unlock()
	_ = s.ptmx.Close()
	s.done <- code
}

// relay sends one read of UI output to the client. The lock is released
// by a defer: a panic here (recovered by crash.Go) must not leave the
// server locked.
func (s *Server) relay(b []byte) {
	out, detach := s.detach.Filter(b)
	out, marks := s.marks.Filter(out)
	s.mu.Lock()
	defer s.mu.Unlock()
	out = s.holdBack(out, marks)
	if len(out) > 0 {
		s.sendLocked(frameOutput, out)
	}
	if detach {
		s.dropLocked(msgDetached)
	}
}

// pumpFailed ends the session after a panic in pump: the UI gets SIGTERM
// (it saves its unsaved buffers to the hot-exit backup, see terminate)
// and a moment for it before Serve returns.
func (s *Server) pumpFailed() {
	s.terminate()
	time.AfterFunc(5*time.Second, func() {
		select {
		case s.done <- 1:
		default:
		}
	})
}

// terminate ends the session: the UI gets SIGTERM (it saves unsaved
// buffers to the hot-exit backup), then SIGKILL if it hangs.
func (s *Server) terminate() {
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		select {
		case s.done <- 0:
		default: // already ending (a second kill)
		}
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	time.AfterFunc(10*time.Second, func() { _ = cmd.Process.Kill() })
}

// lockFile takes an exclusive, non-blocking flock on path, creating the
// file. The lock lives as long as the returned file is open (and dies
// with the process).
func lockFile(path string) (*os.File, error) {
	return flockPath(path, true)
}

// flockPath locks path. The holder may remove the file before it lets go
// (cleanup), so a lock on a file that is no longer at path is worth
// nothing: try again. Without create, a missing file is os.ErrNotExist.
func flockPath(path string, create bool) (*os.File, error) {
	flag := os.O_RDWR
	if create {
		flag |= os.O_CREATE
	}
	for range 10 {
		f, err := os.OpenFile(path, flag, 0o600)
		if err != nil {
			return nil, err
		}
		if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			_ = f.Close()
			return nil, err
		}
		fi, ferr := f.Stat()
		pi, perr := os.Stat(path)
		if ferr == nil && perr == nil && os.SameFile(fi, pi) {
			return f, nil
		}
		_ = f.Close()
	}
	return nil, errors.New("session: the lock file keeps changing")
}

// locked reports whether a live server holds the workspace lock. It
// never creates the lock file.
func locked(path string) bool {
	f, err := flockPath(path, false)
	if err != nil {
		return !errors.Is(err, os.ErrNotExist)
	}
	_ = f.Close()
	return false
}

// OpenLog opens the server log for the server's output. It appends: a
// second server started at the same moment must not wipe the log of the
// one that runs. The server that gets the lock empties it (Serve).
func OpenLog(p Paths) (*os.File, error) {
	return os.OpenFile(p.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// truncateOwnLog empties the log when it is our stderr: a new session
// starts a new log.
func truncateOwnLog(p Paths) {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return
	}
	if li, err := os.Stat(p.Log); err == nil && os.SameFile(fi, li) {
		_ = os.Stderr.Truncate(0)
	}
}
