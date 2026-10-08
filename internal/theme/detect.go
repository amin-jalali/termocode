package theme

import (
	"os"
	"strings"
	"time"

	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// ResolveIconMode picks the icon set for the "icons" setting.
//
//   - TERMOCODE_ICON_MODE / TERMOCODE_NERD_FONT=1 win over everything.
//   - "nerd", "unicode", "ascii" are taken as is.
//   - "auto" (or empty): Nerd Font when the doctor found one installed
//     (and we are not over SSH, where the local fonts say nothing about
//     the client); ASCII only on terminals that cannot draw Unicode
//     (TERM=linux / dumb / vt*, or a non-UTF-8 locale); else the Unicode
//     set, which renders in every modern terminal font. unicodeOK, when
//     given, can veto Unicode (a width probe that saw wide glyphs).
func ResolveIconMode(setting string, nerdFont bool, unicodeOK func() bool, getenv func(string) string) IconMode {
	if getenv == nil {
		getenv = os.Getenv
	}
	if v := getenv("TERMOCODE_ICON_MODE"); v != "" {
		switch strings.ToLower(v) {
		case "nerd", "nerd_font", "nerdfont":
			return IconModeNerdFont
		case "unicode":
			return IconModeUnicode
		case "ascii":
			return IconModeASCII
		}
	}
	if getenv("TERMOCODE_NERD_FONT") == "1" {
		return IconModeNerdFont
	}
	switch strings.ToLower(strings.TrimSpace(setting)) {
	case "nerd", "nerd_font", "nerdfont":
		return IconModeNerdFont
	case "unicode":
		return IconModeUnicode
	case "ascii":
		return IconModeASCII
	}
	remote := getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != ""
	if nerdFont && !remote {
		return IconModeNerdFont
	}
	if !unicodeTerminal(getenv) {
		return IconModeASCII
	}
	if unicodeOK != nil && !unicodeOK() {
		return IconModeASCII
	}
	return IconModeUnicode
}

// unicodeTerminal reports whether the terminal can be expected to draw
// Unicode symbols: not a bare console and not an explicitly non-UTF-8
// locale (an unset locale is common over SSH and does not count).
func unicodeTerminal(getenv func(string) string) bool {
	t := strings.ToLower(getenv("TERM"))
	if t == "linux" || t == "dumb" || t == "cons25" || strings.HasPrefix(t, "vt1") || strings.HasPrefix(t, "vt2") {
		return false
	}
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := strings.ToLower(getenv(k)); v != "" {
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return true
}

// ProbeUnicode measures every Unicode icon on the real terminal (CSI 6n
// before / after each glyph) and reports whether all of them draw one cell
// wide. It switches the TTY to raw mode itself and must run before Bubble
// Tea starts. Output is hidden behind save / restore cursor and erased.
func ProbeUnicode(timeout time.Duration) bool {
	in, out, ok := probeStreams()
	if !ok {
		return false
	}
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return false
	}
	defer func() { _ = term.Restore(int(in.Fd()), state) }()
	deadline := time.Now().Add(timeout)
	_, _ = out.WriteString("\x1b7")
	defer func() { _, _ = out.WriteString("\r\x1b[2K\x1b8") }()
	glyphs := append(unicodeCandidates(), unicodeFileGlyphs()...)
	for _, g := range glyphs {
		w, ok := probeGlyph(in, out, g, deadline)
		if !ok || w != 1 {
			return false
		}
	}
	return true
}

// DetectProfile decides the colour profile. termocode defaults to
// truecolor because many terminals support it without advertising it
// (COLORTERM is rarely forwarded over SSH). It only steps down when the
// environment says so:
//
//	TERMOCODE_COLORS=truecolor|256|16|none   explicit choice
//	NO_COLOR (any value)                     no colour
//	TERM=dumb                                no colour
//	TERM=linux, vt100, vt220, ansi, cons25    16 colours
func DetectProfile(getenv func(string) string) termenv.Profile {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch strings.ToLower(getenv("TERMOCODE_COLORS")) {
	case "truecolor", "24bit", "true":
		return termenv.TrueColor
	case "256", "ansi256":
		return termenv.ANSI256
	case "16", "ansi", "8":
		return termenv.ANSI
	case "none", "off", "ascii", "0":
		return termenv.Ascii
	}
	if getenv("NO_COLOR") != "" {
		return termenv.Ascii
	}
	t := strings.ToLower(getenv("TERM"))
	switch {
	case t == "dumb":
		return termenv.Ascii
	case t == "linux" || t == "ansi" || t == "cons25" || strings.HasPrefix(t, "vt1") || strings.HasPrefix(t, "vt2"):
		return termenv.ANSI
	}
	return termenv.TrueColor
}

// unicodeFileGlyphs are the Unicode-tier file icons (one per category).
func unicodeFileGlyphs() []string {
	return []string{uniCode, uniConfig, uniDoc, uniImage, uniArchive, uniGit, uniLock, uniData, uniFile, uniDirC, uniDirO}
}
