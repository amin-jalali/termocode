package app

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.design/x/clipboard"

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
// XWayland) and, when that fails, wl-copy / xclip / xsel / pbcopy.
//
// OSC 52 cannot be checked. It counts only when the local clipboard
// failed or termocode runs over SSH (then the local clipboard is the
// server's), and not in VTE terminals (GNOME Terminal, Tilix …), which
// ignore it.
func (m *Model) writeClipboard(s string) clipVia {
	if m.nvim != nil {
		_ = m.nvim.ExecLuaArgs(`local s = ...
			vim.fn.setreg('"', s)
			pcall(vim.fn.setreg, '+', s)`, s)
	}
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
	osc := len(s) <= osc52MaxBytes && os.Getenv("VTE_VERSION") == ""
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

// systemClipboardWrite writes the OS clipboard and reports success.
func systemClipboardWrite(s string) bool {
	if clipboardReady() && clipboardLibWrite(s) {
		return true
	}
	return clipboardToolWrite(s)
}

// clipboardLibWrite is golang.design/x/clipboard's Write; it returns nil
// when it could not take the clipboard.
func clipboardLibWrite(s string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return clipboard.Write(clipboard.FmtText, []byte(s)) != nil
}

// clipboardTools are the clipboard commands to try, in order.
func clipboardTools() [][]string {
	var out [][]string
	if runtime.GOOS == "darwin" {
		out = append(out, []string{"pbcopy"})
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		out = append(out, []string{"wl-copy"})
	}
	if os.Getenv("DISPLAY") != "" {
		out = append(out, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
	}
	return out
}

// clipboardToolWrite pipes s into the first clipboard command that works.
func clipboardToolWrite(s string) bool {
	for _, argv := range clipboardTools() {
		if _, err := exec.LookPath(argv[0]); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		c := exec.CommandContext(ctx, argv[0], argv[1:]...)
		c.Stdin = strings.NewReader(s)
		err := c.Run()
		cancel()
		if err == nil {
			return true
		}
	}
	return false
}

// clipboardHelp says how to make copying work.
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
		m.toast, c = m.toast.Push(toast.Info, done+" (via terminal)")
	default:
		// nvim's registers took it: Ctrl+V in the editor still pastes.
		title := "Copy failed: no clipboard found"
		if m.nvim != nil {
			title = "Copied to the editor only: no system clipboard"
		}
		m.toast, c = m.toast.PushDetail(toast.Warn, title, clipboardHelp)
	}
	return c
}
