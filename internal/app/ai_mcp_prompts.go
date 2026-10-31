package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/toast"
)

// MCP prompts in the chat (MCPX-13): "/server.prompt arg …" sends a
// server's ready-made prompt. Values fill the prompt's arguments in
// order, or by name (key=value). Tab completes the command and the
// argument values (completion/complete). The prompt's text becomes the
// user message, so the user sees what was sent.

// aiMCPXState is the chat's MCP prompt state.
type aiMCPXState struct {
	prompts map[string][]mcp.Prompt // server → prompts (Tab, the picker)
	loading bool
	elicit  *aiMCPElicitPending // the open server question (ai_mcp_elicit.go)
}

// aiMCPPromptTimeout caps one prompt request.
const aiMCPPromptTimeout = 30 * time.Second

type aiMCPPromptCmd struct {
	server, prompt string
	args           []string
}

// aiMCPPromptParse reads "/server.prompt a b key=c". ok=false when text
// is not of that form.
func aiMCPPromptParse(text string) (aiMCPPromptCmd, bool) {
	if !strings.HasPrefix(text, "/") {
		return aiMCPPromptCmd{}, false
	}
	head, rest, _ := strings.Cut(strings.TrimSpace(text[1:]), " ")
	server, prompt, ok := strings.Cut(head, ".")
	if !ok || server == "" || prompt == "" || strings.ContainsAny(head, "/\n") {
		return aiMCPPromptCmd{}, false
	}
	args, err := aiShellSplit(rest)
	if err != nil {
		args = strings.Fields(rest)
	}
	return aiMCPPromptCmd{server: server, prompt: prompt, args: args}, true
}

// aiMCPBindArgs fills p's arguments: key=value by name (when key is an
// argument), other values in order. A missing required one is an error.
func aiMCPBindArgs(p mcp.Prompt, args []string) (map[string]string, error) {
	out := map[string]string{}
	isArg := map[string]bool{}
	for _, a := range p.Arguments {
		isArg[a.Name] = true
	}
	var pos []string
	for _, a := range args {
		if k, v, ok := strings.Cut(a, "="); ok && isArg[k] {
			out[k] = v
			continue
		}
		pos = append(pos, a)
	}
	for _, a := range p.Arguments {
		if _, set := out[a.Name]; set || len(pos) == 0 {
			continue
		}
		out[a.Name], pos = pos[0], pos[1:]
	}
	if len(pos) > 0 {
		return nil, fmt.Errorf("too many values: %s", strings.Join(pos, " "))
	}
	var miss []string
	for _, a := range p.Arguments {
		if a.Required && out[a.Name] == "" {
			miss = append(miss, a.Name)
		}
	}
	if len(miss) > 0 {
		return nil, fmt.Errorf("needs %s (%s)", strings.Join(miss, ", "), aiMCPPromptUsage(p))
	}
	return out, nil
}

// aiMCPPromptUsage is "file [style]".
func aiMCPPromptUsage(p mcp.Prompt) string {
	var parts []string
	for _, a := range p.Arguments {
		if a.Required {
			parts = append(parts, a.Name)
		} else {
			parts = append(parts, "["+a.Name+"]")
		}
	}
	return strings.Join(parts, " ")
}

// aiMCPPromptText turns a prompt's messages into one user message.
func aiMCPPromptText(server, name string, r mcp.PromptResult) string {
	var parts []string
	for _, msg := range r.Messages {
		body := mcp.CallResult{Content: []mcp.Content{msg.Content}}.Text()
		if strings.TrimSpace(body) == "" {
			continue
		}
		if msg.Role == "assistant" {
			body = "(Assistant, from the prompt:) " + body
		}
		parts = append(parts, body)
	}
	text := ai.RedactSecrets(aiCleanText(strings.Join(parts, "\n\n")))
	return "[MCP prompt " + server + "." + name + "]\n" + text
}

// aiMCPPromptMsg is a prompt got for the chat.
type aiMCPPromptMsg struct {
	typed string // the command as typed (kept as the draft on error)
	text  string
	err   error
}

// aiMCPFindPrompt lists a server's prompts and finds name.
func aiMCPFindPrompt(ctx context.Context, mgr *mcp.Manager, server, name string) (mcp.Prompt, error) {
	ps, err := mgr.Prompts(ctx, nil, server)
	if err != nil {
		return mcp.Prompt{}, err
	}
	for _, p := range ps {
		if p.Name == name {
			return p, nil
		}
	}
	return mcp.Prompt{}, fmt.Errorf("MCP %s has no prompt %q", server, name)
}

// aiMCPPromptSubmit runs "/server.prompt …" when server is configured;
// ok=false lets the text go to the model as it is.
func (m *Model) aiMCPPromptSubmit(text string) (tea.Cmd, bool) {
	c, ok := aiMCPPromptParse(text)
	if !ok || !mcp.LoadSettings(mcpConfigPath()).Enabled {
		return nil, false
	}
	mgr := m.aiMCP()
	if _, known := mgr.Caps(c.server); !known {
		return nil, false
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), aiMCPPromptTimeout)
		defer cancel()
		p, err := aiMCPFindPrompt(ctx, mgr, c.server, c.prompt)
		if err != nil {
			return aiMCPPromptMsg{typed: text, err: err}
		}
		args, err := aiMCPBindArgs(p, c.args)
		if err != nil {
			return aiMCPPromptMsg{typed: text, err: fmt.Errorf("/%s.%s %w", c.server, c.prompt, err)}
		}
		r, err := mgr.GetPrompt(ctx, nil, c.server, c.prompt, args)
		if err != nil {
			return aiMCPPromptMsg{typed: text, err: err}
		}
		return aiMCPPromptMsg{typed: text, text: aiMCPPromptText(c.server, c.prompt, r)}
	}, true
}

// onAIMCPPrompt sends the got prompt, or keeps the draft and says why.
func (m *Model) onAIMCPPrompt(msg aiMCPPromptMsg) tea.Cmd {
	if msg.err != nil {
		return m.aiMCPWarn("MCP prompt not sent", ai.Redact(msg.err.Error()))
	}
	s := m.aiS()
	if string(s.input) == msg.typed || strings.TrimSpace(string(s.input)) == strings.TrimSpace(msg.typed) {
		s.input, s.caret = nil, 0
	}
	return m.sendAIChat(msg.text)
}

// ── Tab completion ─────────────────────────────────────────────────────

// aiMCPPromptsMsg is the loaded prompt lists (then Tab runs again).
type aiMCPPromptsMsg struct {
	prompts map[string][]mcp.Prompt
	retab   bool
}

// aiMCPLoadPrompts lists the prompts of every usable server (it starts
// them; untrusted project servers are skipped).
func (m *Model) aiMCPLoadPrompts(retab bool) tea.Cmd {
	mgr := m.aiMCP()
	m.aiS().mcpx.loading = true
	return func() tea.Msg {
		mgr.Reload()
		ctx, cancel := context.WithTimeout(context.Background(), aiMCPPromptTimeout)
		defer cancel()
		out := map[string][]mcp.Prompt{}
		for _, st := range mgr.Servers() {
			if st.State == mcp.StateInvalid || st.State == mcp.StateDisabled || st.State == mcp.StateSignIn {
				continue
			}
			if ps, err := mgr.Prompts(ctx, nil, st.Name); err == nil && len(ps) > 0 {
				out[st.Name] = ps
			}
		}
		return aiMCPPromptsMsg{prompts: out, retab: retab}
	}
}

func (m *Model) onAIMCPPrompts(msg aiMCPPromptsMsg) tea.Cmd {
	s := m.aiS()
	s.mcpx.prompts, s.mcpx.loading = msg.prompts, false
	if len(msg.prompts) == 0 {
		return m.aiInfo("No MCP prompts: no connected server has any")
	}
	if msg.retab {
		return m.aiMCPPromptTab()
	}
	return nil
}

// aiMCPPromptNames are "/server.prompt" for every loaded prompt, sorted.
func (s *aiState) aiMCPPromptNames() []string {
	var out []string
	for server, ps := range s.mcpx.prompts {
		for _, p := range ps {
			out = append(out, "/"+server+"."+p.Name)
		}
	}
	sort.Strings(out)
	return out
}

func commonPrefix(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	p := xs[0]
	for _, x := range xs[1:] {
		for !strings.HasPrefix(x, p) {
			p = p[:len(p)-1]
		}
	}
	return p
}

// aiMCPPromptTab completes the "/" command or the argument at the end
// of the input (the prompt lists load first).
func (m *Model) aiMCPPromptTab() tea.Cmd {
	s := m.aiS()
	in := string(s.input)
	if s.mcpx.prompts == nil {
		if s.mcpx.loading {
			return nil
		}
		return tea.Batch(m.aiInfo("Loading MCP prompts…"), m.aiMCPLoadPrompts(true))
	}
	if !strings.Contains(in, " ") { // the command name
		var hits []string
		for _, n := range s.aiMCPPromptNames() {
			if strings.HasPrefix(n, in) {
				hits = append(hits, n)
			}
		}
		switch len(hits) {
		case 0:
			return m.aiInfo("No MCP prompt starts with " + in)
		case 1:
			m.aiSetInput(hits[0] + " ")
			if p, ok := s.aiMCPPrompt(hits[0]); ok && len(p.Arguments) > 0 {
				return m.aiInfo(hits[0] + " " + aiMCPPromptUsage(p))
			}
			return nil
		}
		m.aiSetInput(commonPrefix(hits))
		return m.aiInfo(strings.Join(hits, "  "))
	}
	c, _ := aiMCPPromptParse(in)
	p, ok := s.aiMCPPrompt("/" + c.server + "." + c.prompt)
	if !ok || len(p.Arguments) == 0 {
		return nil
	}
	// The word being typed: "key=part" or the next value in order.
	last := ""
	if !strings.HasSuffix(in, " ") {
		last = in[strings.LastIndex(in, " ")+1:]
	}
	done := c.args
	if last != "" && len(done) > 0 {
		done = done[:len(done)-1]
	}
	bound := map[string]string{}
	for k, v := range aiMCPBindPartial(p, done) {
		bound[k] = v
	}
	arg, part, prefix := "", last, ""
	if k, v, ok := strings.Cut(last, "="); ok {
		arg, part, prefix = k, v, k+"="
	} else {
		for _, a := range p.Arguments {
			if _, set := bound[a.Name]; !set {
				arg = a.Name
				break
			}
		}
	}
	if arg == "" {
		return nil
	}
	mgr := m.aiMCP()
	typed := in
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), aiMCPPromptTimeout)
		defer cancel()
		vals, err := mgr.CompletePrompt(ctx, nil, c.server, c.prompt, arg, part, bound)
		return aiMCPCompleteMsg{typed: typed, word: last, prefix: prefix, arg: arg, values: vals, err: err}
	}
}

// aiMCPBindPartial is aiMCPBindArgs without the required check.
func aiMCPBindPartial(p mcp.Prompt, args []string) map[string]string {
	q := p
	q.Arguments = append([]mcp.PromptArgument(nil), p.Arguments...)
	for i := range q.Arguments {
		q.Arguments[i].Required = false
	}
	out, err := aiMCPBindArgs(q, args)
	if err != nil {
		return map[string]string{}
	}
	return out
}

func (s *aiState) aiMCPPrompt(cmd string) (mcp.Prompt, bool) {
	server, name, _ := strings.Cut(strings.TrimPrefix(cmd, "/"), ".")
	for _, p := range s.mcpx.prompts[server] {
		if p.Name == name {
			return p, true
		}
	}
	return mcp.Prompt{}, false
}

// aiMCPCompleteMsg is a completion answer for the input typed.
type aiMCPCompleteMsg struct {
	typed, word, prefix, arg string
	values                   []string
	err                      error
}

func (m *Model) onAIMCPComplete(msg aiMCPCompleteMsg) tea.Cmd {
	s := m.aiS()
	if string(s.input) != msg.typed { // the user typed on
		return nil
	}
	if msg.err != nil {
		return m.aiMCPWarn("No completion", ai.Redact(msg.err.Error()))
	}
	base := strings.TrimSuffix(msg.typed, msg.word)
	quote := func(v string) string {
		if strings.ContainsAny(v, " \t\"'") {
			return fmt.Sprintf("%q", v)
		}
		return v
	}
	switch len(msg.values) {
	case 0:
		return m.aiInfo("No values for " + msg.arg)
	case 1:
		m.aiSetInput(base + msg.prefix + quote(msg.values[0]) + " ")
		return nil
	}
	if cp := commonPrefix(msg.values); len(cp) > len(strings.TrimPrefix(msg.word, msg.prefix)) {
		m.aiSetInput(base + msg.prefix + cp)
	}
	shown := msg.values
	if len(shown) > 8 {
		shown = append(shown[:8:8], fmt.Sprintf("… %d more", len(msg.values)-8))
	}
	return m.aiInfo(msg.arg + ": " + strings.Join(shown, "  "))
}

// aiSetInput replaces the chat input and puts the caret at its end.
func (m *Model) aiSetInput(text string) {
	s := m.aiS()
	s.input = []rune(text)
	s.caret = len(s.input)
}

// aiWarn is a warning toast with a detail line.
func (m *Model) aiMCPWarn(title, detail string) tea.Cmd {
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Warn, title, detail)
	return c
}
