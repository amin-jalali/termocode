package main

import (
	"fmt"
	"os"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"termocode/internal/app"
	"termocode/internal/setup"
	"termocode/internal/theme"
)

// fontDelta returns the number of font-points by which we ask the host
// terminal to shrink (or grow) its font on launch. Negative = smaller.
// Override with TERMOCODE_FONT_DELTA (set =0 to disable).
func fontDelta() int {
	if v := os.Getenv("TERMOCODE_FONT_DELTA"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return -2
}

// applyFontDelta emits the OSC 1337 ChangeFontSize escape unconditionally
// when delta ≠ 0. The format is "ESC ] 1337 ; ChangeFontSize=±N BEL".
// Terminals that recognise it (iTerm2, WezTerm, Hyper, VSCode integrated
// terminal) honour the request; others silently swallow the OSC sequence
// without printing it (per VT100 spec). Detection-by-env-var doesn't
// work over SSH because TERM_PROGRAM isn't usually forwarded, so we just
// emit and let the host terminal decide.
func applyFontDelta(delta int) {
	if delta == 0 {
		return
	}
	sign := "+"
	if delta < 0 {
		sign = "" // %d emits its own minus sign for negative values
	}
	_, _ = os.Stdout.WriteString(fmt.Sprintf("\x1b]1337;ChangeFontSize=%s%d\x07", sign, delta))
}

func init() {
	// Force truecolor SGR output. Many terminals support 24-bit color but
	// don't advertise it via $COLORTERM; without this lipgloss/termenv
	// auto-detection falls back to ANSI-256 and dark-gray shades collapse
	// onto adjacent palette indices that some terminals render identically.
	lipgloss.SetColorProfile(termenv.TrueColor)
	theme.CurrentIconMode = theme.DetectIconMode()
}

func main() {
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
	runTUI()
}

func printUsage() {
	fmt.Println("termocode — terminal IDE")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  termocode          launch the editor")
	fmt.Println("  termocode setup    install JetBrainsMono Nerd Font and check prerequisites")
	fmt.Println("  termocode -h       show this help")
}

func runTUI() {
	// Probe the host terminal for icon-glyph cell widths BEFORE Bubble
	// Tea grabs the TTY into raw/altscreen mode. The probe writes a few
	// CSI sequences and reads cursor-position reports; doing this after
	// tea.NewProgram would race with Bubble Tea's own input reader.
	// Honours TERMOCODE_ICON_MODE (manual override) and NO_COLOR.
	theme.CurrentIconMode = theme.Probe()

	delta := fontDelta()
	applyFontDelta(delta)
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
		// Restore font size by emitting the inverse delta.
		applyFontDelta(-delta)
		if r := recover(); r != nil {
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
	if _, err := p.Run(); err != nil {
		_, _ = os.Stdout.WriteString("\x1b]111\x07") // os.Exit skips the defer
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
