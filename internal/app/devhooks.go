package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/textdiff"
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
//	toasts:clear           drop every toast on show
//	panel:<kind>           show a bottom-panel tab: terminal, problems,
//	                       output, tests, debug-console
//	setting:<row id>=<v>   apply a Settings row value, as the Settings UI
//	                       does (row ids: SettingsCatalog().ID)
//	theme:<id>             apply a color theme (not persisted)
//	aidemo                 load a sample AI conversation
//	diffdemo               open the AI review card with sample files
//	                       (nothing is written)
//	plugin:nvim-dap=<dir>  set nvim-dap up from a checkout (tests: the
//	                       sandbox has none; Group K7)
type DevActionMsg struct{ ID string }

// NvimMode reports the embedded nvim's mode and whether it is blocked
// waiting for input (a prompt that would freeze the app). ok is false
// without nvim. For tests (internal/testkit checks it after every step).
func (m Model) NvimMode() (mode string, blocking, ok bool) {
	if m.nvim == nil {
		return "", false, false
	}
	mode, blocking, err := m.nvim.Mode()
	return mode, blocking, err == nil
}

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
	case "plugin":
		if name, dir, ok := strings.Cut(arg, "="); ok && name == pluginDap.name {
			m.setupPlugin(pluginDap, dir)
			return m.debugRefresh()
		}
	case "aidemo": // a sample conversation for the AI chat shot
		s := m.aiS()
		s.conv.Messages = []ai.StoredMessage{
			{Role: ai.StoredUser, Text: "Why does the toast slide out twice?", Context: "Attached model.go:120-160:\n..."},
			{Role: ai.StoredAssistant, Text: aiDemoAnswer},
		}
		s.scroll = 0
	case "aidemo-fa": // a Persian (right-to-left) conversation for the shots
		s := m.aiS()
		s.conv.Messages = []ai.StoredMessage{
			{Role: ai.StoredUser, Text: "چرا پیام toast دو بار بیرون می‌رود؟"},
			{Role: ai.StoredAssistant, Text: aiDemoAnswerFa},
		}
		s.scroll = 0
	case "diffdemo": // the review card for the shots (writes nothing)
		for _, f := range diffview.DemoFiles() {
			m.addAIReview(&aiReviewItem{file: f, finish: func(_ *Model, d *textdiff.Diff, acc map[int]bool, _ bool) (string, tea.Cmd) {
				return aiReviewSummary(d, acc), nil
			}})
		}
		if r := m.ai.review; r != nil {
			r.card.Select(0)
			r.card.Action("next")
			r.card.Action("accept")
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
	case "panel":
		kinds := map[string]panelKind{"terminal": panelKindTerminal, "problems": panelKindProblems,
			"output": panelKindOutput, "tests": panelKindTests, "debug-console": panelKindDebugConsole}
		if k, ok := kinds[arg]; ok {
			m.showPanelTab(k)
		}
	case "setting":
		id, value, _ := strings.Cut(arg, "=")
		m.settingPromptID = id
		return m.applySettingValue(value)
	case "toasts":
		if arg == "clear" {
			m.toast = toast.New()
		}
	case "toast":
		title, detail, _ := strings.Cut(arg, "|")
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Info, title, detail)
		return cmd
	}
	return nil
}

const aiDemoAnswer = "## Cause\n\n`Tick` marks an expired toast as **leaving** but keeps it in the list, and `Step` removes it only after the slide ends. When a second tick fires *before* that, the toast is marked again:\n\n1. first tick: `leaving = true`, slide starts\n2. second tick: the check runs again and **restarts** the slide\n\n### Fix\n\nSkip toasts that are already leaving:\n\n```go\nfor _, t := range m.toasts {\n\tif !t.ExpiresAt.After(now) && !t.leaving {\n\t\tt.leaving = true // slide out once\n\t\tt.slide.Set(1)\n\t}\n\tout = append(out, t)\n}\n```\n\n- keeps one slide per toast\n- no change when animations are off\n\n> Tip: run `go test ./internal/toast` after the change."

// aiDemoAnswerFa is the Persian answer of the "aidemo-fa" shot: RTL
// paragraphs with English words, numbers, inline code and a code block.
const aiDemoAnswerFa = "## علت\n\nتابع `Tick` پیام منقضی را **در حال خروج** علامت می‌زند ولی آن را در فهرست نگه می‌دارد. اگر تیک دوم (پیش از پایان) برسد، حرکت دوباره شروع می‌شود.\n\n### راه حل\n\nپیام‌هایی را که در حال خروج هستند رد کن:\n\n```go\nif !t.ExpiresAt.After(now) && !t.leaving {\n\tt.leaving = true // فقط یک بار\n}\n```\n\n- هر پیام فقط 1 بار حرکت می‌کند\n- با animations خاموش تغییری نیست\n\n> نکته: بعد از تغییر، `go test ./internal/toast` را اجرا کن."
