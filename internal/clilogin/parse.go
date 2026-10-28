package clilogin

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The parsers below depend only on "the first https:// link in the
// output", a prompt line that asks for a code, and a code-shaped word
// after the link, never on a CLI's exact wording (which is unverified
// and changes between versions).

// cleanText is s with escape sequences removed and every line end as
// "\n" (an Ink app redraws with "\r").
func cleanText(s string) string {
	s = ansi.Strip(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

var urlRe = regexp.MustCompile("https://[^\\s\"'<>`]+")

// firstURL is the first complete https:// link in s (one that a space
// or a line end follows: the rest may not be read yet), without the
// punctuation around it; "" when there is none.
func firstURL(s string) string {
	for _, loc := range urlRe.FindAllStringIndex(s, -1) {
		if loc[1] >= len(s) {
			return ""
		}
		u := strings.TrimRight(s[loc[0]:loc[1]], ".,;:!?)]}")
		if len(u) > len("https://") {
			return u
		}
	}
	return ""
}

// needsCode reports whether the last line of s asks for a code to be
// pasted (Claude Code's fallback when the browser cannot call back).
func needsCode(s string) bool {
	line := lastLine(s)
	l := strings.ToLower(line)
	if !strings.Contains(l, "code") || strings.Contains(l, "https://") {
		return false
	}
	if strings.Contains(l, "paste") || strings.Contains(l, "enter") || strings.Contains(l, "type") {
		return true
	}
	return strings.HasSuffix(line, ":") || strings.HasSuffix(line, ">") || strings.HasSuffix(line, "?")
}

// lastLine is the last non-blank line of s, trimmed.
func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

var codeRe = regexp.MustCompile(`\b[A-Z0-9]{4,}-[A-Z0-9]{4,}\b`)

// deviceCode is the one-time code printed after the link of a device
// sign-in (Codex --device-auth), "" when there is none yet.
func deviceCode(s string) string {
	loc := urlRe.FindStringIndex(s)
	if loc == nil {
		return ""
	}
	return codeRe.FindString(s[loc[1]:])
}

// nodeMissing reports whether s is a shell's "node is not there" error
// (the npm shim of a CLI starts node).
func nodeMissing(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "node") && (strings.Contains(l, "no such file") || strings.Contains(l, "not found"))
}
