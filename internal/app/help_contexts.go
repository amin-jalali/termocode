package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/help"
)

// Help for the focused item: Shift+F1 (keymap.ActionContextHelp) and
// "Help: Explain This…" open the built-in guide at the section for what
// has the focus. A hover label on screen wins; then overlays, panels and
// the sidebar; the editor is the default. TestHelpContextTopicsResolve
// checks every topic against the embedded pages.

// helpContexts are the guide sections of the focus states, by name.
var helpContexts = map[string]string{
	"editor":        "guides/editing.md#edit-text",
	"welcome":       "getting-started/first-steps.md#1-open-a-project",
	"preview":       "guides/editing.md#markdown-zen-and-more",
	"keybindings":   "reference/keys.md#change-a-key",
	"theme":         "guides/customize.md#pick-a-theme",
	"settings":      "reference/settings.md",
	"palette":       "reference/commands.md#commands-added-at-run-time",
	"review":        "guides/ai-use.md#review-ai-changes",
	"agent-confirm": "guides/ai-use.md#agent-let-the-chat-use-tools",
	"connect":       "guides/mcp.md#connect-a-service",
	"chat":          "guides/ai-use.md#chat-panel",
	"agents":        "guides/ai-profiles.md#the-agents-panel",
	"find":          "guides/search.md#find-in-this-file",
	"replace":       "guides/search.md#replace-in-this-file",
	"search":        "guides/search.md#live-overlay",
	"search-view":   "guides/search.md#find-in-all-files-find-results-buffer",
	"terminal":      "guides/editing.md#terminal",
	"problems":      "guides/tasks-problems.md#the-problems-panel",
	"output":        "guides/tasks-problems.md#run-a-task",
	"debug-console": "guides/debug.md#debug-console",
	"tests-panel":   "guides/tests.md#read-a-failure",
	"commit-view":   "guides/git.md#the-commit-view",
	"diff":          "guides/git.md#stage-one-hunk",
	"http-response": "guides/http-client.md#the-response-page",
	"http-file":     "guides/http-client.md#send-a-request",
	"db-compare":    "guides/databases.md#compare-schemas",
	"databases":     "guides/databases.md#open-the-view",
	"db-result":     "guides/databases.md#results",
}

// settingsTopic is the row of setting key in the settings reference:
// its category's section, the key as the table shows it.
func settingsTopic(category, key string) string {
	return "reference/settings.md#" + help.Slug(category) + "?`" + key + "`"
}

// paletteTopic is the row of a palette command in the commands
// reference: the section of its group ("Git: Stage Hunk" → Git), the
// title as the table shows it. User and extension commands are not
// listed there: their section says where they come from.
func paletteTopic(title string) string {
	group := "General"
	if i := strings.Index(title, ": "); i > 0 {
		group = title[:i]
	}
	if group == "User" || group == "Ext" {
		return helpContexts["palette"]
	}
	return "reference/commands.md#" + help.Slug(group) + "?" + title
}

// helpTopicForFocus is the guide topic for what has the focus ("" =
// nothing to explain, e.g. the guide itself is open).
func (m Model) helpTopicForFocus() string {
	if m.tip.shown && m.tip.help != "" {
		return m.tip.help
	}
	switch {
	case m.previewOpen:
		if m.helpActive() {
			return ""
		}
		return helpContexts["preview"]
	case m.settingsModalOpen:
		s := m.settingsModal
		if s.cursor >= 0 && s.cursor < len(s.rows) && !s.rows[s.cursor].Header {
			return settingsTopic(s.rows[s.cursor].Category, s.rows[s.cursor].Row.Label)
		}
		return helpContexts["settings"]
	case m.pickerOpen:
		if t := m.pickerHelpTopic(); t != "" {
			return t
		}
	case m.confirmOpen && m.confirmKind == confirmKindAITool:
		return helpContexts["agent-confirm"]
	case m.reviewOpen:
		if m.ai != nil && m.ai.review != nil && m.ai.review.onAction != nil {
			return helpContexts["db-compare"] // the only card with its own actions
		}
		return helpContexts["review"]
	case m.connectOpen:
		return helpContexts["connect"]
	case m.searchOpen:
		return helpContexts["search"]
	case m.replaceOpen:
		return helpContexts["replace"]
	case m.findOpen:
		return helpContexts["find"]
	case m.commitViewShown():
		return helpContexts["commit-view"]
	}
	if m.ai != nil && m.ai.focused {
		if m.ai.agents.on {
			return helpContexts["agents"]
		}
		return helpContexts["chat"]
	}
	if m.focus == FocusExplorer && m.showExp {
		if t := m.sidebarHelpTopic(); t != "" {
			return t
		}
	}
	if m.panelFocused {
		switch m.panelActive {
		case panelKindProblems:
			return helpContexts["problems"]
		case panelKindOutput:
			return helpContexts["output"]
		case panelKindTests:
			return helpContexts["tests-panel"]
		case panelKindDebugConsole:
			return helpContexts["debug-console"]
		}
	}
	switch {
	case m.inTerminal:
		return helpContexts["terminal"]
	case m.gitDiffActive:
		return helpContexts["diff"]
	case lastPathPart(m.editor.Path()) == "SQL Result":
		return helpContexts["db-result"]
	case m.editor.Lang() == "httpresponse":
		return helpContexts["http-response"]
	case isHTTPPath(m.editor.Path()):
		return helpContexts["http-file"]
	case m.welcomeShowing():
		pane := m.editorPaneLayout()
		rows := welcomeRows(pane.W, pane.H)
		if m.welcomeFocus >= 0 && m.welcomeFocus < len(rows) {
			if topic := tooltipHelp("welcome:" + rows[m.welcomeFocus].ID); topic != "" {
				return topic
			}
		}
		return helpContexts["welcome"]
	}
	return helpContexts["editor"]
}

// pickerHelpTopic is the topic of the open picker ("" = explain what is
// under it instead).
func (m Model) pickerHelpTopic() string {
	switch m.pickerKind {
	case pickerKindPalette:
		it, ok := m.picker.Current()
		if !ok {
			return "reference/commands.md"
		}
		for _, p := range paletteItems() {
			if p.ID == it.ID {
				return paletteTopic(p.Title)
			}
		}
		return paletteTopic(it.Title)
	case pickerKindKeybinding:
		return helpContexts["keybindings"]
	case pickerKindTheme:
		return helpContexts["theme"]
	case pickerKindDBCompareSource, pickerKindDBCompareTarget:
		return helpContexts["db-compare"]
	}
	return ""
}

// sidebarHelpTopic is the topic of the sidebar view.
func (m Model) sidebarHelpTopic() string {
	switch m.activity.Active() {
	case activity.ViewFiles:
		return activityHelp["Explorer"]
	case activity.ViewGit:
		return activityHelp["Source Control"]
	case activity.ViewRun:
		return activityHelp["Run"]
	case activity.ViewTests:
		return activityHelp["Testing"]
	case activity.ViewServices:
		return activityHelp["Containers"]
	case activity.ViewSearch:
		return helpContexts["search-view"]
	case activity.ViewDatabases:
		return helpContexts["databases"]
	}
	return ""
}

// openContextHelp opens the guide for what has the focus. Overlays stay
// open under the guide, so Esc goes back to them.
func (m *Model) openContextHelp() tea.Cmd {
	topic := m.helpTopicForFocus()
	if topic == "" {
		return nil
	}
	l, err := help.Parse(topic)
	if err != nil {
		return nil
	}
	m.tip.hide()
	return m.openHelp(l)
}
