package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/toast"
)

// DevActionMsg lets developer tools (internal/devtools/shots) drive the app
// without a terminal: the screenshot tool sends these through
// tea.Program.Send to set up each scene. Users never produce it.
//
// ID forms:
//
//	palette:<command id>   run a command-palette entry (see paletteItems)
//	open:<path>            open a file in the editor
//	openfile:<path>        open a file as the explorer does (image preview)
//	termsend:<line>        type a line into the active terminal tab
//	view:files|git         switch the sidebar view
//	toast:<title>|<detail> push an info toast
//	theme:<id>             apply a color theme (not persisted)
type DevActionMsg struct{ ID string }

func (m *Model) runDevAction(id string) tea.Cmd {
	kind, arg, _ := strings.Cut(id, ":")
	switch kind {
	case "palette":
		return m.dispatchPaletteAction(arg)
	case "open":
		m.openFileAtLine(arg, 1)
	case "termsend": // type a line into the active terminal tab
		if m.nvim != nil && m.terminalWinID > 0 {
			_ = m.nvim.ExecLua(fmt.Sprintf(`local b = vim.api.nvim_win_get_buf(%d); vim.api.nvim_chan_send(vim.bo[b].channel, %q)`,
				m.terminalWinID, arg+"\r"))
		}
	case "openfile": // as if picked in the explorer (images open the preview)
		return func() tea.Msg { return explorer.OpenFileMsg{Path: arg} }
	case "view":
		m.showExp = true
		switch arg {
		case "git":
			m.activity.SetActive(activity.ViewGit)
		default:
			m.activity.SetActive(activity.ViewFiles)
		}
		m.applyLayout()
	case "toast":
		title, detail, _ := strings.Cut(arg, "|")
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Info, title, detail)
		return cmd
	}
	return nil
}
