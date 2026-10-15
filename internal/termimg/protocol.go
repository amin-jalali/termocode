package termimg

import (
	"strings"
)

// Protocol is the image drawing back end.
type Protocol int

const (
	// None draws no image at all.
	None Protocol = iota
	// Blocks draws half-block "▀" cells (text only).
	Blocks
	// Sixel uses the DEC sixel graphics format.
	Sixel
	// Kitty uses the kitty graphics protocol.
	Kitty
)

func (p Protocol) String() string {
	switch p {
	case None:
		return "off"
	case Blocks:
		return "blocks"
	case Sixel:
		return "sixel"
	case Kitty:
		return "kitty"
	}
	return "unknown"
}

// Mode is the user setting "images".
type Mode string

// Setting values.
const (
	ModeAuto   Mode = "auto"
	ModeKitty  Mode = "kitty"
	ModeSixel  Mode = "sixel"
	ModeBlocks Mode = "blocks"
	ModeOff    Mode = "off"
)

// ParseMode normalises a setting value; unknown values mean auto.
func ParseMode(s string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(s))) {
	case ModeKitty:
		return ModeKitty
	case ModeSixel:
		return ModeSixel
	case ModeBlocks:
		return ModeBlocks
	case ModeOff:
		return ModeOff
	}
	return ModeAuto
}

// Caps is what the terminal supports, as found by Probe.
type Caps struct {
	Protocol    Protocol
	CellW       int // cell size in pixels (0 = unknown)
	CellH       int
	InTmux      bool
	Passthrough bool   // tmux allow-passthrough is on
	Reason      string // short human text for the Doctor
	// KittyKeys: the terminal answered the kitty keyboard query (CSI ? u),
	// so termocode may turn the protocol on (ctrl+shift+o, ctrl+. …).
	KittyKeys bool
}

// CellSize returns the cell size in pixels, with a sane default when the
// terminal did not answer the query.
func (c Caps) CellSize() (w, h int) {
	w, h = c.CellW, c.CellH
	if w <= 0 || h <= 0 {
		return 10, 20
	}
	return w, h
}

// DetectEnv guesses the protocol from environment variables. sure=false
// means the variables say nothing useful (Blocks is returned then).
func DetectEnv(getenv func(string) string) (Protocol, bool) {
	term := strings.ToLower(getenv("TERM"))
	prog := strings.ToLower(getenv("TERM_PROGRAM"))
	switch {
	// TERM_PROGRAM first: a nested terminal (VS Code started from a kitty
	// shell) inherits the parent's KITTY_WINDOW_ID / WT_SESSION.
	case prog == "vscode":
		return Blocks, true
	case getenv("KITTY_WINDOW_ID") != "" || term == "xterm-kitty":
		return Kitty, true
	case prog == "ghostty" || getenv("GHOSTTY_RESOURCES_DIR") != "" || term == "xterm-ghostty":
		return Kitty, true
	case prog == "wezterm" || getenv("WEZTERM_EXECUTABLE") != "":
		return Kitty, true
	case prog == "iterm.app":
		return Sixel, true
	case strings.HasPrefix(term, "foot"):
		return Sixel, true
	case getenv("WT_SESSION") != "":
		return Sixel, true
	}
	return Blocks, false
}

// Resolve turns the user setting plus the detected caps into the protocol to
// use. Graphics inside tmux without passthrough always degrade to Blocks.
// truecolor is accepted for future use: half blocks degrade through the
// color profile on their own.
func Resolve(mode Mode, caps Caps, truecolor bool) Protocol {
	_ = truecolor
	blockedByTmux := caps.InTmux && !caps.Passthrough
	switch mode {
	case ModeOff:
		return None
	case ModeBlocks:
		return Blocks
	case ModeKitty:
		if blockedByTmux {
			return Blocks
		}
		return Kitty
	case ModeSixel:
		if blockedByTmux {
			return Blocks
		}
		return Sixel
	}
	if blockedByTmux {
		return Blocks
	}
	if caps.Protocol == Kitty || caps.Protocol == Sixel {
		return caps.Protocol
	}
	return Blocks
}

// Wrap wraps seq in a tmux passthrough DCS when needed.
func Wrap(seq string, caps Caps) string {
	if seq == "" || !caps.InTmux || !caps.Passthrough {
		return seq
	}
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}
