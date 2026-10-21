// Package shotkit runs the real termocode app model (with a real embedded
// nvim) in process, drives it with scripted steps and returns the frame
// Bubble Tea would print. The shots command (README screenshots) and the
// golden screen tests both use it.
package shotkit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/zalando/go-keyring"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Step is one scripted input. Msg (or Build, when the message depends on
// the current frame) is sent to the program, then the driver waits Wait.
type Step struct {
	Msg   tea.Msg
	Build func(frame string) (tea.Msg, error)
	Wait  time.Duration
	// Hold is how long a recording (Options.OnFrame) shows the frame after
	// this step; 0 = defaultHold.
	Hold time.Duration
}

// defaultHold is the recording time of a step without Hold.
const defaultHold = 600 * time.Millisecond

// Scene is a named script: a fresh app with the given theme / icon mode,
// extra environment and steps.
type Scene struct {
	Name  string
	Theme string // default "vscode-dark-plus"
	Icons string // nerd (default), unicode, ascii
	Env   map[string]string
	Steps []Step
	// SetupCard shows the first-run setup card (a fresh config).
	SetupCard bool
	// Recents are files (relative to the working directory) written to
	// recents.json, newest first; Workspaces likewise ("." = the working
	// directory) to workspaces.json. The start page lists them.
	Recents, Workspaces []string
	// KeepHover skips the final resize (it hides a hover label), so a
	// scene can show one.
	KeepHover bool
}

// demoAges are how long ago the seeded recents / workspaces were opened.
var demoAges = []time.Duration{2 * time.Hour, 3 * time.Hour, 5 * time.Hour, 26 * time.Hour, 9 * 24 * time.Hour}

// writeOpened writes paths (relative to the working directory) as a
// recents / workspaces list with opened_at times from demoAges.
func writeOpened(file string, rel []string) {
	if len(rel) == 0 {
		return
	}
	wd, _ := os.Getwd()
	type entry struct {
		Path     string    `json:"path"`
		OpenedAt time.Time `json:"opened_at"`
	}
	var es []entry
	for i, r := range rel {
		es = append(es, entry{filepath.Clean(filepath.Join(wd, r)), clock.Now().Add(-demoAges[min(i, len(demoAges)-1)])})
	}
	b, _ := json.Marshal(es)
	_ = os.WriteFile(file, b, 0o644)
}

// Act sends app.DevActionMsg{ID: id} (see internal/app/devhooks.go).
func Act(id string) Step { return Step{Msg: app.DevActionMsg{ID: id}, Wait: 400 * time.Millisecond} }

// Pause just waits.
func Pause(d time.Duration) Step { return Step{Wait: d} }

// Key sends a key press; s is what tea.KeyMsg.String() prints ("ctrl+p", "a").
func Key(s string) Step { return Step{Msg: KeyMsg(s), Wait: 300 * time.Millisecond} }

// Hover moves the mouse (no button) to cell x, y.
func Hover(x, y int) Step { return Step{Msg: motion(x, y), Wait: 300 * time.Millisecond} }

// HoverText moves the mouse onto the first cell showing text in the current
// frame, so a scene works at any terminal size.
func HoverText(text string) Step {
	return Step{Wait: 300 * time.Millisecond, Build: func(frame string) (tea.Msg, error) {
		for y, line := range strings.Split(StripANSI(frame), "\n") {
			if i := strings.Index(line, text); i >= 0 {
				return motion(len([]rune(line[:i])), y), nil
			}
		}
		return nil, fmt.Errorf("hover: %q not on screen", text)
	}}
}

// HoverLastText is HoverText searching from the bottom row up (the
// status bar first).
func HoverLastText(text string) Step {
	return Step{Wait: 300 * time.Millisecond, Build: func(frame string) (tea.Msg, error) {
		lines := strings.Split(StripANSI(frame), "\n")
		for y := len(lines) - 1; y >= 0; y-- {
			if i := strings.Index(lines[y], text); i >= 0 {
				return motion(len([]rune(lines[y][:i])), y), nil
			}
		}
		return nil, fmt.Errorf("hover: %q not on screen", text)
	}}
}

func motion(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone}
}

// Options tune Run.
type Options struct {
	// Settle waits until the frame stops changing after start-up, after each
	// step and at the end, instead of relying on the fixed waits alone.
	Settle bool
	// FinalQuiet is how long the last frame must stay unchanged with Settle
	// (default 500ms). Background work such as the explorer's git badges can
	// land a while after the last redraw, so tests use a longer window.
	FinalQuiet time.Duration
	// Quiet drops cursor-blink and toast-expiry ticks so the frame can
	// settle and toasts stay on screen.
	Quiet bool
	// OnFrame, when set, gets the settled frame after start-up and after
	// every step, with how long a recording should show it (Step.Hold).
	OnFrame func(frame string, hold time.Duration)
}

// quietMsgs are message types dropped in Quiet mode (by %T).
var quietMsgs = map[string]bool{
	"findbar.blinkMsg": true,
	"recents.blinkMsg": true,
	"toast.TickMsg":    true,
}

// recorder wraps the app model and keeps the last frame View produced.
type recorder struct {
	inner tea.Model
	quiet bool
	mu    *sync.Mutex
	last  *string
	gen   *int
}

func (r recorder) Init() tea.Cmd { return r.inner.Init() }

func (r recorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if r.quiet && quietMsgs[fmt.Sprintf("%T", msg)] {
		return r, nil
	}
	var cmd tea.Cmd
	r.inner, cmd = r.inner.Update(msg)
	return r, cmd
}

func (r recorder) View() string {
	v := r.inner.View()
	r.mu.Lock()
	if v != *r.last {
		*r.last = v
		*r.gen++
	}
	r.mu.Unlock()
	return v
}

// Run starts a fresh app in a private config dir (XDG_CONFIG_HOME is set
// for the process), plays the scene and returns the last frame. It changes
// process-wide state (environment, theme globals), so scenes must not run
// concurrently in one process.
func Run(sc Scene, w, h int, opt Options) (string, error) {
	cfg, err := os.MkdirTemp("", "termocode-shots-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(cfg)
	_ = os.MkdirAll(filepath.Join(cfg, "termocode"), 0o755)
	th := sc.Theme
	if th == "" {
		th = "vscode-dark-plus"
	}
	icons := sc.Icons
	if icons == "" {
		icons = "nerd"
	}
	// The demo has finished the first-run setup card, unless the scene
	// is about the card.
	conf := fmt.Sprintf(`{"theme": %q, "icons": %q, "animations": "off", "images": "blocks", "setup_done": %v}`, th, icons, !sc.SetupCard)
	_ = os.WriteFile(filepath.Join(cfg, "termocode", "config.json"), []byte(conf), 0o644)
	writeOpened(filepath.Join(cfg, "termocode", "recents.json"), sc.Recents)
	writeOpened(filepath.Join(cfg, "termocode", "workspaces.json"), sc.Workspaces)
	os.Setenv("XDG_CONFIG_HOME", cfg)
	// The secret store must not touch the real OS keyring for a demo.
	os.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	keyring.MockInit()
	if home, err := os.UserHomeDir(); err == nil && os.Getenv("TERMOCODE_PLUGINS_DIR") == "" {
		if p := filepath.Join(home, ".local", "share", "termocode", "plugins"); dirExists(p) {
			os.Setenv("TERMOCODE_PLUGINS_DIR", p)
		}
	}
	// The start page writes paths under the home directory as "~/…". With
	// seeded lists the working directory's parent plays the home, so the
	// shots read "~/termocode/…" like on a real machine (the golden child
	// has that layout already).
	if len(sc.Recents)+len(sc.Workspaces) > 0 {
		if wd, err := os.Getwd(); err == nil {
			old := os.Getenv("HOME")
			os.Setenv("HOME", filepath.Dir(wd))
			defer os.Setenv("HOME", old)
		}
	}
	for k, v := range sc.Env {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	switch icons {
	case "unicode":
		theme.CurrentIconMode = theme.IconModeUnicode
	case "ascii":
		theme.CurrentIconMode = theme.IconModeASCII
	default:
		theme.CurrentIconMode = theme.IconModeNerdFont
	}

	var mu sync.Mutex
	last, gen := "", 0
	rec := recorder{inner: app.New(), quiet: opt.Quiet, mu: &mu, last: &last, gen: &gen}
	p := tea.NewProgram(rec, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutSignalHandler())
	done := make(chan struct{})
	go func() {
		_, _ = p.Run()
		close(done)
	}()
	defer func() {
		p.Kill()
		<-done
	}()
	frame := func() (string, int) {
		mu.Lock()
		defer mu.Unlock()
		return last, gen
	}
	const stepQuiet = 500 * time.Millisecond
	finalQuiet := opt.FinalQuiet
	if finalQuiet <= 0 {
		finalQuiet = stepQuiet
	}
	// settle waits until no new frame has been drawn for quiet.
	settle := func(min, quiet time.Duration) error {
		time.Sleep(min)
		if !opt.Settle {
			return nil
		}
		const limit = 20 * time.Second
		_, g := frame()
		since, start := time.Now(), time.Now()
		for time.Since(since) < quiet {
			if time.Since(start) > limit {
				return errors.New("screen did not settle (something keeps redrawing)")
			}
			time.Sleep(25 * time.Millisecond)
			if _, g2 := frame(); g2 != g {
				g, since = g2, time.Now()
			}
		}
		return nil
	}

	record := func(hold time.Duration) {
		if opt.OnFrame == nil {
			return
		}
		if hold <= 0 {
			hold = defaultHold
		}
		f, _ := frame()
		opt.OnFrame(f, hold)
	}
	p.Send(tea.WindowSizeMsg{Width: w, Height: h})
	if err := settle(1500*time.Millisecond, stepQuiet); err != nil {
		return "", fmt.Errorf("start-up: %w", err)
	}
	record(0)
	for i, st := range sc.Steps {
		msg := st.Msg
		if st.Build != nil {
			f, _ := frame()
			if msg, err = st.Build(f); err != nil {
				return "", fmt.Errorf("step %d: %w", i+1, err)
			}
		}
		if msg != nil {
			p.Send(msg)
		}
		if err := settle(st.Wait, stepQuiet); err != nil {
			return "", fmt.Errorf("step %d: %w", i+1, err)
		}
		record(st.Hold)
	}
	// One more resize forces a fresh View with the final state.
	if !sc.KeepHover {
		p.Send(tea.WindowSizeMsg{Width: w, Height: h})
	}
	if err := settle(300*time.Millisecond, finalQuiet); err != nil {
		return "", fmt.Errorf("final frame: %w", err)
	}
	f, _ := frame()
	return f, nil
}

var ansiRE = regexp.MustCompile(`\x1b(\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[@-Z\\-_])`)

// StripANSI removes escape sequences (SGR colours, OSC hyperlinks, …).
func StripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// KeyMsg builds the tea.KeyMsg whose String() is s ("ctrl+p", "alt+x", "a").
func KeyMsg(s string) tea.KeyMsg {
	for k := tea.KeyType(-200); k < 200; k++ {
		if k == tea.KeyRunes {
			continue
		}
		if (tea.KeyMsg{Type: k}).String() == s {
			return tea.KeyMsg{Type: k}
		}
	}
	if strings.HasPrefix(s, "alt+") {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s[4:]), Alt: true}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
