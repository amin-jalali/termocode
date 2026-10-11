package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
)

// notifyLua replaces vim.notify. The default one echoes the message; with
// cmdheight=0 a long or multi-line echo opens a hit-enter prompt, and while
// nvim waits for <CR> it answers no RPC call, so the whole app freezes
// (seen offline: "rainbow-delimiters bootstrap failed"). Warnings and
// errors go to Go as toasts instead; lower levels are dropped. It runs
// first in attachCmd, so later wrappers (nvim-dap's) chain onto it.
const notifyLua = `
vim.notify = function(msg, level, _)
  level = level or vim.log.levels.INFO
  if level < vim.log.levels.WARN then return end
  local send = _G.termocode_notify
  if send then pcall(send, 'termocode_nvim_notify', level, tostring(msg)) end
end
`

func init() {
	registerNotifyHandler("termocode_nvim_notify", func(m *Model, args []any) tea.Cmd {
		text := strings.TrimSpace(notifyArgString(args, 1))
		if text == "" {
			return nil
		}
		sev := toast.Warn
		if notifyArgInt(args, 0) >= 4 { // vim.log.levels.ERROR
			sev = toast.Errr
		}
		var cmd tea.Cmd
		m.toast, cmd = m.toast.Push(sev, text)
		return cmd
	})
}
