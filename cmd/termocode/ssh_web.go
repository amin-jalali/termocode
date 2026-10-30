//go:build unix

package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/webgw"
)

// The local side of `termocode ssh --web` (WEB-29). The far termocode
// hands over the port, its gateway's unix socket and a one-time secret
// (session.WebHandoff); here a second ssh with the user's own arguments
// forwards 127.0.0.1:<port> to that socket (no port opens on the far
// host), and the browser on this computer opens the link through a 0600
// page, never through argv. The forward is started again when it dies
// (at most webFwdTries times in a row); what went wrong is said after
// termocode ssh leaves the terminal.

// msgWebFwdFailed is E7.
const msgWebFwdFailed = "termocode: SSH could not forward the web panel. The server may block it " +
	"(AllowStreamLocalForwarding), or ssh needs a password here: use a key or ssh-agent."

// webFwdTries is how often a dying forward is started again in a row.
const webFwdTries = 3

// webFwdReady is how long the first link waits for the forward.
const webFwdReady = 15 * time.Second

// webFwdProc is a running forward ssh.
type webFwdProc interface {
	Wait() error
	Kill()
}

// webForward runs the forward for one `termocode ssh --web`.
type webForward struct {
	ssh  string
	args []string
	dir  string // a private folder for the redirect page

	// Swapped in tests.
	start   func(name string, argv []string) (webFwdProc, error)
	open    func(path string) error
	ready   func(port int, d time.Duration) bool
	backoff func(try int) time.Duration

	queue chan session.WebHandoff
	done  chan struct{}

	mu     sync.Mutex
	key    string     // port and socket of the forward wanted
	proc   webFwdProc // nil while none runs
	opened bool
	closed bool
	notes  []string
	pages  []string
}

func newWebForward(ssh string, args []string, dir string) *webForward {
	f := &webForward{ssh: ssh, args: args, dir: dir,
		start: startFwdProc, open: openInBrowser, ready: portAnswers,
		backoff: func(try int) time.Duration { return time.Duration(try) * time.Second },
		queue:   make(chan session.WebHandoff, 8), done: make(chan struct{})}
	crash.Go("termocode ssh --web: the hand-offs", f.run)
	return f
}

// Handoff takes a hand-off from the far side; it never blocks for long
// (RunSSH calls it from its output loop).
func (f *webForward) Handoff(h session.WebHandoff) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	select {
	case f.queue <- h:
	default: // a burst of reconnects: the next one comes again
	}
}

// run handles the hand-offs in order.
func (f *webForward) run() {
	defer close(f.done)
	for h := range f.queue {
		f.handle(h)
	}
}

func (f *webForward) handle(h session.WebHandoff) {
	if h.Err != "" {
		f.note("termocode: no web panel: " + h.Err)
		return
	}
	key := strconv.Itoa(h.Port) + " " + h.Sock
	f.mu.Lock()
	if f.key != key || f.proc == nil {
		if f.proc != nil {
			f.proc.Kill()
			f.proc = nil
		}
		f.key = key
		f.superviseLocked(key, session.SSHForwardArgv(f.args, h.Port, h.Sock))
	}
	first := !f.opened
	f.opened = true
	f.mu.Unlock()
	if !first {
		return // a reconnect: the open tab comes back by itself
	}
	if !f.ready(h.Port, webFwdReady) {
		f.mu.Lock()
		f.opened = false
		f.mu.Unlock()
		return // the supervisor says why
	}
	path := filepath.Join(f.dir, "ssh-"+strconv.Itoa(h.Port)+".web-open.html")
	if err := webgw.WriteRedirect(path, webgw.LaunchURL(h.Port, h.Secret)); err != nil {
		f.note("termocode: could not open the web panel: " + err.Error())
		return
	}
	f.mu.Lock()
	f.pages = append(f.pages, path)
	f.mu.Unlock()
	time.AfterFunc(time.Minute, func() { _ = os.Remove(path) }) // the link is dead by then
	if err := f.open(path); err != nil {
		f.note("termocode: could not open a browser here. In termocode, use Web: Copy Link and open it in this computer's browser.")
	}
}

// superviseLocked starts the forward and starts it again when it dies,
// webFwdTries times in a row at most. f.mu is held.
func (f *webForward) superviseLocked(key string, argv []string) {
	start := func() webFwdProc {
		p, err := f.start(f.ssh, argv)
		if err != nil {
			return nil
		}
		return p
	}
	p := start()
	f.proc = p
	crash.Go("termocode ssh --web: the forward", func() {
		for try := 1; ; try++ {
			if p != nil {
				_ = p.Wait()
			}
			f.mu.Lock()
			if f.closed || f.key != key || f.proc != p {
				f.mu.Unlock()
				return
			}
			f.proc = nil
			if try > webFwdTries {
				f.notes = append(f.notes, msgWebFwdFailed)
				f.mu.Unlock()
				return
			}
			f.mu.Unlock()
			time.Sleep(f.backoff(try))
			f.mu.Lock()
			if f.closed || f.key != key || f.proc != nil {
				f.mu.Unlock()
				return
			}
			p = start()
			f.proc = p
			f.mu.Unlock()
		}
	})
}

func (f *webForward) note(s string) {
	f.mu.Lock()
	f.notes = append(f.notes, s)
	f.mu.Unlock()
}

// Close stops the forward, removes the redirect page and returns what to
// tell the user (termocode ssh prints it after the terminal is back).
func (f *webForward) Close() []string {
	f.mu.Lock()
	if !f.closed {
		f.closed = true
		close(f.queue)
	}
	f.mu.Unlock()
	<-f.done
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.proc != nil {
		f.proc.Kill()
		f.proc = nil
	}
	for _, p := range f.pages {
		_ = os.Remove(p)
	}
	return f.notes
}

// cmdProc is a forward ssh process.
type cmdProc struct{ c *exec.Cmd }

func (p cmdProc) Wait() error { return p.c.Wait() }
func (p cmdProc) Kill()       { _ = p.c.Process.Kill() }

// startFwdProc starts the forward ssh with no terminal, in its own
// process group (the terminal's keys and signals stay with the main ssh).
func startFwdProc(name string, argv []string) (webFwdProc, error) {
	c := spawn.Command(name, argv...)
	c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := c.Start(); err != nil {
		return nil, err
	}
	return cmdProc{c}, nil
}

// openInBrowser opens a local page in the default browser.
func openInBrowser(path string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	c := spawn.Command(name, path)
	if err := c.Start(); err != nil {
		return err
	}
	crash.Go("termocode ssh --web: the browser opener", func() { _ = c.Wait() })
	return nil
}

// portAnswers waits until 127.0.0.1:port accepts a connection.
func portAnswers(port int, d time.Duration) bool {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(d)
	for {
		if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			_ = c.Close()
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// freeLocalPort picks a free port on 127.0.0.1 for the forward.
func freeLocalPort() (int, error) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("no free local port: %w", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}
