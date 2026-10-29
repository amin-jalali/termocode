package git

import "strings"

// CmdError is a failed git command. Error() is the line of git's output
// that says why (the toast shows only one line, and git often starts with
// noise such as "To <url>" or "Auto-merging x"); Output keeps all of it.
type CmdError struct {
	Err    error  // the exec error ("exit status 1")
	Reason string // the most useful line of Output
	Output string // git's full stdout + stderr, trimmed
}

func (e *CmdError) Error() string { return e.Reason }

func (e *CmdError) Unwrap() error { return e.Err }

// cmdError wraps err (from cmd.CombinedOutput) with git's reason. Without
// output it returns err unchanged.
func cmdError(err error, out []byte) error {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return err
	}
	return &CmdError{Err: err, Reason: errorReason(text), Output: text}
}

// indexLockReason is the reason for a held .git/index.lock.
const indexLockReason = "Another git command is using this repository (.git/index.lock). Try again in a moment."

// errorReason picks the line of git output that best says what went
// wrong: a "fatal:" line, then a rejected ref ("! [rejected] main -> main
// (fetch first)"), then "CONFLICT", then "error:", else the last line that
// is not a hint. The "fatal: " / "error: " prefixes are dropped.
func errorReason(out string) string {
	// Another git (a second window, a terminal) holds the index lock.
	// git's own line is long and names a temp path; say what it means.
	if strings.Contains(out, "index.lock") && strings.Contains(out, "File exists") {
		return indexLockReason
	}
	lines := strings.Split(out, "\n")
	find := func(match func(string) bool) string {
		for _, l := range lines {
			if l = strings.TrimSpace(l); match(l) {
				return l
			}
		}
		return ""
	}
	if l := find(func(l string) bool { return strings.HasPrefix(l, "fatal:") }); l != "" {
		reason := strings.TrimSpace(strings.TrimPrefix(l, "fatal:"))
		// Over ssh git always ends with this line; ssh's own line before
		// it says why (refused, no network, no key).
		if reason == "Could not read from remote repository." {
			for _, p := range lines {
				p = strings.TrimSpace(p)
				if p == l {
					break
				}
				if p != "" && !strings.HasPrefix(p, "hint:") && !strings.HasPrefix(p, "warning:") {
					return p
				}
			}
		}
		return reason
	}
	if l := find(func(l string) bool { return strings.HasPrefix(l, "! [") }); l != "" {
		return strings.Join(strings.Fields(strings.TrimPrefix(l, "! ")), " ")
	}
	if l := find(func(l string) bool { return strings.HasPrefix(l, "CONFLICT") }); l != "" {
		return l
	}
	if l := find(func(l string) bool { return strings.HasPrefix(l, "error:") }); l != "" {
		return strings.TrimSpace(strings.TrimPrefix(l, "error:"))
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.HasPrefix(l, "hint:") {
			return l
		}
	}
	return strings.TrimSpace(lines[0])
}
