package clilogin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// maxTail caps the CLI output a Session keeps (the newest part).
const maxTail = 64 << 10

// The pty is wide so a full-screen CLI does not break the link over
// lines (an Ink app wraps its text at the terminal's width).
const (
	ptyCols = 1000
	ptyRows = 50
)

// EventKind says what a Session saw.
type EventKind int

const (
	// EventURL: the sign-in link (Event.URL), sent once.
	EventURL EventKind = iota
	// EventCode: the one-time code to type on the link's page
	// (Event.Code; Codex's device sign-in), sent once.
	EventCode
	// EventNeedsCode: the CLI asks for a code from the browser page to be
	// pasted (Session.Write), sent once.
	EventNeedsCode
	// EventExit: the CLI ended (Event.Err nil on success; ErrCanceled
	// after Cancel; else an *Error). Always the last event.
	EventExit
)

// Event is one thing a Session saw.
type Event struct {
	Kind EventKind
	URL  string
	Code string
	Err  error
}

// ErrCanceled is the exit of a Session that Cancel (or its context)
// stopped.
var ErrCanceled = errors.New("sign-in canceled")

// Options are the settings of one sign-in run.
type Options struct {
	CLI string // ai.CLIClaude or ai.CLICodex
	// Env is the environment (nil: os.Environ() with termocode's tools/bin
	// first); clistep.CleanEnv is always applied.
	Env []string
	// DeviceAuth runs Codex's device sign-in (a link and a code to type,
	// no local callback: for SSH). Ignored for Claude Code.
	DeviceAuth bool
}

// Session is one running sign-in of a CLI under a pty.
type Session struct {
	cli   string
	title string
	cmd   *exec.Cmd
	pty   *os.File

	events chan Event
	done   chan struct{}
	read   chan struct{} // closed when the pty reader ends

	mu     sync.Mutex
	tail   []byte
	closed bool
	sent   map[EventKind]bool
	device bool

	killOnce sync.Once
	killed   atomic.Bool
}

// loginArgs are the CLI's own sign-in arguments.
func loginArgs(cli string, device bool) []string {
	switch cli {
	case ai.CLIClaude:
		return []string{"auth", "login"}
	case ai.CLICodex:
		if device {
			return []string{"login", "--device-auth"}
		}
		return []string{"login"}
	}
	return nil
}

// Start runs the CLI's sign-in under a pty. ctx ending kills it.
func Start(ctx context.Context, o Options) (*Session, error) {
	title := ai.CLITitle(o.CLI)
	if title == "" {
		return nil, fmt.Errorf("unknown CLI %q", o.CLI)
	}
	env := o.Env
	if env == nil {
		env = lspinstall.EnvWithToolsBin()
	}
	env = clistep.CleanEnv(env)
	if envGet(env, "TERM") == "" {
		env = append(env, "TERM=xterm-256color")
	}
	bin, err := lookPath(clistep.Bin(o.CLI), env)
	if err != nil {
		return nil, clistep.NotInstalled(o.CLI)
	}
	device := o.DeviceAuth && o.CLI == ai.CLICodex
	cmd := spawn.Command(bin, loginArgs(o.CLI, device)...)
	cmd.Env = env
	if home := envGet(env, "HOME"); home != "" {
		if fi, err := os.Stat(home); err == nil && fi.IsDir() {
			cmd.Dir = home
		}
	}
	f, err := startPty(cmd, ptyCols, ptyRows)
	if err != nil {
		return nil, fmt.Errorf("%s sign-in did not start: %w", title, err)
	}
	s := &Session{
		cli: o.CLI, title: title, cmd: cmd, pty: f,
		events: make(chan Event, 8), done: make(chan struct{}), read: make(chan struct{}),
		sent: map[EventKind]bool{}, device: device,
	}
	go s.readLoop()
	go s.waitLoop()
	go func() {
		select {
		case <-ctx.Done():
			s.Cancel()
		case <-s.done:
		}
	}()
	return s, nil
}

// Events are the session's events; the channel closes after EventExit.
func (s *Session) Events() <-chan Event { return s.events }

// Done is closed when the CLI has ended and the events are all sent.
func (s *Session) Done() <-chan struct{} { return s.done }

// Write sends a pasted code (one line) to the CLI, as if typed and
// Enter pressed. The code is never logged.
func (s *Session) Write(code string) error {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 4096 || strings.ContainsFunc(code, unicode.IsControl) {
		return errors.New("the code must be one line of text")
	}
	select {
	case <-s.done:
		return errors.New("the sign-in has ended")
	default:
	}
	if _, err := io.WriteString(s.pty, code+"\r"); err != nil {
		return errors.New("the sign-in has ended")
	}
	return nil
}

// Cancel kills the CLI and everything it started (its process group).
// Safe to call more than once and after the end.
func (s *Session) Cancel() {
	s.killOnce.Do(func() {
		s.killed.Store(true)
		if s.cmd.Process != nil {
			killGroup(s.cmd.Process.Pid)
		}
	})
}

func (s *Session) readLoop() {
	defer close(s.read)
	buf := make([]byte, 4096)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			s.feed(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// feed adds CLI output to the tail and sends what it now shows.
func (s *Session) feed(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.tail = append(s.tail, p...)
	if over := len(s.tail) - maxTail; over > 0 {
		s.tail = s.tail[over:]
		if i := bytes.IndexByte(s.tail, '\n'); i >= 0 {
			s.tail = s.tail[i+1:] // no half line (or half escape) at the start
		}
	}
	text := cleanText(string(s.tail))
	if !s.sent[EventURL] {
		if u := firstURL(text); u != "" {
			s.sendLocked(Event{Kind: EventURL, URL: u})
		}
	}
	if s.device && s.sent[EventURL] && !s.sent[EventCode] {
		if c := deviceCode(text); c != "" {
			s.sendLocked(Event{Kind: EventCode, Code: c})
		}
	}
	if s.cli == ai.CLIClaude && !s.sent[EventNeedsCode] && needsCode(text) {
		s.sendLocked(Event{Kind: EventNeedsCode})
	}
}

// sendLocked sends ev once per kind (the buffer holds every kind, so it
// never blocks). s.mu is held.
func (s *Session) sendLocked(ev Event) {
	s.sent[ev.Kind] = true
	s.events <- ev
}

func (s *Session) waitLoop() {
	werr := s.cmd.Wait()
	// The rest of the output, unless something the CLI started still
	// holds the pty.
	select {
	case <-s.read:
	case <-time.After(500 * time.Millisecond):
	}
	_ = s.pty.Close()
	<-s.read

	s.mu.Lock()
	text := cleanText(string(s.tail))
	ev := Event{Kind: EventExit, Err: s.exitErr(werr, text)}
	s.sent[EventExit] = true
	s.events <- ev
	s.closed = true
	close(s.events)
	s.mu.Unlock()
	close(s.done)
}

// exitErr turns the CLI's exit into nil, ErrCanceled or an *Error with a
// plain message (never the CLI's output).
func (s *Session) exitErr(werr error, text string) error {
	if s.killed.Load() {
		return ErrCanceled
	}
	if werr == nil {
		return nil
	}
	var ee *exec.ExitError
	if (errors.As(werr, &ee) && ee.ExitCode() == 127) || nodeMissing(text) {
		return errNoNode(s.title)
	}
	if lspinstall.IsOfflineText(text) {
		return errOffline()
	}
	return errNotFinished()
}

// tailLen is the size of the kept output (tests).
func (s *Session) tailLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tail)
}

// UseDeviceAuth reports whether env looks like an SSH session without a
// display or a browser setting, where a local browser callback cannot
// work (Codex then signs in with a link and a code).
func UseDeviceAuth(env []string) bool {
	if envGet(env, "SSH_CONNECTION") == "" && envGet(env, "SSH_TTY") == "" {
		return false
	}
	return envGet(env, "DISPLAY") == "" && envGet(env, "WAYLAND_DISPLAY") == "" && envGet(env, "BROWSER") == ""
}

// envGet is the value of k in env (the last one wins).
func envGet(env []string, k string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], k+"="); ok {
			return v
		}
	}
	return ""
}

// lookPath finds bin on the PATH of env.
func lookPath(bin string, env []string) (string, error) {
	for _, d := range filepath.SplitList(envGet(env, "PATH")) {
		if d == "" {
			continue
		}
		p := filepath.Join(d, bin)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}
