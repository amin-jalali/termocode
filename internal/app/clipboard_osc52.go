package app

import (
	"encoding/base64"
	"os"
)

// osc52MaxBytes caps the raw text size we'll ship via OSC 52. Many
// terminal emulators reject sequences longer than ~8 KiB after
// base64 expansion (which inflates by ~4/3). 6000 bytes raw → ~8000
// bytes base64, which fits the tightest commonly-seen limit. Larger
// payloads are silently skipped — we never want a copy operation to
// leak garbage into the user's screen or break the alt-screen render.
const osc52MaxBytes = 6000

// emitOSC52 writes an OSC 52 "set clipboard" escape sequence to stdout.
// The local terminal emulator intercepts the sequence and copies the
// payload into the user's *local* clipboard — crucially, this works
// over SSH (the bytes ride the same TTY stream as ordinary output),
// which is the whole point: nvim's `+` register and golang.design/x
// /clipboard only ever reach the *server's* clipboard.
//
// Terminals that don't understand OSC 52 ignore the unknown OSC
// silently, so it's always safe to emit. We deliberately do not log
// or surface failures — this is a best-effort augmentation of the
// existing safeClipboardWrite path, not a replacement.
//
// The sequence is bracketed with \x1b]52;c;<base64>\x07 (BEL
// terminator). Bubble Tea's alt-screen renderer treats OSC sequences
// as out-of-band terminal commands, so writing directly to os.Stdout
// here does not corrupt the screen buffer — confirmed by the same
// pattern used for DECSCUSR (`\x1b[0 q`) and OSC 1337 ChangeFontSize
// in cmd/termocode/main.go.
func emitOSC52(s string) {
	if s == "" {
		return
	}
	if len(s) > osc52MaxBytes {
		// Some terminals cap OSC 52 payloads. Skip rather than
		// truncate, because a truncated copy is more confusing
		// than no copy at all (the local-clipboard fallback via
		// safeClipboardWrite still ran for the full text).
		return
	}
	defer func() {
		// os.Stdout writes shouldn't panic on a TTY, but be paranoid:
		// a closed stdout (e.g. test harness) shouldn't crash the
		// editor mid-keystroke.
		_ = recover()
	}()
	b64 := base64.StdEncoding.EncodeToString([]byte(s))
	_, _ = os.Stdout.WriteString("\x1b]52;c;" + b64 + "\x07")
}
