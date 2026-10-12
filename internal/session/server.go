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
)

// RepaintSignal asks the UI inside the session to redraw everything after
// a client attached (cmd/termocode handles it).
const RepaintSignal = syscall.SIGUSR1

// clientWriteTimeout drops a client whose connection stopped reading (a
// frozen SSH link) instead of letting the UI block on its output. A
// variable so tests can make it short.
var clientWriteTimeout = 5 * time.Second

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

	mu      sync.Mutex
	client  *net.UnixConn
	cmd     *exec.Cmd
	ptmx    *os.File
	modes   *Modes
	started time.Time
	exiting bool
	done    chan int // UI exit code
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
	signal.Ignore(syscall.SIGHUP, syscall.SIGINT, syscall.SIGPIPE)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)
	go func() {
		for range sigs {
			s.terminate()
		}
	}()

	go s.acceptLoop(ln)
	code := <-s.done
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
		go s.handle(c)
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
		cols, rows, err := decodeSize(payload)
		if err != nil {
			_ = c.Close()
			return
		}
		if err := s.attach(c, cols, rows); err != nil {
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
// and the UI redraws everything.
func (s *Server) attach(c *net.UnixConn, cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exiting {
		return errors.New("session is ending")
	}
	size := &pty.Winsize{Cols: uint16(max(cols, 1)), Rows: uint16(max(rows, 1))}
	if s.cmd == nil {
		cmd := s.uiCmd()
		ptmx, err := pty.StartWithSize(cmd, size)
		if err != nil {
			return fmt.Errorf("start UI: %w", err)
		}
		s.cmd, s.ptmx, s.client = cmd, ptmx, c
		go s.pump()
		go s.writeInfo()
		return nil
	}
	if old := s.client; old != nil {
		s.sendLocked(frameOutput, s.modes.Reset())
		s.sendLocked(frameDetached, []byte("termocode: detached, the session was attached from another terminal"))
		_ = old.Close()
	}
	s.client = c
	s.sendLocked(frameOutput, s.modes.Replay())
	_ = pty.Setsize(s.ptmx, size)
	// The size may not have changed, so no SIGWINCH: ask for the redraw.
	_ = s.cmd.Process.Signal(RepaintSignal)
	go s.writeInfo()
	return nil
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
		go s.writeInfo()
	}
}

// readClient relays one client's input until it disconnects.
func (s *Server) readClient(c *net.UnixConn) {
	defer func() {
		s.mu.Lock()
		if s.client == c {
			s.client = nil
			go s.writeInfo()
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
func (s *Server) pump() {
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 32*1024)
		for {
			n, err := s.ptmx.Read(buf)
			if n > 0 {
				s.mu.Lock()
				s.modes.Feed(buf[:n])
				s.sendLocked(frameOutput, buf[:n])
				s.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
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
	go func() {
		time.Sleep(10 * time.Second)
		_ = cmd.Process.Kill()
	}()
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
