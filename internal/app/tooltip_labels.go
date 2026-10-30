package app

import (
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/explorer"
)

// tooltipSpec is the hover label of one kind of hit target.
type tooltipSpec struct {
	name string
	// palette is the command-palette id of the same command: its key
	// comes from the live keymap (keymap.json included), shown the way
	// the palette shows it, so it cannot drift.
	palette string
	// local is a key the widget handles itself (not in the keymap).
	local string
	// help is the guide section that explains the button
	// ("guides/ai-use.md#chat-panel", see internal/help). The label gets a
	// "More info" line only when it is set; TestHelpTopicsResolve checks
	// every topic against the built-in guide.
	help string
}

// Guide sections the labels point at.
const (
	helpChat      = "guides/ai-use.md#chat-panel"
	helpAgents    = "guides/ai-profiles.md#the-agents-panel"
	helpTerminal  = "guides/editing.md#terminal"
	helpTabs      = "guides/editing.md#tabs-and-splits"
	helpServices  = "guides/services.md#keys"
	helpBranches  = "guides/git.md#branches-history-and-stash"
	helpFind      = "guides/search.md#find-in-this-file"
	helpReplace   = "guides/search.md#replace-in-this-file"
	helpStageHunk = "guides/git.md#stage-one-hunk"
)

// activityHelp is the guide section of each activity-bar view, by its
// label (activity labels are not in tooltipLabels).
var activityHelp = map[string]string{
	"Explorer":       "guides/editing.md#open-files",
	"Source Control": "guides/git.md#open-the-source-control-panel",
	"Run":            "guides/debug.md#debug-in-five-steps",
	"Testing":        "guides/tests.md#open-the-testing-view",
	"Containers":     "guides/containers.md#the-view",
	"Databases":      "guides/databases.md#open-the-view",
	"Itermoc":        "guides/itermoc.md#the-view",
	"Runs":           runsGuidePage + "#open-runs",
}

// tooltipLabels maps a hit target id (see hit_targets.go) to its label.
// Ids ending in ":" are prefixes for numbered targets ("close:3").
// Activity-bar icons use their own name (activity.Item.Label).
var tooltipLabels = map[string]tooltipSpec{
	"overflow:glyph": {name: "More actions"},

	// AI chat panel header (+ ≡ ⚐ ×) and the launcher chip.
	"ai:new":             {name: "New chat", local: "Ctrl+N", help: helpChat}, // aiPanelKey
	"ai:history":         {name: "Chat history", help: helpChat},
	"ai:profile":         {name: "Agent profile (and its details)", palette: "ai-profile-next", help: "guides/ai-profiles.md#pick-a-profile"},
	"ai:mcp":             {name: "Services the chat can use · click: list", help: "guides/itermoc.md#the-view"},
	"ai:more":            {name: "More: profile, services, usage, agents", help: helpChat},
	"ai:usage":           {name: "Session usage: tokens and cost by model and agent (AI: Usage)", help: helpAgents},
	"ai:chat":            {name: "Back to the chat", palette: "ai-agents", help: helpAgents},
	"ai:btn:agents:":     {name: "Show in AGENTS", palette: "ai-agents", help: helpAgents},
	"ai:btn:usage:":      {name: "Tokens and cost by model and agent (AI: Usage)", help: helpAgents},
	"ai:btn:attach-file": {name: "Attach file", help: "guides/ai-use.md#files-in-and-out-of-the-chat"},
	"ai:btn:copymsg:":    {name: "Copy message", help: helpChat},
	"ai:btn:copy:":       {name: "Copy code block", help: helpChat},
	"actions:pin":        {name: "Unpin panel", help: helpChat},
	"actions:close":      {name: "Close panel", palette: "toggle-actions-panel", help: helpChat},
	"actions:launcher":   {name: "AI chat", palette: "toggle-actions-panel", help: helpChat},

	// Bottom panel tab bar.
	"panel:new":         {name: "New terminal", palette: "terminal-new-tab", help: helpTerminal},
	"panel:maximize":    {name: "Maximize panel", help: helpTerminal},
	"panel:minimize":    {name: "Minimize panel", help: helpTerminal},
	"panel:close":       {name: "Close panel", palette: "toggle-terminal", help: helpTerminal},
	"panel:term-close:": {name: "Close terminal", palette: "terminal-close-tab", help: helpTerminal},
	"panel:tab-close:":  {name: "Close tab"},

	// Editor tab bar.
	"close:":            {name: "Close", palette: "close-buffer", help: helpTabs},
	"tabs:scroll-left":  {name: "Scroll tabs left", help: helpTabs},
	"tabs:scroll-right": {name: "Scroll tabs right", help: helpTabs},

	// Status bar.
	"status:ai":       {name: "AI chat", palette: "toggle-actions-panel", help: helpChat},
	"status:lsp":      {name: "Language servers", help: "guides/language-servers.md#install-a-language-server"},
	"status:diag":     {name: "Problems", palette: "problems-panel", help: "guides/tasks-problems.md#the-problems-panel"},
	"status:tests":    {name: "Test results", help: "guides/tests.md#read-a-failure"},
	"status:agents":   {name: "Outside agents (MCP server)", help: "guides/mcp.md#let-an-outside-agent-use-termocode"},
	"status:web":      {name: "Web panel: click to stop it", palette: "web-stop"},
	"status:services": {name: "Services", help: "guides/services.md#the-view"},
	"status:ci":       {name: "CI and pull request", help: "guides/git.md#ci-and-pull-requests"},
	"status:update":   {name: "A newer termocode is out", help: "getting-started/install.md#update-termocode"},
	"status:debug":    {name: "Call stack", help: "guides/debug.md#step-through-code"},
	"status:branch":   {name: "Git branch", help: helpBranches},
	"status:indent":   {name: "Indentation"},
	"status:encoding": {name: "File encoding"},
	"status:lang":     {name: "Language", help: "guides/language-servers.md#which-tools-are-supported"},
	"status:term":     {name: "Terminal", palette: "toggle-terminal", help: helpTerminal},
	"status:pos":      {name: "Line and column"},

	// Services view action bar (services_view.go).
	"svc:btn:start-all": {name: "Start all services", local: "a", help: helpServices},
	"svc:btn:stop-all":  {name: "Stop all services", local: "A", help: helpServices},
	"svc:btn:refresh":   {name: "Refresh", local: "R", help: helpServices},
	"cont:more":         {name: "Sections and actions", local: "m", help: "guides/containers.md#keys"},

	// Source Control header.
	"scm:toggle-view": {name: "Tree or list view", help: "guides/git.md#open-the-source-control-panel"},
	"scm:btn:sync":    {name: "Sync changes", help: "guides/git.md#sync-with-the-remote"},
	"scm:btn:more":    {name: "More git actions", help: helpBranches},

	// Find bar.
	"find:chevron":     {name: "Toggle replace", help: helpReplace},
	"find:case":        {name: "Match case", help: helpFind},
	"find:word":        {name: "Match whole word", help: helpFind},
	"find:regex":       {name: "Use regular expression", help: helpFind},
	"find:prev":        {name: "Previous match", help: helpFind},
	"find:next":        {name: "Next match", help: helpFind},
	"find:menu":        {name: "More options", help: helpFind},
	"find:close":       {name: "Close", help: helpFind},
	"find:replace":     {name: "Replace", help: helpReplace},
	"find:replace-all": {name: "Replace all", help: helpReplace},

	// Side-by-side diff winbar.
	"diff:prev": {name: "Previous change", help: helpStageHunk},
	"diff:next": {name: "Next change", help: helpStageHunk},
}

// tooltipSpecFor finds the spec of hit target id.
func tooltipSpecFor(id string) (tooltipSpec, bool) {
	if s, ok := tooltipLabels[id]; ok {
		return s, true
	}
	if i := strings.LastIndexByte(id, ':'); i > 0 {
		if s, ok := tooltipLabels[id[:i+1]]; ok {
			return s, true
		}
	}
	return tooltipSpec{}, false
}

// tooltipLabel is the name and key of hit target id ("" name = none).
func (m Model) tooltipLabel(id string) (name, key string) {
	if strings.HasPrefix(id, "cont:sec:") {
		if strings.Contains(id, "containers") {
			return "Fold section · right-click to prune stopped containers", "Space · z"
		}
		if strings.Contains(id, "images") {
			return "Image actions and dangling image cleanup", "u · P · p"
		}
		if strings.Contains(id, "networks") {
			return "Create or prune networks", "n · p"
		}
		if strings.Contains(id, "volumes") {
			return "Create, prune, or read disk usage", "n · p · d"
		}
		return "Fold section", "Space"
	}
	if name, key, _, ok := itermocTooltip(id); ok {
		return name, key
	}
	if strings.HasPrefix(id, "cont:grp:") {
		return "Project actions: start, stop, restart, or remove", "s · x · r · D"
	}
	if strings.HasPrefix(id, "cont:row:containers/") {
		return "Container actions: start, stop, restart, pause, logs, shell, attach, stats, inspect, port, copy", "s · x · r · p · l · e · t · S · i · o · y · D"
	}
	if strings.HasPrefix(id, "cont:row:images/") {
		return "Image actions: pull, push, tag, run, history, inspect, copy, remove", "u · P · t · Enter · h · i · y · D"
	}
	if strings.HasPrefix(id, "cont:row:networks/") {
		return "Network users and removal", "Space · D"
	}
	if strings.HasPrefix(id, "cont:row:volumes/") {
		return "Volume users and removal", "Space · D"
	}
	if strings.HasPrefix(id, "welcome:card:") {
		idx, err := strconv.Atoi(strings.TrimPrefix(id, "welcome:card:"))
		if err == nil && idx >= 0 && idx < len(welcomeQuickActions()) {
			return welcomeQuickActions()[idx].title, welcomeQuickActions()[idx].shortcut
		}
	}
	if strings.HasPrefix(id, "welcome:") {
		name := "Start page item"
		switch {
		case strings.HasPrefix(id, "welcome:recent:"):
			name = "Recent file"
		case strings.HasPrefix(id, "welcome:workspace:"):
			name = "Workspace"
		case strings.Contains(id, "view-all"):
			name = "All recent files"
		case strings.Contains(id, "browse-all"):
			name = "All workspaces"
		case strings.HasPrefix(id, "welcome:setup:"):
			name = "Setup step"
		}
		return name, ""
	}
	if strings.HasPrefix(id, "explorer:") {
		if id == "explorer:header:actions" {
			return "Explorer actions", ""
		}
		if n := m.explorerNodeForTarget(id); n != nil {
			for _, root := range m.explorer.Roots() {
				if root == n.Path {
					return "Workspace", ""
				}
			}
			if n.IsDir {
				return "Folder", ""
			}
			return "File", ""
		}
	}
	if label, ok := strings.CutPrefix(id, "activity:"); ok {
		key := ""
		if label == "Explorer" {
			key = m.paletteKey("toggle-explorer")
		}
		return label, key
	}
	if id == "ai:profile" { // the click picks; the key goes to the next one
		who := ""
		if m.ai != nil {
			who = ": " + m.ai.activeProfile().Name
		}
		return "Profile" + who + " · click: pick · next:", m.paletteKey("ai-profile-next")
	}
	s, ok := tooltipSpecFor(id)
	if !ok {
		return "", ""
	}
	if s.palette != "" {
		return s.name, m.paletteKey(s.palette)
	}
	return s.name, s.local
}

func (m Model) explorerNodeForTarget(id string) *explorer.Node {
	path := strings.TrimPrefix(id, "explorer:")
	for y := 0; y < m.h; y++ {
		if n, ok := m.explorer.VisibleAt(y); ok && n.Path == path {
			return n
		}
	}
	return nil
}

// paletteKey is the main key of palette command id: the first key the
// palette shows for it (keyHint), "" when nothing is bound.
func (m Model) paletteKey(id string) string {
	a, ok := paletteActions[id]
	if !ok {
		return ""
	}
	static := ""
	for _, it := range paletteItems() {
		if it.ID == id {
			static = it.Hint
			break
		}
	}
	first, _, _ := strings.Cut(keyHint(m.keys.KeysFor(a), static), " / ")
	return first
}

// tooltipText is the label of hit target id as drawn, without padding.
func (m Model) tooltipText(id string) string {
	name, key := m.tooltipLabel(id)
	if name == "" || key == "" {
		return name
	}
	return name + " (" + key + ")"
}

// tooltipHelp is the guide topic of hit target id ("" = none).
func tooltipHelp(id string) string {
	if _, _, help, ok := itermocTooltip(id); ok {
		return help
	}
	if strings.HasPrefix(id, "welcome:card:") {
		return "getting-started/first-steps.md#1-open-a-project"
	}
	if strings.HasPrefix(id, "welcome:") {
		return "getting-started/first-steps.md#1-open-a-project"
	}
	if strings.HasPrefix(id, "explorer:") {
		return "guides/editing.md#open-files"
	}
	if label, ok := strings.CutPrefix(id, "activity:"); ok {
		return activityHelp[label]
	}
	s, _ := tooltipSpecFor(id)
	return s.help
}
