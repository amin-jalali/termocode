package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
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
