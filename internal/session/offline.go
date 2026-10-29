package session

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// reconnectDelay is how long a client waits before reconnect attempt n
// (1 = the first): 1 s, 2 s, 5 s, then 10 s for every later one.
func reconnectDelay(attempt int) time.Duration {
	switch {
	case attempt <= 1:
		return time.Second
	case attempt == 2:
		return 2 * time.Second
	case attempt == 3:
		return 5 * time.Second
	}
	return 10 * time.Second
}

// offlineLine draws the status line a client shows on the last row of
// its terminal while the link is down. The screen above it stays as it
// was; the cursor goes back where it was. Plain ASCII when the terminal
// is not UTF-8.
func offlineLine(attempt, cols, rows int, unicode bool) string {
	dots, sep := "…", "·"
	if !unicode {
		dots, sep = "...", "-"
	}
	text := fmt.Sprintf(" Reconnecting%s (attempt %d) %s typing is paused %s Ctrl+C to quit ", dots, attempt, sep, sep)
	if n := utf8.RuneCountInString(text); cols > 0 && n > cols {
		text = string([]rune(text)[:cols])
	} else if cols > n {
		text += strings.Repeat(" ", cols-n)
	}
	// Save the cursor, bottom row, reverse video, restore the cursor.
	return fmt.Sprintf("\x1b7\x1b[%d;1H\x1b[0;7m%s\x1b[0m\x1b8", max(rows, 1), text)
}

// offlineQuit reports whether keys typed while offline ask to quit:
// Ctrl+C, as a plain byte or in the kitty keyboard encoding.
func offlineQuit(p []byte) bool {
	return bytes.IndexByte(p, 0x03) >= 0 || bytes.Contains(p, []byte("\x1b[99;5u"))
}

// terminalReports are what a terminal sends by itself while the UI's
// modes stay on: mouse (SGR and X10) and focus reports.
var terminalReports = regexp.MustCompile(`\x1b\[<[0-9;]*[Mm]|\x1b\[M[\x20-\xff]{3}|\x1b\[[IO]`)

// offlineKeys reports whether p, read while offline, holds keys the user
// typed (not just mouse moves or focus changes), which are then lost.
func offlineKeys(p []byte) bool {
	return len(terminalReports.ReplaceAll(p, nil)) > 0
}

// ReconnectNote is what the UI tells the user after a reconnect.
func ReconnectNote(offline time.Duration, dropped bool) string {
	s := int(offline.Round(time.Second) / time.Second)
	var msg string
	switch {
	case s < 1:
		msg = "Reconnected"
	case s < 120:
		msg = fmt.Sprintf("Reconnected after %d s", s)
	default:
		msg = fmt.Sprintf("Reconnected after %d min", s/60)
	}
	if dropped {
		msg += " · keys typed while offline were dropped"
	}
	return msg
}
