package setup

import "strings"

// TerminalHints are Doctor rows for settings of the host terminal that
// termocode cannot change (and never changes itself): the Option key on
// macOS, the palette key in IDE terminals, Konsole's own bidi, the Linux
// console. Only rows for the terminal in getenv are returned.
// (terminal-compat R9, R12, R13, R14.)
func TerminalHints(getenv func(string) string) []Check {
	var out []Check
	add := func(name, detail string) {
		out = append(out, Check{Group: "terminal", Name: name, Status: Warn, Detail: detail})
	}
	prog := getenv("TERM_PROGRAM")
	switch prog {
	case "Apple_Terminal":
		add("Alt key", "macOS Terminal sends letters (å ƒ Ω) for Option: turn on Settings → Profiles → Keyboard → \"Use Option as Meta key\", else Alt+A, Alt+X, Alt+Z and the other Alt shortcuts do not reach termocode")
	case "iTerm.app":
		add("Alt key", "iTerm2 sends letters (å ƒ Ω) for Option by default: set Settings → Profiles → Keys → Left / Right Option key to \"Esc+\", else Alt+A, Alt+X, Alt+Z and the other Alt shortcuts do not reach termocode")
	}
	switch {
	case prog == "vscode":
		add("palette key", "VS Code keeps F1 (and Ctrl+P, Ctrl+Shift+P, Ctrl+`) for itself: open termocode's command palette with Alt+X, or remove F1 from VS Code's terminal.integrated.commandsToSkipShell")
	case strings.HasPrefix(getenv("TERMINAL_EMULATOR"), "JetBrains"):
		add("palette key", "the JetBrains terminal keeps F1 and many Ctrl keys for the IDE: open termocode's command palette with Alt+X, or turn off \"Override IDE shortcuts\" in Settings → Tools → Terminal")
	}
	if getenv("KONSOLE_VERSION") != "" {
		add("right-to-left text", "Konsole reorders Persian / Arabic / Hebrew rows itself, so RTL text in the AI chat can cross pane borders: turn off Profile → Advanced → \"Bi-Directional text rendering\", then set \"rtl\": \"termocode\" in config.json")
	}
	if getenv("TERM") == "linux" {
		add("Linux console", "the Linux console has 16 colours, no mouse and no alternate screen: termocode uses ASCII icons and clears the screen when it quits; a terminal emulator (or SSH from one) gives the full UI")
	}
	return out
}
