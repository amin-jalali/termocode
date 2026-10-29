package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Terminal restore (G-RST). A detach keeps every shell alive in the
// session server; a restart of termocode ends them. So the terminal tabs
// of a workspace, their split panes, folders and focus are kept in the
// workspace session, and the next start opens fresh shells in the same
// places. Output, scrollback and environment values are never kept. A
// command line is kept only with terminal_restore_commands; otherwise only
// the name of the program that was running, for a hint.

// savedTerminals is the terminal panel of a workspace as the session
// keeps it.
type savedTerminals struct {
	// Open: the panel was on show. A hidden panel's tabs come back on the
	// next Ctrl+T.
	Open bool `json:"open"`
	// Active is the index of the tab on show.
	Active int            `json:"active,omitempty"`
	Tabs   []savedTermTab `json:"tabs"`
}

// savedTermTab is one terminal tab.
type savedTermTab struct {
	Name string `json:"name,omitempty"`
	// Focus is the pane that had the keyboard.
	Focus int `json:"focus,omitempty"`
	// Panes are the tab's shells: the tab's own first, then the split panes
	// in the order they were made.
	Panes []savedTermPane `json:"panes"`
}

// savedTermPane is one shell of a tab.
type savedTermPane struct {
	// Cwd is the folder the shell was in (shell integration's, when known).
	Cwd string `json:"cwd,omitempty"`
	// Split ("right" / "down") and Parent say how a split pane was made
	// (pane 1 and up), like a layout file's panes.
	Split  string `json:"split,omitempty"`
	Parent int    `json:"parent,omitempty"`
	// Program is the name of the program that was running ("npm"), for a
	// hint. Cmd is its command line, only with terminal_restore_commands.
	Program string `json:"program,omitempty"`
	Cmd     string `json:"cmd,omitempty"`
}

// terminalSnapshot is the terminal part of the session now: the tabs on
// show, or those of a hidden panel, or the saved ones not opened yet. Tabs
// of the task runner and the Services view are left out: their job is not
// a shell of the user's. nil when there is no terminal. withCmds keeps the
// command line of a running program.
func (m *Model) terminalSnapshot(withCmds bool) *savedTerminals {
	if p := m.termRestorePending; p != nil {
		if withCmds {
			return p
		}
		out := &savedTerminals{Open: p.Open, Active: p.Active}
		for _, t := range p.Tabs {
			t.Panes = append([]savedTermPane(nil), t.Panes...)
			for k := range t.Panes {
				t.Panes[k].Cmd = "" // the setting was turned off since
			}
			out.Tabs = append(out.Tabs, t)
		}
		return out
	}
	tabs, active, open := m.terminalTabs, m.terminalActiveTab, m.termOpen
	if len(tabs) == 0 {
		tabs, active, open = m.hiddenTerminalTabs, m.hiddenTerminalActive, false
	}
	out := &savedTerminals{Open: open}
	for i, t := range tabs {
		if t.isJob() {
			continue
		}
		if i == active {
			out.Active = len(out.Tabs)
		}
		st := savedTermTab{Name: t.Name, Focus: t.focusedPane()}
		st.Panes = append(st.Panes, savedPane(t.Cwd, t.Busy, t.LastCmd, withCmds))
		for _, s := range t.Splits {
			p := savedPane(s.Cwd, s.Busy, s.LastCmd, withCmds)
			p.Split, p.Parent = layoutPaneSplits[s.Dir], s.Parent
			st.Panes = append(st.Panes, p)
		}
		out.Tabs = append(out.Tabs, st)
	}
	if len(out.Tabs) == 0 {
		return nil
	}
	return out
}

// observeTerminals hands the terminal part of the session to the session
// writer, which writes it when it changed. Cheap: Go state only. Nothing is
// written before the saved terminals were opened at start.
func (m *Model) observeTerminals() {
	if !m.termRestoreReady {
		return
	}
	var snap *savedTerminals
	if !m.termRestoreOff {
		snap = m.terminalSnapshot(m.termRestoreCmds)
	}
	m.ensureSessionWriter().terminalsChanged(snap)
}

// savedPane is one shell in the session: its folder, and what it was
// running.
func savedPane(cwd string, busy bool, cmd string, withCmds bool) savedTermPane {
	p := savedTermPane{Cwd: cwd}
	if busy && cmd != "" {
		p.Program = programName(cmd)
		if withCmds && p.Program != "" {
			p.Cmd = cmd
		}
	}
	return p
}

// programName is the name of the program a command line runs: its first
// word after any NAME=value, without the folder ("./bin/server" → "server").
func programName(cmd string) string {
	for _, f := range strings.Fields(cmd) {
		if strings.Contains(f, "=") {
			continue
		}
		name := filepath.Base(f)
		if len(name) > 40 {
			name = name[:40]
		}
		return name
	}
	return ""
}

// configBool reads the bool key of config.json; def when it is missing or
// the file can not be read.
func configBool(key string, def bool) bool {
	path, err := settingsConfigPath()
	if err != nil {
		return def
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return def
	}
	var cfg map[string]json.RawMessage
	if json.Unmarshal(b, &cfg) != nil {
		return def
	}
	var v bool
	if raw, ok := cfg[key]; !ok || json.Unmarshal(raw, &v) != nil {
		return def
	}
	return v
}

// terminalRestoreOn reads config.json "terminal_restore" (default true).
func terminalRestoreOn() bool { return configBool("terminal_restore", true) }

// terminalRestoreCommandsOn reads config.json "terminal_restore_commands"
// (default false): keep the last command line of a pane that was running
// one, so Enter can run it again after a restart.
func terminalRestoreCommandsOn() bool { return configBool("terminal_restore_commands", false) }

func init() {
	aiSettingPolicies["terminal_restore"] = aiNever("decides what is kept of the user's terminals between starts; a file-only switch the user edits in config.json")
	aiSettingPolicies["terminal_restore_commands"] = aiNever("keeps command lines on disk and re-runs them; a file-only switch the user edits in config.json")
}
