package app

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/toast"
)

// sendF11ToHost simulates an F11 keypress at the OS level so the host
// terminal's native fullscreen handler fires — the only reliable way to
// cover terminals (kitty, iTerm2, Ghostty, alacritty, gnome-terminal)
// that ignore the xterm CSI 10 ; t fullscreen escape. Best-effort: tries
// every tool we know about in sequence and silently no-ops on failure
// (no toast — a missing tool isn't an error worth shouting about).
func sendF11ToHost() {
	switch runtime.GOOS {
	case "darwin":
		// AppleScript: System Events sends key code 111 = F11.
		_ = exec.Command("osascript", "-e",
			`tell application "System Events" to key code 111`).Start()
	case "linux", "freebsd", "openbsd", "netbsd":
		// X11: xdotool. Wayland: wtype. Try both — only one will exist
		// on a given system; the other returns "command not found".
		_ = exec.Command("xdotool", "key", "F11").Start()
		_ = exec.Command("wtype", "-k", "F11").Start()
	}
}

// toggleZenMode flips the chrome-hiding flag and asks the host terminal
// to go (or leave) fullscreen. The chrome side always works — the
// renderer reads m.zenMode directly. The fullscreen side is best-effort
// because terminals expose wildly different APIs for it:
//
//   - xterm-family CSI 10;Ps t: works on xterm, wezterm, urxvt, konsole,
//     mintty. We send `1` (on) / `0` (off) AND `2` (toggle) so terminals
//     that honour any one of them flip correctly.
//   - kitty, iTerm2, Ghostty, alacritty, gnome-terminal, Terminal.app:
//     no programmatic fullscreen. They silently ignore the escapes.
//     Toast hint suggests pressing F11 / native shortcut.
func (m *Model) toggleZenMode() tea.Cmd {
	m.zenMode = !m.zenMode
	if m.zenMode {
		m.showExp = false
		// Try fullscreen ON via every escape we know …
		_, _ = os.Stdout.WriteString("\x1b[10;1t" + "\x1b[10;2t")
		// … and also fire an F11 at the OS keyboard layer so terminals
		// that ignore the escape (kitty, iTerm2, Ghostty, alacritty)
		// still toggle fullscreen via their native shortcut.
		sendF11ToHost()
	} else {
		m.showExp = true
		_, _ = os.Stdout.WriteString("\x1b[10;0t" + "\x1b[10;2t")
		sendF11ToHost()
	}
	m.applyLayout()
	var toastCmd tea.Cmd
	if m.zenMode {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info,
			"Zen mode on", "press F11 if window didn't fullscreen")
	} else {
		m.toast, toastCmd = m.toast.Push(toast.Info, "Zen mode off")
	}
	return toastCmd
}

// resetSession wipes the persisted editor state — open files, cursor
// positions, expanded explorer dirs — and re-execs termocode so the user
// gets a clean slate. Useful after a buffer leak (zombie `term://...` or
// PREVIEW entries from previous sessions) leaves tabs that won't close.
//
// We back up the existing session.json next to it as a `.bak` so a
// pebkac "I didn't mean it" can be undone with `mv ... .bak ... .json`.
func (m *Model) resetSession() tea.Cmd {
	dir, err := configDir()
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Config error", err.Error())
		return toastCmd
	}
	target := dir + "/session.json"
	// Best-effort backup; errors are non-fatal — the user explicitly
	// asked to reset, missing backup just means we can't undo.
	if data, err := os.ReadFile(target); err == nil {
		_ = os.WriteFile(target+".bak", data, 0o644)
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Reset failed", err.Error())
		return toastCmd
	}
	// Re-exec ourselves so the next launch starts from the clean
	// session. (Doing this in-process would mean reverting every
	// in-memory cache and re-attaching nvim, which is a lot riskier
	// than just restarting cleanly.)
	return m.reloadWindow()
}

// reloadWindow re-execs termocode in the same cwd. Useful when the user
// has changed config files or installed a new LSP server and wants a
// fresh process without dropping back to the shell.
func (m *Model) reloadWindow() tea.Cmd {
	if m.nvim != nil {
		_ = m.nvim.Close()
	}
	bin, err := os.Executable()
	if err != nil {
		bin = os.Args[0]
	}
	args := []string{bin}
	if err := syscall.Exec(bin, args, os.Environ()); err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Reload failed", err.Error())
		return toastCmd
	}
	return nil
}

// toggleAutoSave installs (or removes) an idle-write autocmd. nvim's
// CursorHold fires after `updatetime` ms (1000) of no input — once a
// buffer is modified, the autocmd writes it. Disabling clears the augroup.
func (m *Model) toggleAutoSave() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	m.autoSaveOn = !m.autoSaveOn
	if m.autoSaveOn {
		// Group is replaceable so re-enabling doesn't stack autocmds.
		_ = m.nvim.ExecLua(`
			vim.api.nvim_create_augroup('TermocodeAutoSave', { clear = true })
			vim.api.nvim_create_autocmd({ 'CursorHold', 'CursorHoldI', 'BufLeave', 'FocusLost' }, {
				group = 'TermocodeAutoSave',
				callback = function()
					if vim.bo.modified and vim.bo.buftype == '' and vim.fn.expand('%') ~= '' then
						pcall(vim.cmd, 'silent! write')
					end
				end,
			})
		`)
	} else {
		_ = m.nvim.ExecLua(`pcall(vim.api.nvim_del_augroup_by_name, 'TermocodeAutoSave')`)
	}
	var toastCmd tea.Cmd
	state := "off"
	if m.autoSaveOn {
		state = "on"
	}
	m.toast, toastCmd = m.toast.Push(toast.Info, "Auto-save "+state)
	return toastCmd
}
