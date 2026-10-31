package app

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentctl"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/session"
)

// The agent control socket (OMX-33): every terminal tab gets
// TERMOCODE_ENV=1, TERMOCODE_TAB_ID and TERMOCODE_SOCKET, and the
// `termocode agent list|prompt|wait|report` CLI talks to the running UI
// over that socket (internal/agentctl). Requests are answered on the UI
// goroutine. Typing into a tab (prompt) asks first: a Deny-first card,
// with "Allow for this session". list, wait and report only read or set
// a tab's agent state.

const confirmKindAgentCtlPrompt confirmKindEnum = 2906

// agentCtlCall is one request waiting for the UI.
type agentCtlCall struct {
	req   agentctl.Request
	reply chan agentctl.Reply // buffered 1
}

// agentCtlMsg brings a request to Update.
type agentCtlMsg struct{ call agentCtlCall }

// agentCtlTickMsg checks the waits.
type agentCtlTickMsg struct{}

// agentCtlReqs carries requests from the socket to the UI.
var agentCtlReqs = make(chan agentCtlCall)

// agentCtlStarted: the listener runs (once per process).
var (
	agentCtlOnce   sync.Once
	agentCtlPath   atomic.Value // string: the socket's path ("" when none)
	agentCtlLn     atomic.Pointer[agentCtlCloser]
	agentCtlTabSeq atomic.Int64
)

type agentCtlCloser struct{ close func() error }

// agentCtlState is the UI side: session permission, pending card, waits.
type agentCtlState struct {
	allowPrompt bool
	pending     *agentCtlCall
	waits       []agentCtlWait
}

type agentCtlWait struct {
	call     agentCtlCall
	until    string
	deadline time.Time
}

func (m *Model) ctlS() *agentCtlState {
	s := m.aiS()
	if s.ctl == nil {
		s.ctl = &agentCtlState{}
	}
	return s.ctl
}

// agentCtlSocketPath is where the socket of root is ("" when unknown).
func agentCtlSocketPath(root string) string {
	p, err := session.PathsFor(root)
	if err != nil {
		return ""
	}
	return p.AgentSocket
}

// agentCtlTabEnv is the env of a new terminal tab with id.
func agentCtlTabEnv(id string) map[string]string {
	env := map[string]string{agentctl.EnvMarker: "1", agentctl.EnvTab: id}
	if p, _ := agentCtlPath.Load().(string); p != "" {
		env[agentctl.EnvSocket] = p
	} else if p := agentCtlSocketPath(aiWorkspaceRoot()); p != "" {
		env[agentctl.EnvSocket] = p
	}
	return env
}

// agentCtlNextTabID is a new tab id ("t1", "t2" …; unique per process).
func agentCtlNextTabID() string { return "t" + strconv.FormatInt(agentCtlTabSeq.Add(1), 10) }

// agentCtlInitCmd starts the listener (not in headless runs) and waits
// for the first request.
func agentCtlInitCmd() tea.Cmd {
	if headless() {
		return nil
	}
	agentCtlOnce.Do(func() {
		path := agentCtlSocketPath(aiWorkspaceRoot())
		if path == "" {
			return
		}
		ln, err := mcp.ListenUnix(path)
		if err != nil {
			recordError("[agent-ctl] not started: " + err.Error())
			return
		}
		agentCtlPath.Store(path)
		agentCtlLn.Store(&agentCtlCloser{close: ln.Close})
		go agentctl.Serve(ln, session.CheckPeer, agentCtlHandle)
	})
	return waitAgentCtl()
}

// StopAgentCtl closes the control socket (at exit).
func StopAgentCtl() {
	if c := agentCtlLn.Swap(nil); c != nil {
		_ = c.close()
	}
}

// agentCtlHandle runs on a connection's goroutine: it hands the request
// to the UI and waits for the answer.
func agentCtlHandle(req agentctl.Request) agentctl.Reply {
	call := agentCtlCall{req: req, reply: make(chan agentctl.Reply, 1)}
	limit := 2 * time.Minute // a prompt may wait for the card
	if req.Op == agentctl.OpWait {
		limit = agentCtlWaitTimeout(req) + 10*time.Second
	}
	select {
	case agentCtlReqs <- call:
	case <-time.After(10 * time.Second):
		return agentctl.Reply{Error: "termocode is busy"}
	}
	select {
	case r := <-call.reply:
		return r
	case <-time.After(limit):
		return agentctl.Reply{Error: "no answer from termocode"}
	}
}

func waitAgentCtl() tea.Cmd {
	return func() tea.Msg { return agentCtlMsg{call: <-agentCtlReqs} }
}

// agentCtlWaitTimeout is a wait's limit (default 10 minutes, at most 2 hours).
func agentCtlWaitTimeout(r agentctl.Request) time.Duration {
	if r.TimeoutS <= 0 {
		return 10 * time.Minute
	}
	return min(time.Duration(r.TimeoutS)*time.Second, 2*time.Hour)
}

// handleAgentCtlMsg routes the control messages. ok=false: not ours.
func (m *Model) handleAgentCtlMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case agentCtlMsg:
		return tea.Batch(m.onAgentCtl(msg.call), waitAgentCtl()), true
	case agentCtlTickMsg:
		return m.agentCtlCheckWaits(), true
	}
	return nil, false
}

// agentCtlTab is a tab with its control id.
type agentCtlTab struct {
	t      *terminalTab
	id     string
	active bool
}

// agentCtlTabs lists the shown and hidden tabs with their ids (the id
// the tab's shell got, else "b<buffer>").
func (m *Model) agentCtlTabs() []agentCtlTab {
	var out []agentCtlTab
	var bufs []int
	for i := range m.terminalTabs {
		out = append(out, agentCtlTab{t: &m.terminalTabs[i], active: m.termOpen && i == m.terminalActiveTab})
		bufs = append(bufs, m.terminalTabs[i].BufID)
	}
	for i := range m.hiddenTerminalTabs {
		out = append(out, agentCtlTab{t: &m.hiddenTerminalTabs[i]})
		bufs = append(bufs, m.hiddenTerminalTabs[i].BufID)
	}
	ids := map[string]string{}
	if m.nvim != nil && len(bufs) > 0 {
		var raw string
		if err := m.nvim.ExecLuaResult(agentCtlIDsLua, &raw, bufs); err == nil {
			_ = json.Unmarshal([]byte(raw), &ids)
		}
	}
	for i := range out {
		out[i].id = ids[strconv.Itoa(out[i].t.BufID)]
		if out[i].id == "" {
			out[i].id = "b" + strconv.Itoa(out[i].t.BufID)
		}
	}
	return out
}

// agentCtlIDsLua reads b:termocode_tab_id of the buffers.
const agentCtlIDsLua = `local bufs = ...
local out = vim.empty_dict()
for _, b in ipairs(bufs) do
  if vim.api.nvim_buf_is_valid(b) then out[tostring(b)] = vim.b[b].termocode_tab_id or '' end
end
return vim.json.encode(out)`

func (m *Model) agentCtlFind(id string) (agentCtlTab, bool) {
	id = strings.TrimSpace(id)
	for _, t := range m.agentCtlTabs() {
		if t.id == id || "b"+strconv.Itoa(t.t.BufID) == id {
			return t, true
		}
	}
	return agentCtlTab{}, false
}

func agentCtlStateName(t *terminalTab) string {
	if t.Agent == "" {
		return ""
	}
	return t.AgentState.String()
}

// onAgentCtl answers one request (or keeps it: a card, a wait).
func (m *Model) onAgentCtl(call agentCtlCall) tea.Cmd {
	r := call.req
	answer := func(rep agentctl.Reply) tea.Cmd { call.reply <- rep; return nil }
	fail := func(f string, a ...any) tea.Cmd { return answer(agentctl.Reply{Error: fmt.Sprintf(f, a...)}) }
	switch r.Op {
	case agentctl.OpList:
		var tabs []agentctl.Tab
		for _, t := range m.agentCtlTabs() {
			src := ""
			if t.t.Agent != "" {
				src = "screen"
				if t.t.AgentHook {
					src = "hook"
				}
			}
			tabs = append(tabs, agentctl.Tab{ID: t.id, Name: t.t.Name, Agent: t.t.Agent, State: agentCtlStateName(t.t), Source: src, Active: t.active})
		}
		return answer(agentctl.Reply{OK: true, Tabs: tabs})
	case agentctl.OpPrompt:
		t, ok := m.agentCtlFind(r.Tab)
		switch {
		case !ok:
			return fail("no terminal tab %q (termocode agent list)", r.Tab)
		case strings.TrimSpace(r.Text) == "":
			return fail("the prompt is empty")
		case len(r.Text) > 32<<10:
			return fail("the prompt is over 32 KB")
		}
		if m.ctlS().allowPrompt {
			return answer(m.agentCtlSend(t, r))
		}
		if m.ctlS().pending != nil || m.confirmOpen {
			return fail("termocode is asking something else: try again")
		}
		return m.askAgentCtlPrompt(call, t)
	case agentctl.OpWait:
		t, ok := m.agentCtlFind(r.Tab)
		until := r.Until
		if until == "" {
			until = "done"
		}
		switch {
		case !ok:
			return fail("no terminal tab %q (termocode agent list)", r.Tab)
		case !agentctl.ValidState(until):
			return fail("until must be one of %s", strings.Join(agentctl.States, ", "))
		case t.t.Agent != "" && agentCtlStateName(t.t) == until:
			return answer(agentctl.Reply{OK: true, State: until})
		}
		st := m.ctlS()
		st.waits = append(st.waits, agentCtlWait{call: call, until: until, deadline: time.Now().Add(agentCtlWaitTimeout(r))})
		if len(st.waits) == 1 {
			return agentCtlTick()
		}
		return nil
	case agentctl.OpReport:
		return answer(m.agentCtlReport(r))
	}
	return fail("unknown op %q (list, prompt, wait, report)", r.Op)
}

// agentCtlPoll is how often the waits look at the tabs.
var agentCtlPoll = 300 * time.Millisecond

func agentCtlTick() tea.Cmd {
	return tea.Tick(agentCtlPoll, func(time.Time) tea.Msg { return agentCtlTickMsg{} })
}

// agentCtlCheckWaits answers the waits whose tab reached its state, is
// gone, or timed out; it ticks again while any are left.
func (m *Model) agentCtlCheckWaits() tea.Cmd {
	st := m.ctlS()
	if len(st.waits) == 0 {
		return nil
	}
	now := time.Now()
	left := st.waits[:0]
	for _, w := range st.waits {
		t, ok := m.agentCtlFind(w.call.req.Tab)
		switch {
		case !ok:
			w.call.reply <- agentctl.Reply{Error: "the tab closed"}
		case t.t.Agent != "" && agentCtlStateName(t.t) == w.until:
			w.call.reply <- agentctl.Reply{OK: true, State: w.until}
		case now.After(w.deadline):
			w.call.reply <- agentctl.Reply{Error: "timed out; the agent is " + orDash(agentCtlStateName(t.t))}
		default:
			left = append(left, w)
		}
	}
	st.waits = left
	if len(left) == 0 {
		return nil
	}
	return agentCtlTick()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// agentCtlSend types the prompt into the tab.
func (m *Model) agentCtlSend(t agentCtlTab, r agentctl.Request) agentctl.Reply {
	if m.nvim == nil {
		return agentctl.Reply{Error: "the editor is not running"}
	}
	text := r.Text
	if r.Enter == nil || *r.Enter {
		text = strings.TrimRight(text, "\r\n") + "\r"
	}
	var tick int
	if err := m.nvim.ExecLuaResult(aiTermSendLua, &tick, t.t.BufID, text); err != nil || tick < 0 {
		return agentctl.Reply{Error: "could not type into the tab"}
	}
	return agentctl.Reply{OK: true}
}

// askAgentCtlPrompt shows the prompt card (Deny first).
func (m *Model) askAgentCtlPrompt(call agentCtlCall, t agentCtlTab) tea.Cmd {
	m.ctlS().pending = &call
	name := t.t.agentTitle()
	if name == "" {
		name = t.t.Name
	}
	preview := runewidth.Truncate(strings.Join(strings.Fields(ai.Redact(call.req.Text)), " "), 200, "…")
	body := "A program on this computer (termocode agent prompt) wants to type into the tab " + name + " (" + t.id + "):\n\n    " + preview +
		"\n\nAllow for this session lets later prompts in without asking, until termocode quits."
	m.confirm = confirm.Risky("Let a script type into a terminal tab?", body,
		confirm.Button{ID: "deny", Title: "Deny"},
		confirm.Button{ID: "once", Title: "Allow once", Style: confirm.StylePrimary},
		confirm.Button{ID: "session", Title: "Allow for this session"})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentCtlPrompt
	return nil
}

// answerAgentCtlPrompt answers the card ("" = Esc = Deny).
func (m *Model) answerAgentCtlPrompt(id string) tea.Cmd {
	st := m.ctlS()
	call := st.pending
	st.pending = nil
	if call == nil {
		return nil
	}
	if id != "once" && id != "session" {
		call.reply <- agentctl.Reply{Error: "the user denied it"}
		return nil
	}
	st.allowPrompt = id == "session"
	t, ok := m.agentCtlFind(call.req.Tab)
	if !ok {
		call.reply <- agentctl.Reply{Error: "the tab closed"}
		return nil
	}
	call.reply <- m.agentCtlSend(t, call.req)
	return nil
}

// agentCtlReport sets a tab's agent state from the agent's own hook
// (OMX-34); the screen guess then leaves that tab alone.
func (m *Model) agentCtlReport(r agentctl.Request) agentctl.Reply {
	t, ok := m.agentCtlFind(r.Tab)
	if !ok {
		return agentctl.Reply{Error: fmt.Sprintf("no terminal tab %q", r.Tab)}
	}
	var st agents.State
	switch r.State {
	case "working":
		st = agents.Working
	case "waiting":
		st = agents.Waiting
	case "done":
		st = agents.Done
	default:
		return agentctl.Reply{Error: "state must be one of " + strings.Join(agentctl.States, ", ")}
	}
	if r.Agent != "" {
		if a, ok := agents.ByID(r.Agent); ok {
			t.t.Agent = a.ID
		} else if t.t.Agent == "" {
			return agentctl.Reply{Error: fmt.Sprintf("unknown agent %q", r.Agent)}
		}
	}
	if t.t.Agent == "" {
		return agentctl.Reply{Error: "the tab runs no known agent: pass --agent"}
	}
	t.t.AgentState, t.t.AgentHook = st, true
	return agentctl.Reply{OK: true, State: r.State}
}
