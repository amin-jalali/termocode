package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/amin-jalali/termocode/internal/app"
	// Must init before bubbletea: no 5 s OSC 11 wait (see package doc).
	_ "github.com/amin-jalali/termocode/internal/darkbg"
	"github.com/amin-jalali/termocode/internal/setup"
	"github.com/amin-jalali/termocode/internal/termfont"
	"github.com/amin-jalali/termocode/internal/termimg"
	"github.com/amin-jalali/termocode/internal/theme"
)

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
func probeTerminal() {
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
	caps := termimg.Caps{Protocol: termimg.Blocks}
	switch termimg.ParseMode(disp.Images) {
	case termimg.ModeOff, termimg.ModeBlocks:
	default:
		caps = termimg.Probe(os.Stdin, os.Stdout, 250*time.Millisecond, os.Getenv)
	}
	app.SetTerminalCaps(caps, unicodeOK, nerd)
	app.ApplyDisplaySettings(disp)
}

func main() {
	args, noSession := extractFlag(os.Args[1:], "--no-session")
	if noSession {
		_ = os.Setenv(envNoSession, "1")
	}
	if len(args) == 2 && args[0] == serverArg {
		os.Exit(runServer(args[1]))
	}
	if len(args) > 0 {
		switch args[0] {
		case "sessions":
			os.Exit(cmdSessions())
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
	// Record this workspace in the recents-list so "Open Recent Workspace..."
	// remembers where we were. Best-effort.
	if cwd, err := os.Getwd(); err == nil {
		app.PushWorkspace(cwd)
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
		if cwd, err := os.Getwd(); err == nil {
			os.Exit(runClient(cwd, true))
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
	fmt.Println("  termocode setup            install JetBrainsMono Nerd Font and check prerequisites")
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
		app.HotExitFlush(2 * time.Second)
		p.Quit()
	}()
}

func runTUI() {
	// Probe the host terminal for icon-glyph cell widths BEFORE Bubble
	// Tea grabs the TTY into raw/altscreen mode. The probe writes a few
	// CSI sequences and reads cursor-position reports; doing this after
	// tea.NewProgram would race with Bubble Tea's own input reader.
	// Honours TERMOCODE_ICON_MODE (manual override) and NO_COLOR.
	probeTerminal()

	font := applyFontDelta(fontDelta())
	defer func() {
		// Restore the terminal's default cursor shape — we set it via
		// DECSCUSR `\x1b[<n> q` while running.
		_, _ = os.Stdout.WriteString("\x1b[0 q")
		// Erase Saved Lines (CSI 3 J) — drops the host terminal's
		// scrollback buffer on the way out so the user doesn't return to
		// a session with the editor's transient state still scrollable.
		// Mirrors the pre-Run write below (must run in BOTH places —
		// pre-Run is on the primary buffer, this defer fires AFTER the
		// alt-screen flip-back, which lands us on primary again).
		_, _ = os.Stdout.WriteString("\x1b[3J")
		// Restore the terminal's default background (overridden below). OSC 111.
		_, _ = os.Stdout.WriteString("\x1b]111\x07")
		// Undo the font change (if the terminal made one).
		_, _ = os.Stdout.WriteString(font.Restore())
		if r := recover(); r != nil {
			app.HotExitFlush(2 * time.Second)
			fmt.Fprintf(os.Stderr, "termocode panic: %v\n", r)
			os.Exit(1)
		}
	}()

	// CSI 3 J on the PRIMARY buffer — clears the host terminal's
	// scrollback so VTE-family emulators (GNOME Terminal, …) hide the
	// scrollbar overlay while we're inside alt-screen. Must be written
	// BEFORE tea.NewProgram → Run() emits `\x1b[?1049h` (the alt-screen
	// switch); after the switch any erase escape lands on alt, which is
	// already empty.
	_, _ = os.Stdout.WriteString("\x1b[3J")

	// The window height is rarely an exact multiple of the cell height, so the
	// terminal fills the ~1-row leftover strip at the bottom with its OWN
	// default background (a jarring purple on Ubuntu/GNOME). We can't paint
	// there (it's below the character grid), but OSC 11 sets the terminal's
	// default bg, so the strip blends in instead of flashing purple. We use the
	// status-bar bg (#121212) since the status bar spans most of the bottom
	// width. A single colour can't match every region — the strip fully
	// vanishes only when the window is sized to whole rows. Reset via OSC 111
	// in the defer above.
	_, _ = os.Stdout.WriteString("\x1b]11;#121212\x07")

	p := tea.NewProgram(
		app.New(),
		tea.WithAltScreen(),
		// SIGHUP / SIGTERM / SIGINT are ours (watchHangup): unsaved
		// buffers go to the hot-exit backup before we quit.
		tea.WithoutSignalHandler(),
		// WithMouseCellMotion (xterm 1002) — button-event mode.
		//
		// ROOT-CAUSE FIX for "mouse SGR codes leak into picker/prompt
		// input fields as KeyRunes":
		//
		// We previously used WithMouseAllMotion (1003) which forwards
		// EVERY pixel of cursor motion, even with no button held. At
		// high motion rates the SGR sequence (e.g. "\x1b[<35;70;19M")
		// can be split across multiple TTY Read() syscalls. Bubble
		// Tea v1.3.10's input parser only waits for "more data" when
		// the read filled its 256-byte buffer; if the buffer was a
		// short read AND the trailing `M`/`m` of the mouse SGR is in
		// a NEXT chunk, the parser falls through to the rune scanner
		// and emits each fragment ("[", "<35;70;", "19M") as a
		// KeyRunes event. Those then leak into focused text inputs
		// (search box of the picker, prompt input, etc.).
		//
		// 1002 sends events ONLY on press / release / drag (motion
		// with button held) / wheel — i.e. orders of magnitude fewer
		// events than 1003. The high-rate stream that triggers the
		// fragmentation race goes away. Drag-select still works
		// (drag = motion + held button, included in 1002), and
		// neither nvim nor any termocode UI relies on no-button
		// motion (every MouseMotion handler in the codebase already
		// gates on `mouseDown`/`dragKind != none`).
		//
		// Note: a defensive sanitiser still lives in
		// internal/picker/model.go and internal/prompt/model.go in
		// case the fragmentation race somehow triggers under 1002
		// too — it's a belt-and-suspenders.
		// All-motion (xterm 1003) so the UI can react to plain hover. The
		// Update loop downgrades to button-only (cell motion) whenever a text
		// input is focused, which is the only place the 1003 SGR-fragmentation
		// leak (see the note above) can corrupt typed input.
		tea.WithMouseAllMotion(),
	)
	watchRepaint(p)
	watchHangup(p)
	if _, err := p.Run(); err != nil {
		// A crash or a lost terminal: keep the unsaved buffers.
		app.HotExitFlush(2 * time.Second)
		_, _ = os.Stdout.WriteString("\x1b]111\x07" + font.Restore()) // os.Exit skips the defer
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
