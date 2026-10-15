package shellint

import (
	"strconv"
	"strings"
)

// Kind is what an OSC 133 / OSC 7 sequence says.
type Kind int

const (
	// PromptStart (OSC 133;A): the shell draws a prompt; it is idle.
	PromptStart Kind = iota + 1
	// InputStart (OSC 133;B): the prompt ends, the user types.
	InputStart
	// CommandStart (OSC 133;C): a command line runs (Cmd, when sent).
	CommandStart
	// CommandEnd (OSC 133;D): the command finished (Exit when HasExit).
	CommandEnd
	// Cwd (OSC 7): the shell's folder is Cwd on Host.
	Cwd
)

// Event is one parsed sequence.
type Event struct {
	Kind    Kind
	Cmd     string // CommandStart: the command line ("" when unknown)
	Exit    int    // CommandEnd
	HasExit bool   // CommandEnd carried a number
	Cwd     string // Cwd: an absolute path
	Host    string // Cwd: the host part of the URL ("" = this machine)
}

// Parse reads one OSC sequence as nvim's TermRequest hands it over
// ("\x1b]133;D;1", with or without the ESC ] prefix and the BEL or
// ESC \ end). ok is false for anything that is not OSC 133 A-D or OSC 7.
func Parse(seq string) (Event, bool) {
	s := strings.TrimPrefix(seq, "\x1b]")
	s = strings.TrimSuffix(s, "\a")
	s = strings.TrimSuffix(s, "\x1b\\")
	switch {
	case strings.HasPrefix(s, "133;"):
		return parse133(s[len("133;"):])
	case strings.HasPrefix(s, "7;"):
		return parse7(s[len("7;"):])
	}
	return Event{}, false
}

func parse133(s string) (Event, bool) {
	if s == "" {
		return Event{}, false
	}
	parts := strings.Split(s, ";")
	var e Event
	switch parts[0] {
	case "A":
		e.Kind = PromptStart
	case "B":
		e.Kind = InputStart
	case "C":
		e.Kind = CommandStart
		for _, p := range parts[1:] {
			switch {
			case strings.HasPrefix(p, "cmdline_url="):
				e.Cmd = unescape(p[len("cmdline_url="):])
			case strings.HasPrefix(p, "cmdline="):
				e.Cmd = p[len("cmdline="):]
			}
		}
	case "D":
		e.Kind = CommandEnd
		if len(parts) > 1 {
			if n, err := strconv.Atoi(parts[1]); err == nil {
				e.Exit, e.HasExit = n, true
			}
		}
	default:
		return Event{}, false
	}
	return e, true
}

// parse7 reads "file://host/path" (and kitty's "kitty-shell-cwd://").
func parse7(s string) (Event, bool) {
	i := strings.Index(s, "://")
	if i < 0 {
		return Event{}, false
	}
	switch s[:i] {
	case "file", "kitty-shell-cwd":
	default:
		return Event{}, false
	}
	rest := s[i+3:]
	j := strings.IndexByte(rest, '/')
	if j < 0 {
		return Event{}, false
	}
	return Event{Kind: Cwd, Host: rest[:j], Cwd: unescape(rest[j:])}, true
}

// unescape decodes %XX; a broken escape stays as it is.
func unescape(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if h, ok := unhex(s[i+1]); ok {
				if l, ok := unhex(s[i+2]); ok {
					b.WriteByte(h<<4 | l)
					i += 2
					continue
				}
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
