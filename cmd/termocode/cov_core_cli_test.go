//go:build unix

package main

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/termcaps"
	"github.com/amin-jalali/termocode/internal/termimg"
)

// covFakeSession listens on the session socket of dir like a running
// session server: it answers `sessions` with info and ends on `kill`.
func covFakeSession(t *testing.T, dir string, info session.Info) session.Paths {
	t.Helper()
	p, err := session.PathsFor(dir)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", p.Socket)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() { once.Do(func() { _ = ln.Close(); _ = os.Remove(p.Socket) }) }
	t.Cleanup(stop)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var hdr [5]byte
				if _, err := io.ReadFull(c, hdr[:]); err != nil {
					return
				}
				payload := make([]byte, binary.BigEndian.Uint32(hdr[1:]))
				if _, err := io.ReadFull(c, payload); err != nil {
					return
				}
				switch hdr[0] {
				case 'S':
					b, _ := json.Marshal(info)
					out := make([]byte, 5+len(b))
					out[0] = 'S'
					binary.BigEndian.PutUint32(out[1:], uint32(len(b)))
					copy(out[5:], b)
					_, _ = c.Write(out)
				case 'K':
					stop()
				}
			}(c)
		}
	}()
	return p
}

func TestCovCoreSessionsListsRunning(t *testing.T) {
	covMainEnv(t)
	older := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	covFakeSession(t, t.TempDir(), session.Info{Workspace: "/w/new", PID: 42, Started: older.Add(time.Hour)})
	covFakeSession(t, t.TempDir(), session.Info{Workspace: "/w/old", PID: 7, Started: older, Attached: true})
	r := covCall(t, []string{"sessions"}, nil)
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	if r.code != 0 || len(lines) != 2 {
		t.Fatalf("sessions: %+v", r)
	}
	if !strings.HasPrefix(lines[0], "attached   pid 7 ") || !strings.HasSuffix(lines[0], "/w/old") {
		t.Errorf("first (oldest) line = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "detached   pid 42 ") || !strings.HasSuffix(lines[1], "/w/new") {
		t.Errorf("second line = %q", lines[1])
	}
}

func TestCovCoreKillEndsSession(t *testing.T) {
	covMainEnv(t)
	dir := t.TempDir()
	p := covFakeSession(t, dir, session.Info{Workspace: dir})
	r := covCall(t, []string{"kill", dir}, nil)
	if r.code != 0 || r.stdout != "ended the session for "+p.Workspace+"\n" {
		t.Errorf("kill: %+v", r)
	}
	if _, err := os.Stat(p.Socket); !os.IsNotExist(err) {
		t.Errorf("socket still there: %v", err)
	}
}

func TestCovCoreAttachNeedsTerminal(t *testing.T) {
	covMainEnv(t)
	dir := t.TempDir()
	covFakeSession(t, dir, session.Info{Workspace: dir})
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	old := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = old }()
	code, errOut := covStderr(t, func() int { return runClient(dir, clientOpts{}) })
	if code != 1 || errOut != "termocode: attach needs a terminal\n" {
		t.Errorf("attach without a terminal: %d %q", code, errOut)
	}
}

// A broken session folder: every session command says why and fails.
func TestCovCoreSessionDirBroken(t *testing.T) {
	covMainEnv(t)
	// A short base: under macOS's long $TMPDIR the session dir would move
	// to the $TMPDIR fallback and never meet the file.
	file := filepath.Join(shortTemp(t), "runtime")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", file)
	dir := t.TempDir()
	for _, args := range [][]string{{"sessions"}, {"kill", dir}} {
		r := covCall(t, args, nil)
		if r.code != 1 || !strings.HasPrefix(r.stderr, "termocode: ") || !strings.Contains(r.stderr, "not a directory") {
			t.Errorf("%q: %+v", args, r)
		}
	}
	code, errOut := covStderr(t, func() int { return runClient(dir, clientOpts{create: true}) })
	if code != 1 || !strings.Contains(errOut, "not a directory") {
		t.Errorf("attach: %d %q", code, errOut)
	}
}

func TestCovCoreFontDelta(t *testing.T) {
	covMainEnv(t)
	t.Setenv("TERMOCODE_FONT_DELTA", "")
	if got := fontDelta(); got != 0 {
		t.Errorf("default = %d", got)
	}
	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode")
	_ = os.MkdirAll(cfg, 0o755)
	if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"font_delta":-2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := fontDelta(); got != -2 {
		t.Errorf("config = %d, want -2", got)
	}
	t.Setenv("TERMOCODE_FONT_DELTA", "3")
	if got := fontDelta(); got != 3 {
		t.Errorf("env = %d, want 3", got)
	}
	// No change asked: nothing to undo on the way out.
	if s := applyFontDelta(0); s.Restore() != "" {
		t.Errorf("delta 0 restore = %q", s.Restore())
	}
}

// covEnv is a getenv over m.
func covEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestCovCoreStartupProbe(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	oldIn := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = oldIn }()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	state := t.TempDir()
	base := map[string]string{"XDG_STATE_HOME": state, "TERM": "xterm-256color"}
	with := func(kv ...string) func(string) string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return covEnv(m)
	}

	// Probe off: only the environment, and the reason says so.
	c := startupProbe(termimg.ModeAuto, with("TERMOCODE_PROBE", "off"), now)
	if !strings.Contains(c.Reason, "(not probed: TERMOCODE_PROBE=off)") {
		t.Errorf("probe off: %+v", c)
	}
	// Images off as well: plain blocks, no reason needed.
	c = startupProbe(termimg.ModeOff, with("TERMOCODE_PROBE", "off"), now)
	if c.Protocol != termimg.Blocks || c.Reason != "" {
		t.Errorf("probe + images off: %+v", c)
	}
	// A terminal that was silent last time is not asked again.
	getenv := with()
	termcaps.Remember(termcaps.CachePath(getenv), termcaps.CacheKey(getenv), termcaps.Entry{Answers: false}, now.Add(-time.Minute))
	c = startupProbe(termimg.ModeAuto, getenv, now)
	if !strings.Contains(c.Reason, "did not answer last time") {
		t.Errorf("silent before: %+v", c)
	}
	// Inside tmux with images off: the key probe returns at once, and
	// nothing is cached (no real round).
	tmux := with("TMUX", "/tmp/tmux-1/default,1,0", "XDG_STATE_HOME", t.TempDir())
	c = startupProbe(termimg.ModeOff, tmux, now)
	if c.Protocol != termimg.Blocks || c.Answered {
		t.Errorf("tmux keys only: %+v", c)
	}
	if _, ok := termcaps.Lookup(termcaps.CachePath(tmux), termcaps.CacheKey(tmux), now); ok {
		t.Error("a probe that asked nothing was cached")
	}
	if asked(tmux) {
		t.Error("asked inside tmux")
	}
}

func TestCovCoreClientCaps(t *testing.T) {
	covMainEnv(t)
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("KITTY_WINDOW_ID", "1")
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	oldIn := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = oldIn }()
	// A new UI probes by itself: no graphics from the client.
	if c := clientCaps(false); c.Graphics != nil || c.Env["TERM"] != "xterm-kitty" {
		t.Errorf("not running: %+v", c)
	}
	// A running UI gets this terminal's graphics.
	if c := clientCaps(true); c.Graphics == nil {
		t.Errorf("running: no graphics: %+v", c)
	}
	// Images off in the settings: nothing is probed.
	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode")
	_ = os.MkdirAll(cfg, 0o755)
	if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"images":"off"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := clientCaps(true); c.Graphics != nil {
		t.Errorf("images off: %+v", c.Graphics)
	}
	// A client without a probe: images off keeps blocks.
	msg := terminalChange(&session.ClientCaps{Env: map[string]string{"TERM": "xterm-kitty", "KITTY_WINDOW_ID": "1"}, Gen: 3})
	if msg.Graphics.Protocol != termimg.Blocks || msg.Gen != 3 {
		t.Errorf("images off change: %+v", msg)
	}
}

// covRecorder is a tiny tea.Model that keeps the messages it gets.
type covRecorder struct {
	mu   *sync.Mutex
	msgs *[]tea.Msg
}

func (r covRecorder) Init() tea.Cmd { return nil }
func (r covRecorder) Update(m tea.Msg) (tea.Model, tea.Cmd) {
	r.mu.Lock()
	*r.msgs = append(*r.msgs, m)
	r.mu.Unlock()
	return r, nil
}
func (r covRecorder) View() string { return "" }

func TestCovCoreWatchRepaint(t *testing.T) {
	covMainEnv(t)
	// Outside a session nothing is watched.
	t.Setenv(session.EnvSession, "")
	watchRepaint(nil)

	caps := filepath.Join(t.TempDir(), "caps.json")
	b, _ := json.Marshal(session.ClientCaps{Env: map[string]string{"TERM": "foot"}, Gen: 9,
		Graphics: &session.GraphicsCaps{Protocol: "sixel", Reason: "probed"}})
	if err := os.WriteFile(caps, b, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, k := range session.CapsEnvKeys { // the attach rewrites them
		t.Setenv(k, os.Getenv(k))
	}
	t.Setenv(session.EnvSession, "k")
	t.Setenv(session.EnvCapsFile, caps)
	t.Setenv("TERM", "xterm")
	var mu sync.Mutex
	var msgs []tea.Msg
	pr, pw := io.Pipe() // not an *os.File: no epoll reader to race on shutdown
	defer pw.Close()
	p := tea.NewProgram(covRecorder{&mu, &msgs}, tea.WithInput(pr), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	done := make(chan struct{})
	go func() { _, _ = p.Run(); close(done) }()
	defer func() { p.Quit(); <-done }()
	watchRepaint(p)
	if err := syscall.Kill(os.Getpid(), session.RepaintSignal); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	var change *app.TerminalChangedMsg
	for change == nil && time.Now().Before(deadline) {
		mu.Lock()
		for _, m := range msgs {
			if c, ok := m.(app.TerminalChangedMsg); ok {
				change = &c
			}
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	if change == nil {
		t.Fatal("no terminal change after the repaint signal")
	}
	if change.Graphics.Protocol != termimg.Sixel || change.Gen != 9 {
		t.Errorf("change = %+v", change)
	}
	if os.Getenv("TERM") != "foot" {
		t.Errorf("TERM = %q, want the client's foot", os.Getenv("TERM"))
	}
}
