package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/help"
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
//	aidemo-busy:tool|stall a running AI turn with its activity line
//	aipipedemo:running|done a two-step agent pipeline run (scripted models)
//	agentsdemo:running|done a planned diamond run in the AGENTS view
//	                       (scripted models)
//	usagedemo:done         AI: Usage after the planned diamond run (scripted
//	                       models; an earlier run is in the session)
//	profiledemo:chip|picker|trust|save|show the agent profile UI (sample data)
//	connectdemo:<step>     a Connect-a-service step with sample data: pick,
//	                       gh, device, browser, ssh, done, error, token,
//	                       slack (nothing connects)
//	commit:<rev>           open the Source Control commit view on rev
//	compare:<i>,<j>        compare GRAPH commits i and j (0 = newest), as
//	                       Space on both does
//	diffdemo               open the AI review card with sample files
//	                       (nothing is written)
//	svcdemo[:down|empty]   show the Services view with a canned snapshot
//	                       (no docker; the shots and goldens)
//	contdemo[:down|noengine|menu|filter|tags|ai-card|logs|inspect|search|db-menu|db-container-menu] show the Containers view with a canned host snapshot
//	dbdemo[:empty|no-sqlite|stopped|result|messages|error|table|transposed|describe|ddl|edits|submit|plan|plan-card|redis|redis-key|ssh-host|ssh-form|ssl-form|compare-pick|compare]   show a canned Databases tree or result page (no connection)
//	secretsdemo:<card|locked|open> the secret store: migration card with
//	                       sample counts, store screen locked or open (writes
//	                       a small secrets.age: sandbox only)
//	httpdemo:<variant>     docs/users.http and a canned HTTP answer:
//	                       file, response, headers or tls (no network)
//	mcpservedemo:status|card|chip|opencode|tip-gemini|tip-aider|allow|idle|off the MCP
//	                       server status screen, an outside agent's confirm
//	                       card, the status bar chip with no agent, OpenCode's
//	                       "add termocode" card, the hand tip of Gemini CLI /
//	                       Aider, or the Agent Auto-Approve list with sample
//	                       "Always allow" entries, or the status screen with
//	                       nothing listening (made-up state, no listener;
//	                       allow and off write config.json: shots only)
//	helpdemo               open the in-app guide at the chat panel section
//	importdemo:picker|panel|confirm|undo|setup|snippets|empty|file
//	                       sample editor folders (sandbox config dir only),
//	                       then the Import from Another Editor picker or
//	                       the review panel of the VS Code sample; confirm
//	                       goes on to the final card, undo imports and
//	                       opens the Undo card; setup makes the setup
//	                       card look again for editors; snippets adds sample
//	                       snippets to the panel; empty and file write
//	                       nothing (picker with no editor, the From a file
//	                       path prompt)
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
	case "svcdemo": // the Services view with a canned snapshot (services_demo.go)
		m.servicesDemo(arg)
	case "contdemo":
		m.containersDemo(arg)
	case "secretsdemo":
		return m.secretsDemo(arg)
	case "mcpservedemo": // the MCP server's status screen or an outside agent's card (mcp_serve_demo.go)
		return m.mcpServeDemo(arg)
	case "dbdemo":
		switch arg {
		case "result", "messages", "error", "table", "transposed", "describe", "ddl", "edits", "submit", "import", "export", "plan", "plan-card":
			return m.databaseResultDemo(arg)
		case "redis-key":
			return m.databaseRedisKeyDemo()
		case "compare-pick", "compare":
			return m.databaseCompareDemo(arg)
		case "ssh-host", "ssh-form", "ssl-form":
			return m.databaseSSHDemo(arg)
		case "redis":
			m.databaseRedisDemo()
		default:
			if cmd, ok := m.databaseCardsDemo(arg); ok {
				return cmd
			}
			m.databasesDemo(arg)
		}
	case "aidemo": // a sample conversation for the AI chat shot
		s := m.aiS()
		s.conv.Messages = []ai.StoredMessage{
			{Role: ai.StoredUser, Text: "Why does the toast slide out twice?", Context: "Attached model.go:120-160:\n..."},
			{Role: ai.StoredAssistant, Text: aiDemoAnswer},
		}
		s.scroll = 0
	case "aidemo-busy": // a running turn with its activity line (B1): arg tool or stall
		s := m.aiS()
		s.conv.Messages = []ai.StoredMessage{
			{Role: ai.StoredUser, Text: "Why does the toast slide out twice?"},
			{Role: ai.StoredTool, Text: "⚙ Search \"leaving\" in internal/toast → 3 matches"},
			{Role: ai.StoredAssistant, Text: "The toast is marked as leaving in `Tick`. Let me read the code that removes it."},
		}
		s.scroll, s.turnMsg, s.streaming = 0, 0, true
		now := clock.Now()
		if arg == "stall" {
			s.act = aiActivity{phase: aiPhaseWriting, since: now.Add(-40 * time.Second), lastAt: now.Add(-35 * time.Second)}
		} else {
			s.act = aiActivity{phase: aiPhaseTool, tool: "read_file", since: now.Add(-2 * time.Second), lastAt: now.Add(-2 * time.Second)}
		}
	case "connectdemo": // a Connect-a-service step for the shots
		return m.connectDemo(arg)
	case "importdemo": // Import from Another Editor with sample editors (import_demo.go)
		return m.importDemo(arg)
	case "termdemo": // terminal tab marks for the shots (term_fix.go, agent_tabs.go)
		m.termDemo()
	case "aipipedemo": // a two-step agent pipeline run (ai_run_demo.go): arg running or done
		return m.aiPipelineDemo(arg)
	case "agentsdemo": // a planned diamond run in the AGENTS view (ai_run_demo.go)
		return m.aiAgentsDemo(arg)
	case "usagedemo": // AI: Usage after a planned run (ai_run_demo.go)
		return m.aiUsageDemo("done")
	case "profiledemo": // the profile chip / picker / trust card (ai_run_demo.go)
		return m.aiProfileDemo(arg)
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
	case "httpdemo": // the .http file / HTTP Response page shots (no network)
		return m.httpDemo(arg)
	case "helpdemo":
		return m.openHelp(help.Loc{Page: "guides/ai-use.md", Anchor: "chat-panel"})
	case "commit": // open the commit view on a revision (HEAD, a SHA …)
		return m.gitOpenCommitViewCmd(arg)
	case "compare": // Space on GRAPH commits i and j
		var i, j int
		if _, err := fmt.Sscanf(arg, "%d,%d", &i, &j); err == nil && i >= 0 && i < len(m.gitCommits) {
			m.commitViewS().mark = m.gitCommits[i].FullHash // no "Marked" toast in the shot
			return m.gitCompareMark(j)
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
