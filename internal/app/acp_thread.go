package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/acp"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ACP agent threads (CMP-83): Agent: Chat with an ACP Agent… starts an
// outside agent that speaks the Agent Client Protocol (a program the
// user lists in <config>/ai/acp_agents.json) and makes it the chat's
// thread: each message goes to the agent's session, its reply, tool calls
// and plan stream into the chat. The agent runs its own tools, but every
// permission it asks for and every file it writes is a termocode card
// with the safe choice first (a write shows the diff). Files are read and
// written only inside the workspace. Running the command again ends the
// thread and stops the program.

const (
	pickerKindACPAgents      pickerKindEnum  = 3460
	confirmKindACPPermission confirmKindEnum = 3460
	confirmKindACPWrite      confirmKindEnum = 3461
)

// acpStartTimeout caps the start, initialize and session/new.
const acpStartTimeout = 30 * time.Second

// acpAgentsPath is the agents file (a var for tests).
var acpAgentsPath = func() (string, error) {
	d, err := ai.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "ai", "acp_agents.json"), nil
}

// acpStartAgent starts the program in dir and connects to it; stop ends
// it (a var for tests).
var acpStartAgent = func(a acp.Agent, dir string, h acp.Handler) (*acp.Client, func(), error) {
	cmd := spawn.Command(a.Command, a.Args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	for k, v := range a.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stderr = io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	c := acp.NewClient(out, in, h)
	go func() { _ = cmd.Wait() }()
	stop := func() {
		_ = c.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	return c, stop, nil
}

// acpThread is the running agent thread.
type acpThread struct {
	name   string
	root   string
	client *acp.Client
	prov   *acp.Provider
	stop   func()
	asks   chan *acpAsk
	cur    *acpAsk // the card on screen
}

// acpAsk is one question of the agent for the user.
type acpAsk struct {
	perm  *acp.PermissionRequest
	write *acp.WriteRequest
	old   string      // the file before the write ("" for a new file)
	reply chan string // the option id / "write"; "" is no
}

type (
	acpStartedMsg struct {
		t   *acpThread
		err error
	}
	acpAskMsg struct {
		t   *acpThread
		ask *acpAsk
	}
	acpEndedMsg struct{ t *acpThread }
)

// acpHandler serves the agent's calls: reads inside the workspace, and
// asks the user (through the thread's asks) before permissions and writes.
type acpHandler struct {
	root string
	asks chan *acpAsk
}

func (h *acpHandler) ask(ctx context.Context, a *acpAsk) string {
	select {
	case h.asks <- a:
	case <-ctx.Done():
		return ""
	}
	select {
	case r := <-a.reply:
		return r
	case <-ctx.Done():
		return ""
	}
}

func (h *acpHandler) RequestPermission(ctx context.Context, r acp.PermissionRequest) (string, error) {
	return h.ask(ctx, &acpAsk{perm: &r, reply: make(chan string, 1)}), nil
}

func (h *acpHandler) ReadTextFile(_ context.Context, r acp.ReadRequest) (string, error) {
	return acp.ReadInside(h.root, r.Path, r.Line, r.Limit)
}

func (h *acpHandler) WriteTextFile(ctx context.Context, w acp.WriteRequest) error {
	p, err := acp.Inside(h.root, w.Path)
	if err != nil {
		return err
	}
	old := ""
	if b, err := os.ReadFile(p); err == nil {
		old = string(b)
	}
	if h.ask(ctx, &acpAsk{write: &w, old: old, reply: make(chan string, 1)}) != "write" {
		return acp.ErrDenied
	}
	return acp.WriteInside(h.root, w.Path, w.Content)
}

func acpPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "acp-thread", Title: "Agent: Chat with an ACP Agent… / End"},
		{ID: "acp-agents-edit", Title: "Open Config: ACP Agents"},
	}
}

// dispatchACPPalette runs the ACP palette ids.
func (m *Model) dispatchACPPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "acp-thread":
		return m.toggleACPThread(), true
	case "acp-agents-edit":
		return m.editACPAgents(), true
	}
	return nil, false
}

// toggleACPThread lists the agents, or ends the running thread.
func (m *Model) toggleACPThread() tea.Cmd {
	s := m.aiS()
	if s.acp != nil {
		return m.endACPThread("Ended the chat with " + s.acp.name)
	}
	path, err := acpAgentsPath()
	if err != nil {
		return m.aiToastErr("ACP agents", err)
	}
	agents, err := acp.LoadAgents(path)
	if err != nil {
		return m.aiToastErr("ACP agents", err)
	}
	if len(agents) == 0 {
		return m.aiInfo("No ACP agents yet: run Open Config: ACP Agents and add one (name, command, args)")
	}
	items := make([]picker.Item, 0, len(agents))
	for i, a := range agents {
		items = append(items, picker.Item{ID: strconv.Itoa(i), Title: a.Name, Hint: strings.Join(append([]string{a.Command}, a.Args...), " ")})
	}
	m.picker = picker.NewItems(" Chat with an ACP Agent ", items)
	m.picker.SetNote([]string{"The agent becomes this chat's thread. It asks you before each permission and each file write."})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindACPAgents
	return nil
}

// onACPAgentPicked starts the agent off the main loop.
func (m *Model) onACPAgentPicked(id string) tea.Cmd {
	path, err := acpAgentsPath()
	if err != nil {
		return m.aiToastErr("ACP agents", err)
	}
	agents, err := acp.LoadAgents(path)
	i, aerr := strconv.Atoi(id)
	if err != nil || aerr != nil || i < 0 || i >= len(agents) {
		return m.aiInfo("That ACP agent is no longer in the list")
	}
	a := agents[i]
	root := aiWorkspaceRoot()
	asks := make(chan *acpAsk, 8)
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, "Starting "+a.Name+"…")
	return tea.Batch(c, func() tea.Msg {
		t := &acpThread{name: a.Name, root: root, asks: asks}
		client, stop, err := acpStartAgent(a, root, &acpHandler{root: root, asks: asks})
		if err != nil {
			return acpStartedMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), acpStartTimeout)
		defer cancel()
		if err := client.Initialize(ctx); err != nil {
			stop()
			return acpStartedMsg{err: err}
		}
		sid, err := client.NewSession(ctx, root)
		if err != nil {
			stop()
			return acpStartedMsg{err: err}
		}
		t.client, t.stop = client, stop
		t.prov = &acp.Provider{Client: client, Session: sid, Title: a.Name}
		return acpStartedMsg{t: t}
	})
}

// handleACPMsg routes the thread's messages. ok=false: not ours.
func (m *Model) handleACPMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case acpStartedMsg:
		return m.onACPStarted(msg), true
	case acpAskMsg:
		return m.onACPAsk(msg), true
	case acpEndedMsg:
		if m.ai != nil && m.ai.acp == msg.t {
			return m.endACPThread(msg.t.name + " ended"), true
		}
		return nil, true
	}
	return nil, false
}

func (m *Model) onACPStarted(msg acpStartedMsg) tea.Cmd {
	if msg.err != nil {
		return m.aiToastErr("Could not start the ACP agent", fmt.Errorf("%s", ai.Redact(msg.err.Error())))
	}
	s := m.aiS()
	if s.acp != nil { // one thread at a time: the old one ends
		s.acp.stop()
	}
	s.acp = msg.t
	open := m.newAIChat()
	s.conv.Title = msg.t.name
	return tea.Batch(open, acpWait(msg.t),
		m.aiInfo("Chatting with "+msg.t.name+": messages go to the agent · run Agent: Chat with an ACP Agent… again to end"))
}

// acpWait waits for the agent's next question or its end.
func acpWait(t *acpThread) tea.Cmd {
	return func() tea.Msg {
		select {
		case a := <-t.asks:
			return acpAskMsg{t: t, ask: a}
		case <-t.client.Done():
			return acpEndedMsg{t: t}
		}
	}
}

// onACPAsk shows the question's card; another card on screen: ask again
// soon (the agent waits).
func (m *Model) onACPAsk(msg acpAskMsg) tea.Cmd {
	s := m.aiS()
	if s.acp != msg.t {
		msg.ask.reply <- "" // the thread ended: no
		return nil
	}
	if m.confirmOpen || msg.t.cur != nil {
		return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg { return msg })
	}
	msg.t.cur = msg.ask
	switch {
	case msg.ask.perm != nil:
		m.showACPPermission(msg.t, msg.ask.perm)
	case msg.ask.write != nil:
		m.showACPWrite(msg.t, msg.ask)
	}
	if msg.t.client == nil {
		return nil // a test thread
	}
	return acpWait(msg.t)
}

// showACPPermission is the permission card: the agent's first reject
// option is the safe button (Cancel when it offers none).
func (m *Model) showACPPermission(t *acpThread, r *acp.PermissionRequest) {
	title := r.Tool.Title
	if title == "" {
		title = r.Tool.Kind
	}
	text := t.name + " wants to: " + title
	if r.Tool.Kind != "" && r.Tool.Kind != title {
		text += "\nKind: " + r.Tool.Kind
	}
	for _, l := range r.Tool.Location {
		text += "\n    " + acpRel(t.root, l.Path)
	}
	text += "\n\nThe agent runs it itself. Cancel or reject: it does not."
	safe := confirm.Button{ID: "cancel", Title: "Cancel"}
	var actions []confirm.Button
	picked := false
	for _, o := range r.Options {
		b := confirm.Button{ID: "opt:" + o.ID, Title: o.Name}
		if !picked && strings.HasPrefix(o.Kind, "reject") {
			safe, picked = b, true
			continue
		}
		if o.Kind == "allow_once" {
			b.Style = confirm.StylePrimary
		}
		actions = append(actions, b)
	}
	m.confirm = confirm.Risky(t.name+": allow?", text, safe, actions...).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindACPPermission
}

// acpWriteCardText is the write card's body: the file and the diff.
func acpWriteCardText(name, root, path, oldText, newText string) string {
	verb := "change"
	if oldText == "" {
		verb = "create"
	}
	return fmt.Sprintf("%s wants to %s %s\n\n%s", name, verb, acpRel(root, path), acp.DiffPreview(oldText, newText, 40))
}

func (m *Model) showACPWrite(t *acpThread, a *acpAsk) {
	text := acpWriteCardText(t.name, t.root, a.write.Path, a.old, a.write.Content)
	m.confirm = confirm.Risky(t.name+": write the file?", text,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "write", Title: "Write", Style: confirm.StylePrimary}).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindACPWrite
}

// answerACPCard answers the card on screen ("" is no: Cancel or Esc).
func (m *Model) answerACPCard(id string) tea.Cmd {
	s := m.aiS()
	t := s.acp
	if t == nil || t.cur == nil {
		return nil
	}
	a := t.cur
	t.cur = nil
	switch {
	case a.perm != nil && strings.HasPrefix(id, "opt:"):
		a.reply <- strings.TrimPrefix(id, "opt:")
	case a.write != nil && id == "write":
		a.reply <- "write"
	default:
		a.reply <- ""
	}
	return nil
}

func acpRel(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

// endACPThread stops the agent; the chat stays in Chat History.
func (m *Model) endACPThread(text string) tea.Cmd {
	s := m.aiS()
	if t := s.acp; t != nil {
		s.acp = nil
		if t.cur != nil {
			t.cur.reply <- ""
			t.cur = nil
			if m.confirmKind == confirmKindACPPermission || m.confirmKind == confirmKindACPWrite {
				m.confirmOpen = false
			}
		}
		if s.chatCancel != nil {
			s.chatCancel()
		}
		if t.stop != nil {
			t.stop()
		}
	}
	return m.aiInfo(text)
}

// acpChatSlot is the chat turn's slot while a thread runs.
func (s *aiState) acpChatSlot() (aiSlot, bool) {
	if s.acp == nil || s.acp.prov == nil {
		return aiSlot{}, false
	}
	return aiSlot{p: s.acp.prov, cfg: ai.Config{Model: s.cfg.Model}}, true
}

// editACPAgents opens the agents file (a template the first time).
func (m *Model) editACPAgents() tea.Cmd {
	path, err := acpAgentsPath()
	if err != nil {
		return m.aiToastErr("ACP agents", err)
	}
	if err := acp.EnsureAgentsFile(path); err != nil {
		return m.aiToastErr("ACP agents", err)
	}
	m.openFileAtLine(path, 1)
	return m.aiInfo("List your ACP agents (name, command, args), save, then run Agent: Chat with an ACP Agent…")
}
