package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strconv"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/workspace/localhost"
	// Must init before bubbletea: no 5 s OSC 11 wait (see package doc).
	_ "github.com/amin-jalali/termocode/internal/darkbg"
	"github.com/amin-jalali/termocode/internal/setup"
	"github.com/amin-jalali/termocode/internal/termcaps"
	"github.com/amin-jalali/termocode/internal/termfont"
	"github.com/amin-jalali/termocode/internal/termimg"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/transfer"
)

// writeHeapProfile writes a Go heap profile to $TERMOCODE_MEMPROFILE when
// the UI ends, for finding what holds memory ("go tool pprof -sample_index=
// inuse_space termocode <file>"). Off unless the variable is set.
func writeHeapProfile() {
	path := os.Getenv("TERMOCODE_MEMPROFILE")
	if path == "" {
		return
	}
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	runtime.GC() // up-to-date live heap
	_ = pprof.WriteHeapProfile(f)
}

// tuneGC makes the garbage collector run less often than Go's default
// (GOGC=100). Measured in a 60 s key / wheel / hover soak with the AI
// chat open (Group P4), after the frame caches cut allocations ~6×:
// GOGC 100 → 18-21% CPU, 44 MB RSS; 200 → 16%, 50 MB; 300 → 16%, 56 MB.
// 200 keeps the CPU of 300 for 6 MB less. The soft limit makes GC work
// harder before the heap grows large (the live heap is ~10 MB).
// GOGC / GOMEMLIMIT still win.
func tuneGC() {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(200)
	}
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(128 << 20)
	}
}

// startCPUProfile writes a Go CPU profile to $TERMOCODE_CPUPROFILE while
// the UI runs, for finding slow paths ("go tool pprof termocode <file>").
// Off unless the variable is set; the returned func stops it (safe to
// call twice).
func startCPUProfile() func() {
	path := os.Getenv("TERMOCODE_CPUPROFILE")
	if path == "" {
		return func() {}
	}
	f, err := os.Create(path)
	if err != nil {
		return func() {}
	}
	if pprof.StartCPUProfile(f) != nil {
		_ = f.Close()
		return func() {}
	}
	var once sync.Once
	return func() { once.Do(func() { pprof.StopCPUProfile(); _ = f.Close() }) }
}

// fontDelta returns the number of font-points by which we ask the host
// terminal to shrink (or grow) its font on launch. Negative = smaller.
// TERMOCODE_FONT_DELTA wins, then "font_delta" in config.json (written by
// the Settings UI), then the default 0 (off: only kitty with remote
// control can do it, see internal/termfont).
func fontDelta() int {
	return resolveFontDelta(os.Getenv("TERMOCODE_FONT_DELTA"), app.ConfigFontDelta())
}

// resolveFontDelta is the pure part of fontDelta: env (if it parses), else
// cfg (if set), else 0.
func resolveFontDelta(env string, cfg *int) int {
	if env != "" {
		if n, err := strconv.Atoi(env); err == nil {
			return n
		}
	}
	if cfg != nil {
		return *cfg
	}
	return 0
}

// applyFontDelta asks the terminal for the font change (kitty only; it
// must run before Bubble Tea takes the TTY, it reads kitty's answer) and
// tells the UI, which shows a zoom tip when it was not done.
func applyFontDelta(delta int) termfont.State {
	s := termfont.Change(os.Getenv, delta, func(q string, done func([]byte) bool) []byte {
		return termimg.Query(os.Stdin, os.Stdout, q, 500*time.Millisecond, done)
	})
	app.SetFontResult(delta, s)
	return s
}

func init() {
	// Truecolor SGR output by default: many terminals support 24-bit
	// colour without advertising it via $COLORTERM (it is rarely forwarded
	// over SSH), and lipgloss' own detection would fall back to ANSI-256,
	// collapsing the dark greys. theme.DetectProfile steps down only when
	// the environment says so (TERMOCODE_COLORS, NO_COLOR, TERM=linux …).
	lipgloss.SetColorProfile(theme.DetectProfile(os.Getenv))
	theme.CurrentIconMode = theme.DetectIconMode()
}

// probeTerminal resolves the display settings (icons, animations, images)
// against what the terminal can do. The probes write queries and read the
// replies in raw mode, so this must run before Bubble Tea owns the TTY.
// It reports whether the terminal can take the kitty keyboard protocol
// (see kittyKeyboardOption).
func probeTerminal() (kittyKeys, syncOutput bool) {
	disp := app.ConfigDisplay()
	envIcons := os.Getenv("TERMOCODE_ICON_MODE") != "" || os.Getenv("TERMOCODE_NERD_FONT") == "1"
	nerd := false
	unicodeOK := false
	if !envIcons {
		// auto picks Nerd icons only when the terminal's own font setting
		// is a Nerd Font (GNOME profile, alacritty.toml, foot.ini; cached)
		// or the terminal bundles the symbols; else Unicode, which renders
		// in every modern font. Read even when icons != auto, so switching
		// the setting to auto later has the answer.
		nerd = setup.TerminalNerdFont()
		unicodeOK = true
	}
	caps := startupProbe(termimg.ParseMode(disp.Images), os.Getenv, time.Now())
	app.SetTerminalCaps(caps, unicodeOK, nerd)
	app.ApplyDisplaySettings(disp)
	return caps.KittyKeys && keysWanted(os.Getenv), syncOutputWanted(caps.SyncOutput, os.Getenv)
}

// syncOutputWanted: draw each frame as one (mode 2026, synchronized
// output) when the terminal answered DECRQM for it. TERMOCODE_SYNC=on /
// off overrides the answer (terminal-compat R10).
func syncOutputWanted(answered bool, getenv func(string) string) bool {
	switch getenv("TERMOCODE_SYNC") {
	case "on", "1":
		return true
	case "off", "0":
		return false
	}
	return answered
}

// syncOutputOption turns synchronized output on in the renderer.
func syncOutputOption(on bool) tea.ProgramOption {
	if !on {
		return func(*tea.Program) {}
	}
	return tea.WithSynchronizedOutput()
}

// probeTimeout bounds the startup probe: a terminal that does not answer
// costs this once (then the cache remembers it, internal/termcaps).
const probeTimeout = 250 * time.Millisecond

// startupProbe asks the terminal what it can do (images, cell size, kitty
// keyboard) in one raw-mode round, unless it may not be asked
// (TERMOCODE_PROBE=off, GNU screen, TERM=dumb) or it was silent on an
// earlier run (terminal-compat R3). The answer, or the silence, is kept in
// $XDG_STATE_HOME/termocode/termcaps.json for termcaps.CacheTTL.
func startupProbe(mode termimg.Mode, getenv func(string) string, now time.Time) termimg.Caps {
	images := mode != termimg.ModeOff && mode != termimg.ModeBlocks
	// What the environment alone says (no query is written: no TTY).
	envOnly := func(why string) termimg.Caps {
		c := termimg.Caps{Protocol: termimg.Blocks}
		if images {
			c = termimg.ProbeWith(nil, nil, 0, getenv, termimg.ProbeOpts{})
			c.Reason += " (not probed: " + why + ")"
		}
		return c
	}
	if ok, why := termcaps.ProbeAllowed(getenv); !ok {
		return envOnly(why)
	}
	path, key := termcaps.CachePath(getenv), termcaps.CacheKey(getenv)
	seen, cached := termcaps.Lookup(path, key, now)
	if cached && !seen.Answers {
		return envOnly("the terminal did not answer last time")
	}
	caps := termimg.Caps{Protocol: termimg.Blocks}
	switch {
	case images:
		caps = termimg.ProbeWith(os.Stdin, os.Stdout, probeTimeout, getenv, termimg.ProbeOpts{AnsweredBefore: cached && seen.Answers})
	default:
		caps = termimg.ProbeTerminal(os.Stdin, os.Stdout, probeTimeout, getenv)
	}
	// Only a real round counts (not a TTY or inside tmux: nothing asked).
	if asked(getenv) && (!cached || seen.Answers != caps.Answered || now.Sub(seen.At) > termcaps.CacheTTL/2) {
		termcaps.Remember(path, key, termcaps.Entry{Answers: caps.Answered}, now)
	}
	return caps
}

// asked reports whether the probe really writes to the terminal: both
// ends are a TTY and we are not inside tmux (which has its own path).
func asked(getenv func(string) string) bool {
	return getenv("TMUX") == "" && term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// keysWanted: TERMOCODE_KEYS=legacy keeps the kitty keyboard protocol off.
func keysWanted(getenv func(string) string) bool {
	return getenv("TERMOCODE_KEYS") != "legacy"
}

// kittyKeyboardOption turns on the kitty keyboard protocol ("disambiguate"
// flag) when the terminal answered the query. Then keys that legacy
// terminals cannot send reach termocode: Ctrl+Shift+O, Ctrl+., Ctrl+Tab,
// Shift+Enter … (the patched Bubble Tea parses CSI u; third_party/
// bubbletea/key_csiu.go). Flag 1 only: higher flags change every key.
func kittyKeyboardOption(on bool) tea.ProgramOption {
	if !on {
		return func(*tea.Program) {}
	}
	return tea.WithKittyKeyboard(1)
}

func main() {
	mcp.RunShareIfHelper() // a shared MCP server's broker (internal/mcp/share.go)
	tuneGC()
	args, noSession := extractFlag(os.Args[1:], "--no-session")
	if noSession {
		_ = os.Setenv(envNoSession, "1")
	}
	if exe, err := spawn.Executable(); err == nil {
		app.SetMCPShareExe(exe)
	}
	if len(args) == 2 && args[0] == serverArg {
		os.Exit(runServer(args[1]))
	}
	if len(args) > 0 && isVersionArg(args[0]) {
		fmt.Println("termocode " + versionString())
		return
	}
	if len(args) > 0 {
		switch args[0] {
		case "sessions":
			os.Exit(cmdSessions())
		case "mcp":
			os.Exit(runMCP(args[1:]))
		case "attach", "kill":
			dir := "."
			if len(args) > 1 {
				dir = args[1]
			}
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				fmt.Fprintf(os.Stderr, "termocode: %s is not a folder\n", dir)
				os.Exit(1)
			}
			if args[0] == "attach" {
				os.Exit(runClient(dir, false))
			}
			os.Exit(cmdKill(dir))
		}
	}
	os.Args = append(os.Args[:1], args...)
	// If the user passed a folder as the first arg (and it's not a known
	// subcommand), chdir into it so termocode opens that workspace.
	if len(os.Args) > 1 {
		first := os.Args[1]
		if first != "setup" && first != "-h" && first != "--help" && first != "help" {
			if info, err := os.Stat(first); err == nil && info.IsDir() {
				_ = os.Chdir(first)
				// Drop the path arg so subsequent code (which checks os.Args
				// length for option flags) doesn't see it as a command.
				os.Args = append(os.Args[:1], os.Args[2:]...)
			}
		}
	}
	// The workspace is the folder we are in now (ADR 0018); the app reads
	// its root from here, not from the process folder.
	ws := localhost.Cwd()
	app.SetWorkspace(ws)
	// Record this workspace in the recents-list so "Open Recent Workspace..."
	// remembers where we were. Best-effort.
	if ws.Root != "" {
		app.PushWorkspace(ws.Root)
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "setup":
			os.Exit(setup.Run(os.Args[2:]))
		case "-h", "--help", "help":
			printUsage()
			return
		}
	}
	tty := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
	if sessionsSupported && wantSession(app.ConfigPersistentSession(), noSession, os.Getenv, tty) {
		if ws.Root != "" {
			os.Exit(runClient(ws.Root, true))
		}
	}
	runTUI()
}

func printUsage() {
	fmt.Println("termocode — terminal IDE")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  termocode [dir]            open dir (default: the current folder)")
	fmt.Println("  termocode --no-session     run without a persistent session")
	fmt.Println("  termocode sessions         list the running sessions")
	fmt.Println("  termocode attach [dir]     attach to the session of dir")
	fmt.Println("  termocode kill [dir]       end the session of dir")
	fmt.Println("  termocode mcp [--dir d]    run as an MCP server for an agent CLI (stdio)")
	fmt.Println("  termocode setup            install JetBrainsMono Nerd Font and check prerequisites")
	fmt.Println("  termocode --version        print the version")
	fmt.Println("  termocode -h               show this help")
	fmt.Println()
	fmt.Println("Over SSH the editor runs in a persistent session: when the connection")
	fmt.Println("drops, run termocode again in the same folder to get it back.")
	fmt.Println("Setting persistent_session: auto (on over SSH), on, off.")
}

// watchHangup handles the signals that end termocode without a Quit (the
// terminal hung up, kill, Ctrl+C outside raw mode, `termocode kill`):
// unsaved buffers are written to the hot-exit backup, then the UI quits
// without asking. The next start offers to restore them.
func watchHangup(p *tea.Program) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		app.FlushSession() // the last cursor positions (N1)
		app.HotExitFlush(2 * time.Second)
		p.Quit()
	}()
}

// hostEscapes returns what termocode writes to the host terminal before
// Bubble Tea starts (enter) and after it stops (leave). Only for VTE
// (GNOME Terminal, Tilix …), and never inside tmux / screen, where the
// pane would keep the change:
//
//   - CSI 3 J on the PRIMARY buffer (before the alt-screen switch) clears
//     the scrollback so VTE hides its scrollbar overlay while termocode
//     runs. Elsewhere it would only delete the user's shell history, and
//     it is never sent on the way out.
//   - OSC 11 paints the terminal's default background. The window height
//     is rarely a whole number of rows, and VTE fills the leftover strip
//     under the last row with its own background (purple on Ubuntu);
//     #121212 (the status bar) makes it blend in. leave resets it (OSC 111).
//
// The Linux console (TERM=linux) has no alternate screen: the IDE frame
// would stay on the TTY after quit, so leave clears it (terminal R14).
func hostEscapes(getenv func(string) string) (enter, leave string) {
	if getenv("TERM") == "linux" {
		return "", "\x1b[2J\x1b[H"
	}
	if getenv("VTE_VERSION") == "" || getenv("TMUX") != "" || getenv("STY") != "" {
		return "", ""
	}
	return "\x1b[3J" + "\x1b]11;#121212\x07", "\x1b]111\x07"
}

func runTUI() {
	// Probe the host terminal for icon-glyph cell widths BEFORE Bubble
	// Tea grabs the TTY into raw/altscreen mode. The probe writes a few
	// CSI sequences and reads cursor-position reports; doing this after
	// tea.NewProgram would race with Bubble Tea's own input reader.
	// Honours TERMOCODE_ICON_MODE (manual override) and NO_COLOR.
	kittyKeys, syncOutput := probeTerminal()

	font := applyFontDelta(fontDelta())
	hostEnter, hostLeave := hostEscapes(os.Getenv)
	// Right-to-left text: termocode lays it out, so the terminal must not
	// reorder it again (BDSM explicit mode; internal/rtl). Undone on exit.
	rtlEnter, rtlLeave := app.RTLTerminalModes()
	rtlEnter, rtlLeave = rtlEscapes(rtlEnter, rtlLeave, os.Getenv)
	_, _ = os.Stdout.WriteString(rtlEnter)
	defer func() {
		_, _ = os.Stdout.WriteString(rtlLeave)
		// Restore the terminal's default cursor shape — we set it via
		// DECSCUSR `\x1b[<n> q` while running.
		_, _ = os.Stdout.WriteString("\x1b[0 q")
		// Undo the host background (see hostEscapes).
		_, _ = os.Stdout.WriteString(hostLeave)
		// Undo the font change (if the terminal made one).
		_, _ = os.Stdout.WriteString(font.Restore())
		if r := recover(); r != nil {
			app.FlushSession() // the last cursor positions (N1)
			app.HotExitFlush(2 * time.Second)
			app.StopExtensions() // N6b: no extension process outlives us
			app.StopMCP()        // MCP servers too
			app.StopMCPServer()  // and our own MCP socket
			fmt.Fprintf(os.Stderr, "termocode panic: %v\n", r)
			os.Exit(1)
		}
	}()

	_, _ = os.Stdout.WriteString(hostEnter)

	app.SetMouseHover(app.ResolveMouseHover(app.ConfigMouseHover()))
	app.ThrottleFrames()
	app.SetVersion(versionString()) // sent to process extensions (N6b)
	p := tea.NewProgram(
		app.New(),
		tea.WithAltScreen(),
		kittyKeyboardOption(kittyKeys),
		// Each frame drawn at once where the terminal can (no torn rows on
		// a slow link; the pacing query of framePacingOption follows it).
		syncOutputOption(syncOutput),
		// Over SSH: no run-on after a held key on a slow line.
		framePacingOption(framePacing(os.Getenv)),
		// Downloads with kitty read the terminal's answers: transfer.Input
		// takes them out of the key stream while a download runs and
		// passes every byte through otherwise (internal/transfer).
		inputOption(),
		// SIGHUP / SIGTERM / SIGINT are ours (watchHangup): unsaved
		// buffers go to the hot-exit backup before we quit.
		tea.WithoutSignalHandler(),
		// All-motion (xterm 1003) so the UI can react to plain hover;
		// button-only (1002) with mouse_hover = off. The Update loop
		// switches to 1002 while a text input is open or the window is in
		// the background (internal/app/mouse_hover.go). Split SGR reports
		// are joined and motion bursts merged in the patched input reader
		// (third_party/bubbletea/input_hold.go).
		mouseOption(app.MouseHoverEnabled()),
		// Focus events (xterm 1004): in the background termocode polls
		// git less and stops reading the clipboard (app/clip_watch.go).
		tea.WithReportFocus(),
	)
	watchRepaint(p)
	watchHangup(p)
	stopProfile := startCPUProfile()
	defer stopProfile()
	defer writeHeapProfile()
	if _, err := p.Run(); err != nil {
		stopProfile()
		// A crash or a lost terminal: keep the unsaved buffers.
		app.FlushSession() // the last cursor positions (N1)
		app.HotExitFlush(2 * time.Second)
		app.StopExtensions() // N6b
		app.StopMCP()
		app.StopMCPServer()
		_, _ = os.Stdout.WriteString("\x1b]111\x07" + font.Restore() + rtlLeave) // os.Exit skips the defer
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Quit already wrote it; a hangup (p.Quit from watchHangup) too. A
	// no-op then, but cheap insurance for any other way out of Run.
	app.FlushSession()
	// Process extensions (N6b): shutdown, then kill their process groups.
	app.StopExtensions()
	app.StopMCP()       // MCP servers (and their children)
	app.StopMCPServer() // our own MCP socket (termocode as a server)
}

// rtlEscapes drops the BDSM request on the Linux console, which has no
// bidi at all (terminal R14).
func rtlEscapes(enter, leave string, getenv func(string) string) (string, string) {
	if getenv("TERM") == "linux" {
		return "", ""
	}
	return enter, leave
}

// mouseOption picks the first mouse tracking mode: all motion (hover) or
// button events only (mouse_hover = off).
func mouseOption(hover bool) tea.ProgramOption {
	if hover {
		return tea.WithMouseAllMotion()
	}
	return tea.WithMouseCellMotion()
}

// inputOption is Bubble Tea's input: stdin behind transfer.Input when
// stdin is a terminal (Bubble Tea still sets raw mode on its descriptor),
// else the default (Bubble Tea opens the TTY itself).
func inputOption() tea.ProgramOption {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return func(*tea.Program) {}
	}
	in := transfer.NewInput(os.Stdin)
	transfer.Use(in)
	return tea.WithInput(in)
}
