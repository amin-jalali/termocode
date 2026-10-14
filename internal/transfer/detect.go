package transfer

import (
	"strings"
)

// Method is a way to move files through the terminal.
type Method string

// Methods. None means: show the scp command (and trzsz when installed).
// Local means termocode runs on the user's own computer: a download is a
// copy into the Downloads folder, an upload a copy of chosen files.
const (
	None   Method = ""
	Kitty  Method = "kitty"
	ITerm2 Method = "iterm2"
	Local  Method = "local"
)

// EnvOverride forces a method: kitty, iterm2, local, off (or auto).
const EnvOverride = "TERMOCODE_TRANSFER"

// Support is what this terminal can do.
type Support struct {
	Terminal string // kitty, iTerm2, WezTerm, Ghostty, "" (unknown)
	Download Method
	Upload   Method
	Why      string // short reason when a method is None
	Remote   bool   // termocode runs over SSH (IsRemote)
}

// sshVars are set by sshd for the session it starts.
var sshVars = []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"}

// IsRemote reports whether termocode runs over SSH, so the user's
// computer is another machine. In a persistent session the UI keeps the
// SSH variables of the client that attached last (session.CapsEnvKeys),
// not those of the client that started the session. A container or WSL
// counts as local: its files are on the user's computer.
func IsRemote(getenv func(string) string) bool {
	for _, k := range sshVars {
		if strings.TrimSpace(getenv(k)) != "" {
			return true
		}
	}
	return false
}

// Detect picks the methods from the environment. Locally (IsRemote is
// false) both are Local, whatever the terminal. Over SSH the client
// sends TERM and (by default) LC_*: kitty sets TERM=xterm-kitty, iTerm2
// sets LC_TERMINAL=iTerm2. Inside tmux the terminal's answers do not
// reach the app, so nothing is used there unless forced.
func Detect(getenv func(string) string) Support {
	term := strings.ToLower(getenv("TERM"))
	prog := strings.ToLower(getenv("TERM_PROGRAM"))
	s := Support{Remote: IsRemote(getenv)}
	switch {
	case term == "xterm-kitty" || getenv("KITTY_WINDOW_ID") != "":
		s.Terminal = "kitty"
	case prog == "iterm.app" || strings.EqualFold(getenv("LC_TERMINAL"), "iTerm2"):
		s.Terminal = "iTerm2"
	case prog == "wezterm" || getenv("WEZTERM_EXECUTABLE") != "":
		s.Terminal = "WezTerm"
	case prog == "ghostty" || getenv("GHOSTTY_RESOURCES_DIR") != "" || term == "xterm-ghostty":
		s.Terminal = "Ghostty"
	}
	switch strings.ToLower(strings.TrimSpace(getenv(EnvOverride))) {
	case "kitty":
		s.Download, s.Upload = Kitty, Kitty
		return s
	case "iterm2", "iterm":
		s.Download, s.Upload = ITerm2, ITerm2
		return s
	case "local":
		s.Download, s.Upload = Local, Local
		return s
	case "off", "none", "scp":
		s.Why = EnvOverride + " is off"
		return s
	}
	if !s.Remote {
		s.Download, s.Upload = Local, Local
		return s
	}
	if getenv("TMUX") != "" || strings.HasPrefix(term, "tmux") || strings.HasPrefix(term, "screen") {
		s.Why = "inside tmux / screen the terminal's answers do not reach termocode"
		return s
	}
	switch s.Terminal {
	case "kitty":
		s.Download, s.Upload = Kitty, Kitty
	case "iTerm2":
		s.Download, s.Upload = ITerm2, ITerm2
	case "":
		s.Why = "this terminal has no known file transfer"
	default:
		s.Why = s.Terminal + " has no file transfer that termocode can use"
	}
	return s
}
