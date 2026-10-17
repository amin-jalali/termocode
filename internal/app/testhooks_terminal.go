package app

// TerminalTabState is what tests read about one terminal tab (shell
// integration U9a, Fix chip U9b, CLI agents U8).
type TerminalTabState struct {
	Name, Cwd, LastCmd string
	LastExit           int
	Busy, Integrated   bool
	FixChip            bool // the chip is set (it shows only while AI is on)
	Agent, AgentState  string
	Hidden             bool // the panel is hidden (Ctrl+T)
}

// TerminalTabStates lists the terminal tabs, shown ones first.
func (m Model) TerminalTabStates() []TerminalTabState {
	var out []TerminalTabState
	add := func(tabs []terminalTab, hidden bool) {
		for _, t := range tabs {
			st := ""
			if t.Agent != "" {
				st = t.AgentState.String()
			}
			out = append(out, TerminalTabState{Name: m.panelEntryNameOf(t), Cwd: t.Cwd, LastCmd: t.LastCmd,
				LastExit: t.LastExit, Busy: t.Busy, Integrated: t.Integrated, FixChip: t.FixChip,
				Agent: t.Agent, AgentState: st, Hidden: hidden})
		}
	}
	add(m.terminalTabs, false)
	add(m.hiddenTerminalTabs, true)
	return out
}

// panelEntryNameOf is the tab label of t (panelEntryName by value).
func (m Model) panelEntryNameOf(t terminalTab) string {
	if title := t.agentTitle(); title != "" {
		return title
	}
	if t.Name != "" {
		return t.Name
	}
	return "shell"
}
