package testkit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// Options configures a Driver. Zero values pick the defaults.
type Options struct {
	W, H int // window size (default 120×40)

	Nvim bool // run a real embedded nvim (test skipped without nvim on PATH)
	Git  bool // make the workspace a git repo with a few commits and changes

	// Files is the workspace content (path → body); nil means DefaultFiles.
	Files map[string]string
	// Setup, when set, runs on the workspace before the app starts (e.g. to
	// build a git history with fixed dates or add a remote).
	Setup func(t testing.TB, dir string)
	// Config adds or replaces files in the sandboxed config dir
	// (~/.config/termocode) before the app starts: "keymap.json", or a
	// "config.json" saved by an earlier driver to test a restart.
	Config map[string]string

	// Quiet is how long no message may arrive before a step counts as
	// settled (default 5ms, 40ms with nvim). MaxSettle caps a step's
	// settle time (default 300ms, 1.5s with nvim).
	Quiet, MaxSettle time.Duration
	// StepTimeout is the deadline for one Update or View (default 5s).
	StepTimeout time.Duration

	// BeforeReady, when set, runs right after Init (and a first window
	// size, not settled), while the embedded nvim is still in its setup:
	// the app is usable before nvim.ReadyMsg. New then waits for ReadyMsg
	// as usual.
	BeforeReady func(d *Driver)
}

// Driver runs the app model step by step. See the package doc.
type Driver struct {
	T   testing.TB
	Dir string // workspace root (the cwd while the driver lives)
	W   int
	H   int

	opt     Options
	model   tea.Model
	msgs    chan tea.Msg
	done    chan struct{}
	frame   string
	history []string
	ready   bool     // nvim.ReadyMsg seen
	quit    bool     // the app returned tea.Quit
	updates []string // every message Update got, in order (see Updates)
}

// New builds a sandboxed app model at opt.W × opt.H and settles it.
func New(t testing.TB, opt Options) *Driver {
	t.Helper()
	if opt.Nvim {
		if _, err := exec.LookPath("nvim"); err != nil {
			t.Skip("nvim not on PATH")
		}
	}
	if opt.W == 0 {
		opt.W = 120
	}
	if opt.H == 0 {
		opt.H = 40
	}
	if opt.Quiet == 0 {
		opt.Quiet = 5 * time.Millisecond
		if opt.Nvim {
			opt.Quiet = 40 * time.Millisecond
		}
	}
	if opt.MaxSettle == 0 {
		opt.MaxSettle = 300 * time.Millisecond
		if opt.Nvim {
			opt.MaxSettle = 1500 * time.Millisecond
		}
	}
	if opt.StepTimeout == 0 {
		opt.StepTimeout = 5 * time.Second
	}

	sandbox(t, opt.Nvim)
	for name, body := range opt.Config {
		p := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dir := makeWorkspace(t, opt.Files, opt.Git)
	if opt.Setup != nil {
		opt.Setup(t, dir)
	}
	t.Chdir(dir)
	t.Setenv("PWD", dir)

	oldProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)

	d := &Driver{
		T: t, Dir: dir, W: opt.W, H: opt.H, opt: opt,
		msgs: make(chan tea.Msg, 4096),
		done: make(chan struct{}),
	}
	t.Cleanup(func() {
		close(d.done)
		if m, ok := d.model.(app.Model); ok {
			_ = m.Close()
		}
		lipgloss.SetColorProfile(oldProfile)
	})

	d.history = append(d.history, fmt.Sprintf("start %dx%d nvim=%v git=%v", opt.W, opt.H, opt.Nvim, opt.Git))
	var initCmd tea.Cmd
	if !d.call("New+Init", func() { d.model = app.New(); initCmd = d.model.Init() }) {
		t.FailNow()
	}
	d.run(initCmd)
	if opt.BeforeReady != nil {
		d.deliver(tea.WindowSizeMsg{Width: opt.W, Height: opt.H})
		opt.BeforeReady(d)
	}
	d.Send(tea.WindowSizeMsg{Width: opt.W, Height: opt.H})
	if opt.Nvim {
		d.WaitFor(func() bool { return d.ready }, 15*time.Second)
		if !d.ready {
			d.fail("nvim did not become ready within 15s")
		}
	}
	return d
}

// Frame returns the last rendered frame.
func (d *Driver) Frame() string { return d.frame }

// Model returns the current app model.
func (d *Driver) Model() app.Model { return d.model.(app.Model) }

// NvimReady reports whether the embedded nvim finished its setup.
func (d *Driver) NvimReady() bool { return d.ready }

// History returns the steps run so far, one line each.
func (d *Driver) History() []string { return append([]string(nil), d.history...) }

// Updates returns how many messages Update has handled so far and the
// last few of them. Idle tests use it: a screen that nothing changes must
// not keep the app busy (a timer that never stops costs CPU and makes
// keys lag behind).
func (d *Driver) Updates() (int, []string) {
	n := len(d.updates)
	return n, append([]string(nil), d.updates[max(0, n-20):]...)
}

// UpdatesSince returns every message Update has handled after the
// from-th one (a count from Updates).
func (d *Driver) UpdatesSince(from int) []string {
	if from < 0 || from > len(d.updates) {
		from = len(d.updates)
	}
	return append([]string(nil), d.updates[from:]...)
}

// Failed reports whether the test has failed.
func (d *Driver) Failed() bool { return d.T.Failed() }

// Quit reports whether the app asked to quit (tea.Quit). The model may
// have closed nvim then; start a new Driver to go on.
func (d *Driver) Quit() bool { return d.quit }

// Send delivers msg, settles and returns the new frame.
func (d *Driver) Send(msg tea.Msg) string {
	d.T.Helper()
	d.history = append(d.history, describe(msg))
	d.deliver(msg)
	d.settle(d.opt.MaxSettle)
	d.checkNvim()
	return d.render()
}

// checkNvim fails the test when nvim stays in a hit-enter / more /
// confirm prompt: the next RPC call from the app would hang. The nvim
// client answers such prompts within ~100 ms, so only a prompt that is
// still there half a second later counts.
func (d *Driver) checkNvim() {
	d.T.Helper()
	if !d.opt.Nvim || d.quit {
		return
	}
	m, ok := d.model.(app.Model)
	if !ok {
		return
	}
	inPrompt := func() (string, bool) {
		var mode string
		var blocking, alive bool
		ok := d.call("nvim_get_mode", func() { mode, blocking, alive = m.NvimMode() })
		return mode, ok && alive && blocking && strings.HasPrefix(mode, "r")
	}
	if _, stuck := inPrompt(); !stuck {
		return
	}
	time.Sleep(500 * time.Millisecond)
	if mode, stuck := inPrompt(); stuck {
		d.T.Fatalf("nvim is stuck in a prompt (mode %q): the app would freeze\nlast steps:\n  %s",
			mode, strings.Join(tail(d.history, 12), "\n  "))
	}
}

// Key sends one key per argument ("ctrl+p", "enter", "a").
func (d *Driver) Key(keys ...string) string {
	d.T.Helper()
	for _, k := range keys {
		d.Send(KeyMsg(k))
	}
	return d.frame
}

// Type sends each rune of text as a key.
func (d *Driver) Type(text string) string {
	d.T.Helper()
	for _, r := range text {
		d.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return d.frame
}

// Move sends a mouse motion with no button held (hover).
func (d *Driver) Move(x, y int) string {
	d.T.Helper()
	return d.Send(tea.MouseMsg{X: x, Y: y, Type: tea.MouseMotion, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
}

// Press sends a left-button press.
func (d *Driver) Press(x, y int) string {
	d.T.Helper()
	return d.Send(tea.MouseMsg{X: x, Y: y, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
}

// Release sends a left-button release.
func (d *Driver) Release(x, y int) string {
	d.T.Helper()
	return d.Send(tea.MouseMsg{X: x, Y: y, Type: tea.MouseRelease, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
}

// Click presses and releases the left button at x, y.
func (d *Driver) Click(x, y int) string {
	d.T.Helper()
	d.Press(x, y)
	return d.Release(x, y)
}

// RightClick presses and releases the right button at x, y.
func (d *Driver) RightClick(x, y int) string {
	d.T.Helper()
	d.Send(tea.MouseMsg{X: x, Y: y, Type: tea.MouseRight, Action: tea.MouseActionPress, Button: tea.MouseButtonRight})
	return d.Send(tea.MouseMsg{X: x, Y: y, Type: tea.MouseRelease, Action: tea.MouseActionRelease, Button: tea.MouseButtonRight})
}

// Wheel scrolls one notch up (or down) at x, y.
func (d *Driver) Wheel(x, y int, up bool) string {
	d.T.Helper()
	if up {
		return d.Send(tea.MouseMsg{X: x, Y: y, Type: tea.MouseWheelUp, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	}
	return d.Send(tea.MouseMsg{X: x, Y: y, Type: tea.MouseWheelDown, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
}

// Drag presses at (x0, y0), moves in steps to (x1, y1) and releases there.
func (d *Driver) Drag(x0, y0, x1, y1 int) string {
	d.T.Helper()
	d.Press(x0, y0)
	const n = 4
	for i := 1; i <= n; i++ {
		x, y := x0+(x1-x0)*i/n, y0+(y1-y0)*i/n
		d.Send(tea.MouseMsg{X: x, Y: y, Type: tea.MouseMotion, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	}
	return d.Release(x1, y1)
}

// Resize changes the window size.
func (d *Driver) Resize(w, h int) string {
	d.T.Helper()
	d.W, d.H = w, h
	return d.Send(tea.WindowSizeMsg{Width: w, Height: h})
}

// Action sends an app.DevActionMsg ("view:git", "open:main.go", …).
func (d *Driver) Action(id string) string {
	d.T.Helper()
	return d.Send(app.DevActionMsg{ID: id})
}

// Palette runs a command-palette entry by its id.
func (d *Driver) Palette(id string) string {
	d.T.Helper()
	return d.Action("palette:" + id)
}

// Settle pumps messages for up to dur (or until quiet) and re-renders.
func (d *Driver) Settle(dur time.Duration) string {
	d.T.Helper()
	d.settle(dur)
	return d.render()
}

// WaitFor pumps messages until cond is true or timeout passes, then
// renders. It reports cond's final value.
func (d *Driver) WaitFor(cond func() bool, timeout time.Duration) bool {
	d.T.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() && time.Now().Before(deadline) {
		d.pump(d.opt.Quiet)
	}
	d.render()
	return cond()
}

// WaitForText waits until the frame (ANSI stripped) contains s.
func (d *Driver) WaitForText(s string, timeout time.Duration) bool {
	d.T.Helper()
	return d.WaitFor(func() bool { return strings.Contains(ansi.Strip(d.render()), s) }, timeout)
}

// ── internals ───────────────────────────────────────────────────────────

// fail reports a test failure with the recent steps.
func (d *Driver) fail(format string, args ...any) {
	d.T.Helper()
	d.T.Errorf("%s\nlast steps:\n  %s", fmt.Sprintf(format, args...), strings.Join(tail(d.history, 12), "\n  "))
}

// call runs f with a deadline and panic recovery. It returns false when
// f panicked; a timeout ends the test.
func (d *Driver) call(what string, f func()) (ok bool) {
	d.T.Helper()
	type result struct {
		panicked any
		stack    []byte
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- result{r, debug.Stack()}
			}
		}()
		f()
		ch <- result{}
	}()
	select {
	case r := <-ch:
		if r.panicked != nil {
			d.fail("panic in %s: %v\n%s", what, r.panicked, r.stack)
			return false
		}
		return true
	case <-time.After(d.opt.StepTimeout):
		buf := make([]byte, 1<<20)
		buf = buf[:runtime.Stack(buf, true)]
		d.T.Fatalf("%s did not return within %v (hang)%s\nlast steps:\n  %s\ngoroutines:\n%s",
			what, d.opt.StepTimeout, d.nvimModeNote(), strings.Join(tail(d.history, 12), "\n  "), buf)
		return false
	}
}

// nvimModeNote describes nvim's mode for a hang report (nvim answers
// nvim_get_mode even while it blocks everything else).
func (d *Driver) nvimModeNote() string {
	m, ok := d.model.(app.Model)
	if !d.opt.Nvim || !ok {
		return ""
	}
	ch := make(chan string, 1)
	go func() {
		mode, blocking, alive := m.NvimMode()
		ch <- fmt.Sprintf(" — nvim mode %q blocking=%v alive=%v", mode, blocking, alive)
	}()
	select {
	case s := <-ch:
		return s
	case <-time.After(time.Second):
		return " — nvim does not answer nvim_get_mode"
	}
}

// deliver runs one Update and starts the returned command.
func (d *Driver) deliver(msg tea.Msg) {
	d.T.Helper()
	if _, ok := msg.(nvim.ReadyMsg); ok {
		d.ready = true
	}
	d.updates = append(d.updates, describe(msg))
	var cmd tea.Cmd
	if d.call("Update("+describe(msg)+")", func() {
		var nm tea.Model
		nm, cmd = d.model.Update(msg)
		d.model = nm
	}) {
		d.run(cmd)
	}
}

// run starts cmd in a goroutine; its message comes back through d.msgs.
func (d *Driver) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	// Bind the channels now: after Restart a late message from the old
	// model must go nowhere, not to the new one.
	msgs, done := d.msgs, d.done
	go func() {
		var msg tea.Msg
		func() {
			defer func() {
				if r := recover(); r != nil {
					msg = cmdPanic{r, debug.Stack()}
				}
			}()
			msg = cmd()
		}()
		post(msgs, done, msg)
	}()
}

func post(msgs chan tea.Msg, done chan struct{}, msg tea.Msg) {
	if msg == nil {
		return
	}
	select {
	case msgs <- msg:
	case <-done:
	}
}

type cmdPanic struct {
	v     any
	stack []byte
}

// handle routes a message a command produced, the way tea.Program does.
func (d *Driver) handle(msg tea.Msg) {
	d.T.Helper()
	switch m := msg.(type) {
	case cmdPanic:
		d.fail("panic in tea.Cmd: %v\n%s", m.v, m.stack)
		return
	case tea.BatchMsg:
		for _, c := range m {
			d.run(c)
		}
		return
	case tea.QuitMsg, tea.InterruptMsg, tea.SuspendMsg:
		d.history = append(d.history, fmt.Sprintf("(app asked to %T)", m))
		if _, ok := m.(tea.QuitMsg); ok {
			d.quit = true
		}
		return
	}
	t := reflect.TypeOf(msg)
	if t.PkgPath() == "github.com/charmbracelet/bubbletea" {
		if t.Name() == "sequenceMsg" { // tea.Sequence: run one after another
			v := reflect.ValueOf(msg)
			cmds := make([]tea.Cmd, v.Len())
			for i := range cmds {
				cmds[i], _ = v.Index(i).Interface().(tea.Cmd)
			}
			msgs, done := d.msgs, d.done
			go func() {
				for _, c := range cmds {
					if c == nil {
						continue
					}
					if r := c(); r != nil {
						post(msgs, done, r)
					}
				}
			}()
			return
		}
		if !isExported(t.Name()) {
			return // renderer / terminal control (cursor, alt screen, exec, …)
		}
	}
	d.deliver(msg)
}

func isExported(name string) bool { return name != "" && name[0] >= 'A' && name[0] <= 'Z' }

// pump handles messages until none arrives for quiet.
func (d *Driver) pump(quiet time.Duration) bool {
	d.T.Helper()
	select {
	case msg := <-d.msgs:
		d.handle(msg)
		return true
	case <-time.After(quiet):
		return false
	}
}

// settle pumps until quiet or max passes.
func (d *Driver) settle(max time.Duration) {
	d.T.Helper()
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if !d.pump(d.opt.Quiet) {
			return
		}
	}
}

// render calls View and checks the frame.
func (d *Driver) render() string {
	d.T.Helper()
	var frame string
	if !d.call("View", func() { frame = d.model.View() }) {
		return d.frame
	}
	d.frame = frame
	if probs := FrameProblems(frame, d.W, d.H); len(probs) > 0 {
		d.fail("bad frame %dx%d:\n  %s", d.W, d.H, strings.Join(probs, "\n  "))
	}
	return frame
}

func tail(s []string, n int) []string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// describe is a short, readable form of a message for the step history.
func describe(msg tea.Msg) string {
	switch m := msg.(type) {
	case tea.KeyMsg:
		return "key " + m.String()
	case tea.MouseMsg:
		return fmt.Sprintf("mouse %s at %d,%d", tea.MouseEvent(m).String(), m.X, m.Y)
	case tea.WindowSizeMsg:
		return fmt.Sprintf("resize %dx%d", m.Width, m.Height)
	case app.DevActionMsg:
		return "action " + m.ID
	}
	return fmt.Sprintf("%T", msg)
}
