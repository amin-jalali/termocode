//go:build e2e

// Package e2e runs the real termocode binary inside a PTY, feeds its output
// into a VT emulator and checks what a user would actually see.
//
// Run: go -C e2e test -tags e2e ./...
package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// binPath is the termocode binary built once by TestMain.
var binPath string

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("nvim"); err != nil {
		fmt.Println("e2e: nvim not in PATH, skipping")
		os.Exit(0)
	}
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Println("e2e: git not in PATH, skipping")
		os.Exit(0)
	}
	dir, err := os.MkdirTemp("", "termocode-e2e-bin-")
	if err != nil {
		fmt.Println("e2e:", err)
		os.Exit(1)
	}
	// E2E_BIN runs the suite against a prebuilt binary (e.g. to bisect).
	if b := os.Getenv("E2E_BIN"); b != "" {
		binPath = b
	} else {
		binPath = filepath.Join(dir, "termocode")
		build := exec.Command("go", "build", "-o", binPath, "./cmd/termocode")
		build.Dir = ".."
		build.Stdout, build.Stderr = os.Stdout, os.Stderr
		if err := build.Run(); err != nil {
			fmt.Println("e2e: build failed:", err)
			os.Exit(1)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// Term is one running termocode inside a PTY + VT emulator.
type Term struct {
	t    *testing.T
	cmd  *exec.Cmd
	ptmx *os.File
	home string
	ws   string

	mu        sync.Mutex
	em        *vt.Emulator
	raw       bytes.Buffer // full raw output (for dumps)
	pending   []byte       // incomplete UTF-8 tail between reads
	badUTF8   []string     // context around invalid UTF-8 bytes
	lastApp   []string     // last alt-screen frame seen before Quit
	cursorVis bool
	modes     map[string]bool
	exited    chan struct{}
	exitErr   error
	readDone  chan struct{}
	started   time.Time // when the process was started
	altAt     time.Time // first alt-screen switch (\e[?1049h) seen
}

// StartOpts configures Start.
type StartOpts struct {
	Cols, Rows int
	Workspace  string // defaults to a fresh repo from NewRepo
	Args       []string
	NvimInit   string   // optional init.lua for the embedded nvim
	Home       string   // reuse a HOME (e.g. to start again after a disconnect)
	Env        []string // extra environment ("K=V")
	// Silent makes the terminal answer no queries at all (OSC 11, DA1,
	// CSI 6n …), like some muxers, IDE terminals and dumb ptys.
	Silent bool
}

// Start launches termocode in a fresh PTY with an isolated HOME.
func Start(t *testing.T, o StartOpts) *Term {
	t.Helper()
	if o.Cols == 0 {
		o.Cols = 120
	}
	if o.Rows == 0 {
		o.Rows = 36
	}
	home := o.Home
	if home == "" {
		home = t.TempDir()
	}
	ws := o.Workspace
	if ws == "" {
		ws = NewRepo(t)
	}
	// Before the cleanup below that ends termocode: it runs after it.
	watchLeaks(t, home, ws)
	for _, d := range []string{".config", ".local/share", ".local/state", ".cache"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Stub plugin checkouts stop the first-run background `git clone`s: no
	// network in tests, and no "Plugin download failed" toast.
	plugins := filepath.Join(home, ".local/share/termocode/plugins")
	for _, d := range []string{"vim-visual-multi/.git", "nvim-dap/.git"} {
		if err := os.MkdirAll(filepath.Join(plugins, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stub := filepath.Join(plugins, "rainbow-delimiters.nvim/lua/rainbow-delimiters")
	if err := os.MkdirAll(stub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stub, "setup.lua"), []byte("return {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if o.NvimInit != "" {
		d := filepath.Join(home, ".config", "nvim")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "init.lua"), []byte(o.NvimInit), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(binPath, o.Args...)
	cmd.Dir = ws
	cmd.Env = append(isolatedEnv(home), o.Env...)

	em := vt.NewEmulator(o.Cols, o.Rows)
	// Like a real terminal: answer OSC 10/11 colour queries.
	em.SetDefaultForegroundColor(color.RGBA{0xd0, 0xd0, 0xd0, 0xff})
	em.SetDefaultBackgroundColor(color.RGBA{0x10, 0x10, 0x10, 0xff})
	tm := &Term{
		t: t, cmd: cmd, home: home, ws: ws, em: em,
		cursorVis: true,
		modes:     map[string]bool{},
		exited:    make(chan struct{}),
		readDone:  make(chan struct{}),
	}
	em.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(v bool) { tm.cursorVis = v },
		EnableMode:       func(m ansi.Mode) { tm.modes[modeKey(m)] = true },
		DisableMode:      func(m ansi.Mode) { delete(tm.modes, modeKey(m)) },
	})

	tm.started = time.Now()
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(o.Cols), Rows: uint16(o.Rows)})
	if err != nil {
		t.Fatalf("start termocode: %v", err)
	}
	tm.ptmx = ptmx

	// PTY → emulator.
	go func() {
		defer close(tm.readDone)
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				tm.feed(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	// Emulator replies (DSR, DA, OSC queries) → PTY, like a real terminal.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := em.Read(buf)
			if n > 0 && !o.Silent {
				_, _ = ptmx.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		tm.exitErr = cmd.Wait()
		close(tm.exited)
	}()

	t.Cleanup(func() {
		if t.Failed() {
			tm.dump("failure")
		}
		select {
		case <-tm.exited:
		default:
			_ = cmd.Process.Kill()
			<-tm.exited
		}
		_ = ptmx.Close()
		tm.mu.Lock()
		_ = em.Close()
		tm.mu.Unlock()
	})
	return tm
}

func modeKey(m ansi.Mode) string {
	switch m.(type) {
	case ansi.DECMode:
		return fmt.Sprintf("?%d", m.Mode())
	default:
		return fmt.Sprintf("%d", m.Mode())
	}
}

func isolatedEnv(home string) []string {
	keep := []string{"PATH", "TMPDIR", "GOCOVERDIR"}
	env := []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local/share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local/state"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
		"SHELL=/bin/sh",
		"TERMOCODE_FONT_DELTA=0",
		"GIT_CONFIG_NOSYSTEM=1",
	}
	for _, k := range keep {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// feed writes PTY output into the emulator and validates UTF-8 on the raw
// byte stream (a split glyph is invisible to the grid but breaks real
// terminals).
func (tm *Term) feed(p []byte) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.raw.Write(p)
	if tm.altAt.IsZero() && bytes.Contains(tm.raw.Bytes(), []byte("\x1b[?1049h")) {
		tm.altAt = time.Now()
	}
	data := append(tm.pending, p...)
	tm.pending = nil
	for i := 0; i < len(data); {
		if data[i] < utf8.RuneSelf {
			i++
			continue
		}
		if !utf8.FullRune(data[i:]) {
			tm.pending = append([]byte(nil), data[i:]...)
			break
		}
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size == 1 {
			lo, hi := max(0, i-24), min(len(data), i+24)
			tm.badUTF8 = append(tm.badUTF8, fmt.Sprintf("%q", data[lo:hi]))
		}
		i += size
	}
	_, _ = tm.em.Write(p)
}

// Size returns the emulator size.
func (tm *Term) Size() (cols, rows int) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.em.Width(), tm.em.Height()
}

// Lines returns the visible screen as one string per row (no styles,
// trailing blanks trimmed). Wide glyphs take their cell; the cell after
// a wide glyph contributes nothing.
func (tm *Term) Lines() []string {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.linesLocked()
}

func (tm *Term) linesLocked() []string {
	w, h := tm.em.Width(), tm.em.Height()
	out := make([]string, h)
	for y := 0; y < h; y++ {
		var b strings.Builder
		for x := 0; x < w; x++ {
			c := tm.em.CellAt(x, y)
			if c == nil {
				b.WriteByte(' ')
				continue
			}
			if c.Width == 0 && c.Content == "" {
				continue // continuation of a wide glyph
			}
			if c.Content == "" || hidden(c.Style.Fg, c.Style.Bg) {
				b.WriteByte(' ')
				continue
			}
			b.WriteString(c.Content)
		}
		out[y] = strings.TrimRight(b.String(), " ")
	}
	return out
}

// hidden reports whether a glyph is drawn so close to its background colour
// that a user cannot see it (the app dims text behind modal overlays this
// way). Such cells read as blank.
func hidden(fg, bg color.Color) bool {
	if fg == nil || bg == nil {
		return false
	}
	r1, g1, b1, _ := fg.RGBA()
	r2, g2, b2, _ := bg.RGBA()
	d := func(a, b uint32) int {
		x := int(a>>8) - int(b>>8)
		if x < 0 {
			return -x
		}
		return x
	}
	return d(r1, r2)+d(g1, g2)+d(b1, b2) < 48
}

// Screen returns the visible screen as one string.
func (tm *Term) Screen() string { return strings.Join(tm.Lines(), "\n") }

// Send writes raw bytes to the app (as typed keys).
func (tm *Term) Send(s string) {
	tm.t.Helper()
	if _, err := tm.ptmx.Write([]byte(s)); err != nil {
		tm.t.Fatalf("send %q: %v", s, err)
	}
}

// Type sends text one rune at a time with a small gap so Bubble Tea sees
// key presses, not a paste.
func (tm *Term) Type(s string) {
	tm.t.Helper()
	for _, r := range s {
		tm.Send(string(r))
		time.Sleep(5 * time.Millisecond)
	}
}

// Common key sequences (xterm).
const (
	KeyEsc   = "\x1b"
	KeyEnter = "\r"
	KeyF1    = "\x1bOP"
	KeyCtrlP = "\x10"
	KeyCtrlQ = "\x11"
	KeyCtrlS = "\x13"
	KeyCtrlT = "\x14"
	KeyDown  = "\x1b[B"
	KeyUp    = "\x1b[A"
)

// MouseMove sends an SGR "motion, no button" event at 0-based cell (x, y).
func (tm *Term) MouseMove(x, y int) { tm.Send(fmt.Sprintf("\x1b[<35;%d;%dM", x+1, y+1)) }

// Click sends an SGR left press + release at 0-based cell (x, y).
func (tm *Term) Click(x, y int) {
	tm.Send(fmt.Sprintf("\x1b[<0;%d;%dM", x+1, y+1))
	time.Sleep(15 * time.Millisecond)
	tm.Send(fmt.Sprintf("\x1b[<0;%d;%dm", x+1, y+1))
}

// Resize changes the PTY size (the kernel sends SIGWINCH) and the emulator.
func (tm *Term) Resize(cols, rows int) {
	tm.t.Helper()
	tm.mu.Lock()
	tm.em.Resize(cols, rows)
	tm.mu.Unlock()
	if err := pty.Setsize(tm.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		tm.t.Fatalf("resize: %v", err)
	}
}

// WaitFor polls until cond(lines) is true or the timeout hits.
func (tm *Term) WaitFor(what string, timeout time.Duration, cond func(lines []string) bool) {
	tm.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond(tm.Lines()) {
			return
		}
		select {
		case <-tm.exited:
			tm.t.Fatalf("termocode exited while waiting for %s (err=%v)\n%s", what, tm.exitErr, tm.Screen())
		default:
		}
		if time.Now().After(deadline) {
			tm.t.Fatalf("timeout after %s waiting for %s\nscreen:\n%s", timeout, what, tm.Screen())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// WaitText waits until text appears anywhere on screen.
func (tm *Term) WaitText(text string, timeout time.Duration) {
	tm.t.Helper()
	tm.WaitFor(fmt.Sprintf("%q", text), timeout, func(ls []string) bool {
		return strings.Contains(strings.Join(ls, "\n"), text)
	})
}

// WaitGone waits until text is no longer on screen.
func (tm *Term) WaitGone(text string, timeout time.Duration) {
	tm.t.Helper()
	tm.WaitFor(fmt.Sprintf("%q to disappear", text), timeout, func(ls []string) bool {
		return !strings.Contains(strings.Join(ls, "\n"), text)
	})
}

// WaitStable waits until the screen has not changed for quiet.
func (tm *Term) WaitStable(quiet, timeout time.Duration) {
	tm.t.Helper()
	deadline := time.Now().Add(timeout)
	last := tm.Screen()
	since := time.Now()
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		cur := tm.Screen()
		if cur != last {
			last, since = cur, time.Now()
			continue
		}
		if time.Since(since) >= quiet {
			return
		}
	}
}

// Has reports whether text is on screen now.
func (tm *Term) Has(text string) bool { return strings.Contains(tm.Screen(), text) }

// Frame markers: the first row is the title/tab bar and the last row is the
// status bar. When the terminal scrolls (a line wraps at the bottom, a stray
// newline …) Bubble Tea only repaints changed rows, so these markers move.
type Frame struct {
	Top    func(string) bool
	Bottom func(string) bool
}

// CheckScreen asserts the invariants that must hold after every step:
// no scroll (markers on row 0 and the last row, empty scrollback),
// no U+FFFD on screen, valid UTF-8 in the byte stream, alt screen on.
func (tm *Term) CheckScreen(step string, f Frame) {
	tm.t.Helper()
	tm.mu.Lock()
	lines := tm.linesLocked()
	sb := tm.em.ScrollbackLen()
	alt := tm.em.IsAltScreen()
	bad := append([]string(nil), tm.badUTF8...)
	tm.mu.Unlock()

	var errs []string
	if !alt {
		errs = append(errs, "not on the alt screen")
	}
	if sb != 0 {
		errs = append(errs, fmt.Sprintf("scrollback has %d lines (screen scrolled)", sb))
	}
	if f.Top != nil && !f.Top(lines[0]) {
		errs = append(errs, fmt.Sprintf("row 0 lost its marker (screen scrolled?): %q", lines[0]))
	}
	if f.Bottom != nil && !f.Bottom(lines[len(lines)-1]) {
		errs = append(errs, fmt.Sprintf("last row lost its marker (screen scrolled?): %q", lines[len(lines)-1]))
	}
	for y, l := range lines {
		if strings.ContainsRune(l, utf8.RuneError) {
			errs = append(errs, fmt.Sprintf("row %d has U+FFFD: %q", y, l))
		}
	}
	if len(bad) > 0 {
		errs = append(errs, fmt.Sprintf("invalid UTF-8 in output (%d): %s", len(bad), strings.Join(bad[:min(3, len(bad))], " | ")))
	}
	if len(errs) > 0 {
		tm.t.Fatalf("[%s] screen check failed:\n  %s\nscreen:\n%s", step, strings.Join(errs, "\n  "), strings.Join(lines, "\n"))
	}
}

// Quit sends Ctrl+Q and asserts a clean exit and a restored terminal.
func (tm *Term) Quit() {
	tm.t.Helper()
	tm.mu.Lock()
	tm.lastApp = tm.linesLocked()
	tm.mu.Unlock()
	tm.Send(KeyCtrlQ)
	select {
	case <-tm.exited:
	case <-time.After(10 * time.Second):
		tm.t.Fatalf("termocode did not exit after Ctrl+Q\nscreen:\n%s", tm.Screen())
	}
	// Drain the rest of the output.
	select {
	case <-tm.readDone:
	case <-time.After(3 * time.Second):
	}
	if tm.exitErr != nil {
		var ee *exec.ExitError
		if errors.As(tm.exitErr, &ee) {
			tm.t.Fatalf("termocode exit status %d\nscreen:\n%s", ee.ExitCode(), tm.Screen())
		}
		tm.t.Fatalf("termocode wait: %v", tm.exitErr)
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.em.IsAltScreen() {
		tm.t.Errorf("still on the alt screen after exit")
	}
	if !tm.cursorVis {
		tm.t.Errorf("cursor hidden after exit")
	}
	for _, m := range []string{"?1000", "?1002", "?1003", "?1006", "?2004"} {
		if tm.modes[m] {
			tm.t.Errorf("mode %s still enabled after exit", m)
		}
	}
	if len(tm.badUTF8) > 0 {
		tm.t.Errorf("invalid UTF-8 in output: %v", tm.badUTF8)
	}
}

// dump writes the screen and raw output under E2E_DUMP_DIR (or the test's
// temp dir) so CI can upload it.
func (tm *Term) dump(tag string) {
	dir := os.Getenv("E2E_DUMP_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "termocode-e2e-dumps")
	}
	_ = os.MkdirAll(dir, 0o755)
	name := strings.NewReplacer("/", "_", " ", "_").Replace(tm.t.Name()) + "-" + tag
	tm.mu.Lock()
	lines := tm.linesLocked()
	lastApp := tm.lastApp
	raw := append([]byte(nil), tm.raw.Bytes()...)
	w, h := tm.em.Width(), tm.em.Height()
	tm.mu.Unlock()
	var b strings.Builder
	fmt.Fprintf(&b, "%dx%d\n", w, h)
	for y, l := range lines {
		fmt.Fprintf(&b, "%3d|%s\n", y, l)
	}
	if lastApp != nil {
		fmt.Fprintf(&b, "\nlast app frame before quit:\n")
		for y, l := range lastApp {
			fmt.Fprintf(&b, "%3d|%s\n", y, l)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, name+".screen.txt"), []byte(b.String()), 0o644)
	_ = os.WriteFile(filepath.Join(dir, name+".raw"), raw, 0o644)
	tm.t.Logf("screen dump: %s", filepath.Join(dir, name+".screen.txt"))
}

// NewRepo creates a git workspace with a few files and commits whose
// subjects and bodies are long and full of non-ASCII symbols.
func NewRepo(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = ws
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=E2E Tester", "GIT_AUTHOR_EMAIL=e2e@example.com",
			"GIT_COMMITTER_NAME=E2E Tester", "GIT_COMMITTER_EMAIL=e2e@example.com",
			"GIT_CONFIG_NOSYSTEM=1", "HOME="+ws,
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(ws, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write("README.md", "# demo\n\nHello ⎇ ⌥ → — 界\n")
	write("notes.txt", "first note\n")
	write("main.go", "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n")
	git("add", "-A")
	git("commit", "-q", "-m", "init: first commit ⎇ main")

	subjects := []string{
		"feat(ui): ⎇ branch picker with ⌥ shortcuts → faster switching — 界面 overhaul and a very long subject line that goes on and on",
		"fix: wide glyphs 界界界 in hover card → no more split UTF-8 — ⌥⎇",
		"docs: arrows → ← ↑ ↓ and dashes — – plus CJK 世界 in the guide",
		"refactor: emoji-free but symbol-heavy ⎇⎇⎇ ⌥⌥⌥ →→→ ———",
		"chore: bump deps",
	}
	for i := 0; i < 24; i++ {
		write(fmt.Sprintf("pkg/f%02d.txt", i), fmt.Sprintf("line %d ⎇ → 界\n", i))
		git("add", "-A")
		subj := subjects[i%len(subjects)] + fmt.Sprintf(" #%d", i)
		body := "Body line one with symbols ⎇ ⌥ → — 界.\n\nSecond paragraph: a long line that keeps going to make sure wrapping inside the hover card is exercised properly ⎇ ⌥ → — 界 世界.\n- bullet → one\n- bullet — two"
		git("commit", "-q", "-m", subj, "-m", body)
		if i == 10 {
			git("branch", "feature/⎇-界")
		}
	}
	// One uncommitted change so Source Control has something to show.
	write("main.go", "package main\n\nfunc main() {\n\tprintln(\"hi → 界\")\n}\n")
	return ws
}
