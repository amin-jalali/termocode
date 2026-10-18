package keymap

import "strings"

// DefaultSkipShell lists the actions whose keys reach termocode even while
// the integrated terminal has keyboard focus, like VSCode's
// terminal.integrated.commandsToSkipShell. Every other plain Ctrl+letter
// goes to the shell (Ctrl+C, Ctrl+R, Ctrl+U, …).
var DefaultSkipShell = []Action{ActionQuit, ActionContextHelp}

// SkipShellSet builds the skip-shell set from DefaultSkipShell and the
// user's config.json "terminal_skip_shell" list of action names (as in
// keymap.json). A name adds the action; "-Name" removes it. Unknown names
// are ignored.
func SkipShellSet(names []string) map[Action]bool {
	set := make(map[Action]bool, len(DefaultSkipShell)+len(names))
	for _, a := range DefaultSkipShell {
		set[a] = true
	}
	for _, n := range names {
		n = strings.TrimSpace(n)
		remove := strings.HasPrefix(n, "-")
		a := actionFromName(strings.TrimSpace(strings.TrimPrefix(n, "-")))
		if a == ActionNone {
			continue
		}
		if remove {
			delete(set, a)
		} else {
			set[a] = true
		}
	}
	return set
}
