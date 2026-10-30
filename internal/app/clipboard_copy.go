package app

import (
	"os"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/termcaps"
	"github.com/amin-jalali/termocode/internal/toast"
)

// clipVia is how a copy reached a clipboard.
type clipVia int

const (
	clipFailed   clipVia = iota // no clipboard was reached
	clipLocal                   // the system clipboard (checked)
	clipTerminal                // OSC 52 sent to the terminal (not checked)
)

// clipboardSink, when set, takes every copy instead of the system
// clipboard (tests; see SetClipboardSink).
var clipboardSink atomic.Pointer[func(string)]

// SetClipboardSink sends every copy to f instead of the system clipboard
// and the terminal. For tests: they must never touch the real clipboard.
func SetClipboardSink(f func(string)) (restore func()) {
	var p *func(string)
	if f != nil {
		p = &f
	}
	old := clipboardSink.Swap(p)
	return func() { clipboardSink.Store(old) }
}

// writeClipboard puts s in nvim's unnamed and + registers, the system
// clipboard and (OSC 52) the terminal's clipboard, and reports what it
// reached. The system clipboard is golang.design/x/clipboard (X11, also
// XWayland) and, when that fails, wl-copy / xclip / xsel / pbcopy. In
// Termux it is termux-clipboard-set.
//
// OSC 52 cannot be checked. It counts only when the local clipboard
// failed or termocode runs over SSH (then the local clipboard is the
// server's), and only in terminals that do not ignore it (osc52Honoured).
func (m *Model) writeClipboard(s string) clipVia {
	if m.nvim != nil {
		_ = m.nvim.ExecLuaArgs(`local s = ...
			vim.fn.setreg('"', s)
			pcall(vim.fn.setreg, '+', s)`, s)
	}
	return writeSystemClipboard(s)
}

// writeSystemClipboard is writeClipboard without nvim's registers: for
// text that must not stay in the editor, like a one-time link.
func writeSystemClipboard(s string) clipVia {
	if f := clipboardSink.Load(); f != nil {
		(*f)(s)
		return clipLocal
	}
	if headless() {
		return clipFailed
	}
	remote := os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != ""
	local := systemClipboardWrite(s)
	emitOSC52(s)
	osc := len(s) <= osc52MaxBytes && osc52Honoured(os.Getenv)
	switch {
	case local && !remote:
		return clipLocal
	case osc:
		return clipTerminal
	case local:
		return clipLocal
	}
	return clipFailed
}

// osc52Honoured reports whether the terminal may act on an OSC 52 copy.
// VTE (GNOME Terminal, Tilix …), macOS Terminal.app, xterm (off unless
// allowWindowOps), the Linux console and JetBrains' terminal ignore it.
// Inside tmux or screen the outer terminal decides and the inherited
// variables may be stale, so they count as yes (osc52Seq wraps the copy
// so they forward it). Unknown terminals get the benefit of the doubt.
func osc52Honoured(getenv func(string) string) bool { return termcaps.OSC52Honoured(getenv) }

// systemClipboardWrite writes the OS clipboard and reports success. In
// Termux the golang.design library is skipped: it cannot reach Android's
// clipboard, and termux-clipboard-set can.
func systemClipboardWrite(s string) bool {
	h := realClipHost()
	if !h.termux() && clipboardReady() && clipboardLibWrite(s) {
		return true
	}
	return h.toolWrite(s)
}

// clipboardLibWrite is golang.design/x/clipboard's Write; it returns
// false when it could not take the clipboard.
func clipboardLibWrite(s string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return clipLibWrite(s)
}

// clipboardHelp says how to make copying work on a desktop (see
// clipHost.help for Termux).
const clipboardHelp = "Install wl-clipboard (Wayland) or xclip (X11), or use a terminal with OSC 52 (kitty, WezTerm, foot, Alacritty)."

// copyWithToast copies text and tells the user what happened: done on
// success, a warning with a fix when no clipboard took it.
func (m *Model) copyWithToast(text, done string) tea.Cmd {
	if text == "" {
		return nil
	}
	var c tea.Cmd
	switch m.writeClipboard(text) {
	case clipLocal:
		m.toast, c = m.toast.Push(toast.Info, done)
	case clipTerminal:
		if h := realClipHost(); h.termux() {
			// Termux's terminal takes OSC 52, but reading the Android
			// clipboard (paste from other apps) needs termux-api.
			if _, err := h.lookPath("termux-clipboard-get"); err != nil {
				m.toast, c = m.toast.PushDetail(toast.Info, done+" (via terminal)", "To paste from other apps: "+termuxHelp)
				break
			}
		}
		m.toast, c = m.toast.Push(toast.Info, done+" (via terminal)")
	default:
		// nvim's registers took it: Ctrl+V in the editor still pastes.
		title := "Copy failed: no clipboard found"
		if m.nvim != nil {
			title = "Copied to the editor only: no system clipboard"
		}
		m.toast, c = m.toast.PushDetail(toast.Warn, title, realClipHost().help())
	}
	return c
}
