package rtl

// Mode is who lays out right-to-left text: the rtl setting.
//
// What the terminals do (checked 2026-10):
//
//   - VTE (GNOME Terminal, Ptyxis, Tilix, …; 0.76 here): BiDi and Arabic
//     shaping are on by default, in "implicit" mode: each row is reordered
//     as one LTR paragraph. That is fine for a shell, but a TUI row holds
//     several panes, so text moves across pane borders, and text termocode
//     already reordered would be reversed again. VTE honours BDSM: CSI 8 l
//     (explicit mode, the application lays out) and CSI 8 h (implicit, the
//     default).
//   - WezTerm: bidi_enabled = false by default; when on, CSI 8 l / 8 h turn
//     it off / on too.
//   - kitty, Alacritty, xterm, foot, Ghostty, Windows Terminal: no
//     reordering, no shaping.
//   - Konsole ("Bi-Directional text rendering", on by default) and mlterm
//     reorder and shape and do not know BDSM.
//
// So termocode does the layout itself and asks the terminal for explicit
// mode while it runs; only on Konsole and mlterm (auto) it leaves the
// text to the terminal.
type Mode int8

const (
	// Termocode: shape + reorder in termocode, terminal in explicit mode.
	Termocode Mode = iota
	// Terminal: send logical text; the terminal reorders and shapes.
	Terminal
	// Off: send logical text, change nothing.
	Off
)

// Choices are the values of the rtl setting (first = default).
var Choices = []string{"auto", "termocode", "terminal", "off"}

func (m Mode) String() string {
	switch m {
	case Terminal:
		return "terminal"
	case Off:
		return "off"
	}
	return "termocode"
}

// Resolve turns the rtl setting into a Mode. auto (or an unknown value)
// is Terminal on terminals that always reorder by themselves (Konsole,
// mlterm), else Termocode.
func Resolve(setting string, getenv func(string) string) Mode {
	switch setting {
	case "termocode":
		return Termocode
	case "terminal":
		return Terminal
	case "off":
		return Off
	}
	if getenv != nil && (getenv("KONSOLE_VERSION") != "" || getenv("MLTERM") != "") {
		return Terminal
	}
	return Termocode
}

// Escape sequences of BDSM (ECMA-48 mode 8, terminal-wg BiDi).
const (
	// ExplicitMode tells the terminal not to reorder: the application does.
	ExplicitMode = "\x1b[8l"
	// ImplicitMode is the terminal default, restored on exit.
	ImplicitMode = "\x1b[8h"
)
