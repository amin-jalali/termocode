package termcaps

import "strings"

// Profile is what the environment says about the host terminal: its
// name, the layer between (tmux, screen, zellij), whether termocode runs
// over SSH, and the static quirks of that terminal (the table below).
// The terminal's own answers (the startup probe) beat these guesses.
type Profile struct {
	Name   string // a key of profiles ("unknown" when nothing matched)
	Layer  string // "tmux", "screen", "zellij" or ""
	SSH    bool
	Quirks Quirks
}

// Quirks are the static facts of one terminal (terminal-compat §4).
// Values are short words: "yes" / "no" / "query" / "unknown" …
type Quirks struct {
	Keys   string // keyboard: "kitty", "legacy", "kitty-if-answered", "modifyOtherKeys"
	OSC52  string // clipboard write: "yes", "no", "write", "unknown"
	Images string // "kitty", "sixel", "blocks", "query", "off"
	Hover  string // pointer motion (1003): "yes", "no", "partial"
	Sync   string // mode 2026: "yes", "no", "query"
	Nerd   string // where the font is known: "builtin", "gsettings", "toml", "ini", "font.ttf", "ascii", "unknown"
	Bidi   string // "self" (termocode reorders), "bdsm", "terminal" (it reorders itself), "none"
	// HostKeys are keys the terminal (or the IDE around it) keeps for
	// itself by default.
	HostKeys []string
}

// ProfileNames lists every name FromEnv returns.
var ProfileNames = []string{
	"kitty", "ghostty", "wezterm", "foot", "alacritty", "vte", "konsole",
	"xterm", "iterm2", "apple", "wt", "vscode", "jetbrains", "warp", "wave",
	"termux", "linux", "unknown",
}

// profiles is the terminal table of the terminal-compat plan (§4).
var profiles = map[string]Quirks{
	"kitty":     {Keys: "kitty", OSC52: "yes", Images: "kitty", Hover: "yes", Sync: "yes", Nerd: "builtin", Bidi: "self", HostKeys: []string{"Ctrl+Shift+T", "Ctrl+Shift+W", "Ctrl+Shift+E", "Ctrl+Shift+V", "Ctrl+Shift+N"}},
	"ghostty":   {Keys: "kitty", OSC52: "yes", Images: "kitty", Hover: "yes", Sync: "yes", Nerd: "builtin", Bidi: "self"},
	"wezterm":   {Keys: "kitty-if-answered", OSC52: "yes", Images: "kitty", Hover: "yes", Sync: "yes", Nerd: "builtin", Bidi: "bdsm"},
	"foot":      {Keys: "kitty", OSC52: "yes", Images: "sixel", Hover: "yes", Sync: "yes", Nerd: "ini", Bidi: "self"},
	"alacritty": {Keys: "kitty", OSC52: "yes", Images: "blocks", Hover: "yes", Sync: "yes", Nerd: "toml", Bidi: "self"},
	"vte":       {Keys: "legacy", OSC52: "no", Images: "blocks", Hover: "yes", Sync: "query", Nerd: "gsettings", Bidi: "bdsm", HostKeys: []string{"Ctrl+Shift+C", "Ctrl+Shift+V", "Ctrl+Shift+T", "Ctrl+Shift+W", "Ctrl+Shift+N", "Ctrl+Shift+Q", "Ctrl+Shift+F"}},
	"konsole":   {Keys: "legacy", OSC52: "write", Images: "blocks", Hover: "yes", Sync: "query", Nerd: "unknown", Bidi: "terminal", HostKeys: []string{"Ctrl+Shift+T", "Ctrl+Shift+W", "Ctrl+Shift+N"}},
	"xterm":     {Keys: "modifyOtherKeys", OSC52: "no", Images: "blocks", Hover: "yes", Sync: "no", Nerd: "unknown", Bidi: "self"},
	"iterm2":    {Keys: "kitty-if-answered", OSC52: "yes", Images: "sixel", Hover: "yes", Sync: "query", Nerd: "unknown", Bidi: "self"},
	"apple":     {Keys: "legacy", OSC52: "no", Images: "blocks", Hover: "yes", Sync: "no", Nerd: "unknown", Bidi: "self"},
	"wt":        {Keys: "kitty-if-answered", OSC52: "yes", Images: "query", Hover: "yes", Sync: "query", Nerd: "unknown", Bidi: "self", HostKeys: []string{"Ctrl+Shift+C", "Ctrl+Shift+V", "Ctrl+Shift+T", "Ctrl+Shift+W", "Ctrl+Shift+D", "Ctrl+Shift+F", "Ctrl+Shift+P"}},
	"vscode":    {Keys: "legacy", OSC52: "yes", Images: "query", Hover: "yes", Sync: "no", Nerd: "unknown", Bidi: "self", HostKeys: []string{"F1", "Ctrl+P", "Ctrl+Shift+P", "Ctrl+`", "Ctrl+B", "Ctrl+J"}},
	"jetbrains": {Keys: "legacy", OSC52: "unknown", Images: "blocks", Hover: "yes", Sync: "no", Nerd: "unknown", Bidi: "self", HostKeys: []string{"F1"}},
	"warp":      {Keys: "kitty-if-answered", OSC52: "yes", Images: "query", Hover: "yes", Sync: "query", Nerd: "unknown", Bidi: "self"},
	"wave":      {Keys: "legacy", OSC52: "yes", Images: "query", Hover: "yes", Sync: "no", Nerd: "unknown", Bidi: "self"},
	"termux":    {Keys: "legacy", OSC52: "yes", Images: "blocks", Hover: "yes", Sync: "no", Nerd: "font.ttf", Bidi: "self"},
	"linux":     {Keys: "legacy", OSC52: "no", Images: "off", Hover: "no", Sync: "no", Nerd: "ascii", Bidi: "none"},
	"unknown":   {Keys: "kitty-if-answered", OSC52: "unknown", Images: "query", Hover: "yes", Sync: "query", Nerd: "unknown", Bidi: "self"},
}

// FromEnv names the terminal from the environment. Order of trust:
// TERM_PROGRAM and TERMINAL_EMULATOR (set by the terminal that runs the
// shell, so a nested terminal's own value wins), then the variables a
// terminal exports for itself, then TERM.
func FromEnv(getenv func(string) string) Profile {
	p := Profile{Name: name(getenv), Layer: Layer(getenv), SSH: SSH(getenv)}
	p.Quirks = profiles[p.Name]
	return p
}

func name(getenv func(string) string) string {
	term := getenv("TERM")
	switch strings.ToLower(getenv("TERM_PROGRAM")) {
	case "vscode":
		return "vscode"
	case "ghostty":
		return "ghostty"
	case "wezterm":
		return "wezterm"
	case "iterm.app":
		return "iterm2"
	case "apple_terminal":
		return "apple"
	case "warpterminal":
		return "warp"
	case "waveterm":
		return "wave"
	}
	switch {
	case strings.HasPrefix(getenv("TERMINAL_EMULATOR"), "JetBrains"):
		return "jetbrains"
	case getenv("KITTY_WINDOW_ID") != "" || term == "xterm-kitty":
		return "kitty"
	case term == "xterm-ghostty" || getenv("GHOSTTY_RESOURCES_DIR") != "":
		return "ghostty"
	case getenv("WEZTERM_EXECUTABLE") != "":
		return "wezterm"
	case getenv("LC_TERMINAL") == "iTerm2":
		return "iterm2"
	case getenv("VTE_VERSION") != "":
		return "vte"
	case getenv("KONSOLE_VERSION") != "":
		return "konsole"
	case getenv("WT_SESSION") != "":
		return "wt"
	case getenv("XTERM_VERSION") != "":
		return "xterm"
	case getenv("TERMUX_VERSION") != "":
		return "termux"
	case getenv("ALACRITTY_WINDOW_ID") != "" || getenv("ALACRITTY_SOCKET") != "" || term == "alacritty":
		return "alacritty"
	case strings.HasPrefix(term, "foot"):
		return "foot"
	case term == "linux":
		return "linux"
	}
	return "unknown"
}

// SSH reports whether termocode runs over SSH (the variables sshd sets).
func SSH(getenv func(string) string) bool {
	return getenv("SSH_CONNECTION") != "" || getenv("SSH_CLIENT") != "" || getenv("SSH_TTY") != ""
}

// String is the Doctor's "terminal" text: "kitty · in tmux · over SSH".
func (p Profile) String() string {
	s := p.Name
	if p.Layer != "" {
		s += " · in " + p.Layer
	}
	if p.SSH {
		s += " · over SSH"
	}
	return s
}

// OSC52Honoured reports whether the terminal may act on an OSC 52 copy,
// so "Copied (via terminal)" is not a lie (terminal-compat R5). Inside
// tmux or screen the outer terminal decides and the inherited variables
// may be stale, so they count as yes. Any variable of a terminal that
// ignores OSC 52 by default (VTE, macOS Terminal, xterm, the Linux
// console, JetBrains) means no, even when a nested terminal set
// TERM_PROGRAM: the old check, kept as it was. Unknown terminals get the
// benefit of the doubt.
func OSC52Honoured(getenv func(string) string) bool {
	if getenv("TMUX") != "" || getenv("STY") != "" {
		return true
	}
	switch {
	case getenv("VTE_VERSION") != "",
		getenv("TERM_PROGRAM") == "Apple_Terminal",
		getenv("XTERM_VERSION") != "",
		getenv("TERM") == "linux",
		getenv("TERMINAL_EMULATOR") == "JetBrains-JediTerm":
		return false
	}
	return true
}
