package app

import (
	"encoding/json"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/shellint"
)

// ── Shell integration (U9a, competitive plan T1) ─────────────────────────
//
// bash, zsh and fish tabs start with small scripts (internal/shellint)
// that print OSC 133 (prompt start, command start with its text, command
// end with the exit code) and OSC 7 (the folder). nvim's TermRequest
// autocmd (0.10+) hands each sequence to Go with one rpcnotify — push
// only, nothing polls. Each tab then knows its last command, exit code,
// whether a command runs, and its folder; integrated tabs skip the
// b:term_title poll for the folder.
//
// Shells without a script, nvim 0.9 and the setting off start exactly as
// before. config.json "terminal_shell_integration": false turns it off.

// shellIntegrationOn reads config.json "terminal_shell_integration"
// (default true).
func shellIntegrationOn() bool {
	path, err := settingsConfigPath()
	if err != nil {
		return true
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	var cfg struct {
		On *bool `json:"terminal_shell_integration"`
	}
	if json.Unmarshal(b, &cfg) != nil || cfg.On == nil {
		return true
	}
	return *cfg.On
}

// termShellSpec is the JSON the spawn Lua reads (termShellIntLua):
// plain is the old termopen command (the shell as a string); argv and
// env start the shell with the integration.
type termShellSpec struct {
	Plain string            `json:"plain"`
	Argv  []string          `json:"argv,omitempty"`
	Env   map[string]string `json:"env,omitempty"`
}

// termShellSpecJSON returns the spawn spec for shell. Without the
// integration (setting off, unknown shell, scripts not writable) only
// plain is set.
func termShellSpecJSON(shell string) string {
	spec := termShellSpec{Plain: shell}
	if shellIntegrationOn() && shellint.Supported(shell) {
		dir := shellint.Dir()
		if err := shellint.Install(dir); err == nil {
			if s, ok := shellint.For(shell, dir, os.Getenv); ok {
				spec.Argv, spec.Env = s.Argv, s.Env
			}
		} else {
			recordError("[shellint] install: " + err.Error())
		}
	}
	b, _ := json.Marshal(spec)
	return string(b)
}

// termShellIntLua defines _G.termocode_shellint(spec) → cmd, env for
// termopen, and (once) the TermRequest autocmd that forwards OSC 133 /
// OSC 7 to Go. On nvim without TermRequest the shell starts plain.
const termShellIntLua = `
	_G.termocode_shellint = _G.termocode_shellint or function(spec)
		if spec.argv and vim.fn.exists('##TermRequest') == 1 then
			if not _G.termocode_shellint_au then
				_G.termocode_shellint_au = true
				vim.api.nvim_create_autocmd('TermRequest', {
					group = vim.api.nvim_create_augroup('termocode_shellint', { clear = true }),
					callback = function(ev)
						local s = ev.data
						if type(s) == 'table' then s = s.sequence end
						if type(s) ~= 'string' then s = vim.v.termrequest end
						if type(s) ~= 'string' then return end
						if s:sub(1, 6) == '\027]133;' or s:sub(1, 4) == '\027]7;' then
							if _G.termocode_notify then _G.termocode_notify('termocode_term_osc', ev.buf, s) end
						end
					end,
				})
			end
			return spec.argv, spec.env
		end
		return spec.plain, nil
	end
`

// termTabByBuf finds the tab of terminal buffer buf among the shown and
// the hidden (Ctrl+T) tabs. shown is its index in m.terminalTabs, -1
// for a hidden tab.
func (m *Model) termTabByBuf(buf int) (tab *terminalTab, shown int) {
	for i := range m.terminalTabs {
		if m.terminalTabs[i].BufID == buf {
			return &m.terminalTabs[i], i
		}
	}
	for i := range m.hiddenTerminalTabs {
		if m.hiddenTerminalTabs[i].BufID == buf {
			return &m.hiddenTerminalTabs[i], -1
		}
	}
	return nil, -1
}

// termTabVisible: the tab is the one on show in an open terminal panel.
func (m Model) termTabVisible(shown int) bool {
	return shown >= 0 && m.termOpen && !m.terminalMinimized &&
		m.panelActive == panelKindTerminal && shown == m.terminalActiveTab
}

// termCmdMax caps the stored command line.
const termCmdMax = 2000

// onTermOSC handles one OSC 133 / OSC 7 sequence of terminal buffer buf.
func (m *Model) onTermOSC(buf int, seq string) tea.Cmd {
	e, ok := shellint.Parse(seq)
	if !ok {
		return nil
	}
	if e.Kind == shellint.CommandStart {
		m.dropTermHint(buf) // terminal_restore.go: the user runs something new
	}
	t, shown := m.termTabByBuf(buf)
	if t == nil {
		m.onPaneOSC(buf, e)
		return nil
	}
	t.Integrated = true
	switch e.Kind {
	case shellint.PromptStart:
		t.Busy = false
	case shellint.CommandStart:
		cmd := e.Cmd
		if len(cmd) > termCmdMax {
			cmd = cmd[:termCmdMax]
		}
		t.Busy, t.LastCmd, t.CmdAt, t.FixChip = true, cmd, time.Now(), false
		// An agent CLI (agent_tabs.go): watch its screen until this
		// command ends. A palette start lands here too, via its shell.
		if a, ok := agents.FromCommand(cmd); ok && (t.Agent == "" || t.Agent == a.ID) {
			if t.Agent == "" {
				t.AgentState = agents.Working
				m.agentWatch(buf, true)
			}
			t.Agent, t.agentFromCmd = a.ID, true
		}
	case shellint.CommandEnd:
		t.Busy = false
		if e.HasExit {
			t.LastExit = e.Exit
		} else {
			t.LastExit = 0
		}
		// The Fix chip (U9b): a failed command in the tab on show. Ctrl+C
		// (130) is the user's own stop, not a failure to fix.
		t.FixChip = t.LastExit != 0 && t.LastExit != 130 && t.LastCmd != "" && m.termTabVisible(shown)
		if t.Agent != "" && t.agentFromCmd {
			m.agentWatch(buf, false)
			t.Agent, t.AgentState, t.agentFromCmd = "", agents.Unknown, false
		}
	case shellint.Cwd:
		if !termHostLocal(e.Host) {
			return nil // OSC 7 from a remote shell (ssh): not our folder
		}
		t.Cwd = e.Cwd
		if t.Task == "" {
			t.Name = terminalCwdBasename(e.Cwd)
			if t.Name == "" {
				t.Name = "shell"
			}
		}
		if shown >= 0 && shown == m.terminalActiveTab {
			m.terminalCwd = e.Cwd
		}
	}
	return nil
}

// onPaneOSC records an event of a split pane's shell (pane 1 and up): its
// folder, and whether a command runs and which one.
func (m *Model) onPaneOSC(buf int, e shellint.Event) {
	p := m.termPaneByBuf(buf)
	if p == nil {
		return
	}
	switch e.Kind {
	case shellint.PromptStart, shellint.CommandEnd:
		p.Busy = false
	case shellint.CommandStart:
		cmd := e.Cmd
		if len(cmd) > termCmdMax {
			cmd = cmd[:termCmdMax]
		}
		p.Busy, p.LastCmd = true, cmd
	case shellint.Cwd:
		if termHostLocal(e.Host) {
			p.Cwd = e.Cwd
		}
	}
}

// termPaneByBuf finds the split pane (pane 1 and up) of terminal buffer
// buf among the shown and the hidden tabs.
func (m *Model) termPaneByBuf(buf int) *termPane {
	for _, tabs := range [][]terminalTab{m.terminalTabs, m.hiddenTerminalTabs} {
		for i := range tabs {
			for k := range tabs[i].Splits {
				if tabs[i].Splits[k].BufID == buf {
					return &tabs[i].Splits[k]
				}
			}
		}
	}
	return nil
}

// termHostLocal: the OSC 7 host names this machine.
func termHostLocal(host string) bool {
	if host == "" || host == "localhost" {
		return true
	}
	h, err := os.Hostname()
	if err != nil {
		return true
	}
	return strings.EqualFold(host, h) || strings.EqualFold(host, strings.SplitN(h, ".", 2)[0])
}

func init() {
	registerNotifyHandler("termocode_term_osc", func(m *Model, args []any) tea.Cmd {
		return m.onTermOSC(notifyArgInt(args, 0), notifyArgString(args, 1))
	})
}
