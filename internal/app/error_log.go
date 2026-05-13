package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/preview"
	"termocode/internal/toast"
)

// errorLogCap caps the in-memory error log so a runaway error path
// doesn't consume unbounded RAM.
const errorLogCap = 100

// init registers a hook with the toast package so every error / warning
// toast pushed from anywhere in the app automatically lands in the error
// log too. This eliminates having to thread a logger through every call
// site and means we capture the *full* text even when the toast itself
// gets clipped on render.
func init() {
	toast.OnPush = func(severity toast.Severity, msg string) {
		if severity == toast.Errr || severity == toast.Warn {
			recordError(msg)
		}
	}
}

// errorRing is a process-wide ring of full error / warning messages so
// every code path that surfaces an error can dump the full text here
// without having to thread a Model pointer through. The most recent
// entry is the last element. Mutated with errorRingMu.
var (
	errorRingMu sync.Mutex
	errorRing   []string
)

// recordError appends msg (with timestamp) to the global error ring AND
// to the on-disk log at ~/.config/termocode/errors.log so external tools
// can `tail -f` the file. Both writes are best-effort — a logging
// failure must never propagate.
func recordError(msg string) {
	if strings.TrimSpace(msg) == "" {
		return
	}
	stamp := time.Now().Format("2006-01-02 15:04:05")
	entry := stamp + "  " + msg
	errorRingMu.Lock()
	errorRing = append(errorRing, entry)
	if len(errorRing) > errorLogCap {
		// Drop the oldest entries to stay at the cap.
		errorRing = errorRing[len(errorRing)-errorLogCap:]
	}
	errorRingMu.Unlock()
	// Best-effort disk persistence (silent on failure).
	if dir, err := configDir(); err == nil {
		_ = os.MkdirAll(dir, 0o755)
		if f, err := os.OpenFile(filepath.Join(dir, "errors.log"),
			os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_, _ = f.WriteString(entry + "\n")
			_ = f.Close()
		}
	}
}

// errorEntries returns a copy of the current ring (newest-first) for
// rendering by openErrorLog.
func errorEntries() []string {
	errorRingMu.Lock()
	defer errorRingMu.Unlock()
	out := make([]string, len(errorRing))
	for i, e := range errorRing {
		out[len(errorRing)-1-i] = e
	}
	return out
}

// pushErr is the centralised "show an error" helper: it records the full
// text in the ring, then displays the same text as a toast. Most error
// paths in the app should funnel through here so the log captures
// everything.
func (m *Model) pushErr(msg string) tea.Cmd {
	recordError(msg)
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.Push(toast.Errr, msg)
	return toastCmd
}

// setErr is the central setter for the status-bar error string. Mirrors
// the value into the global ring so the user can read the full text
// later via "Help: Show Error Log" — the status bar truncates everything
// past ~60 chars, and toasts get clipped further.
//
// Call this instead of `m.err = ...` whenever the error originates
// somewhere we want to log. (Plain `m.err = ""` for clearing is fine —
// no point logging an empty string.)
func (m *Model) setErr(msg string) {
	m.err = msg
	if msg != "" {
		recordError(msg)
	}
}

// openErrorLog shows every entry in the global ring in the preview
// overlay so the user can read errors that the toast / status bar
// truncated. Newest first.
func (m *Model) openErrorLog() tea.Cmd {
	entries := errorEntries()
	body := "(no errors recorded this session)"
	if len(entries) > 0 {
		body = strings.Join(entries, "\n\n")
	}
	body += "\n\n— full log file: ~/.config/termocode/errors.log —"
	m.preview = preview.New(" Error Log ", body)
	m.preview.SetSize(m.w, m.h)
	m.previewOpen = true
	return nil
}
