package agentlog

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Claude Code: ~/.claude/projects/<slug of the folder>/<session id>.jsonl,
// one JSON object per line. "user" and "assistant" lines carry an API
// message (content: a string or blocks of text / tool_use / tool_result);
// "summary" and "system" lines are known and not drawn.

type claudeLine struct {
	Type        string          `json:"type"`
	SessionID   string          `json:"sessionId"`
	Cwd         string          `json:"cwd"`
	GitBranch   string          `json:"gitBranch"`
	Timestamp   string          `json:"timestamp"`
	IsSidechain bool            `json:"isSidechain"`
	IsMeta      bool            `json:"isMeta"`
	Message     json.RawMessage `json:"message"`
}

type claudeMessage struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *struct {
		Input       int `json:"input_tokens"`
		CacheCreate int `json:"cache_creation_input_tokens"`
		CacheRead   int `json:"cache_read_input_tokens"`
		Output      int `json:"output_tokens"`
	} `json:"usage"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

func (p *Parser) claudeLine(raw []byte) bool {
	var l claudeLine
	if json.Unmarshal(raw, &l) != nil {
		return false
	}
	at := parseTime(l.Timestamp)
	switch l.Type {
	case "summary", "system", "file-history-snapshot":
		p.stamp(at)
		return true
	case "user", "assistant":
	default:
		p.stamp(at) // a kind this version does not know: dated, skipped
		return false
	}
	var msg claudeMessage
	if len(l.Message) == 0 || json.Unmarshal(l.Message, &msg) != nil {
		return false
	}
	p.stamp(at)
	if p.s.ID == "" {
		p.s.ID = l.SessionID
	}
	if l.Cwd != "" {
		p.s.Cwd = l.Cwd
	}
	if l.GitBranch != "" {
		p.s.Branch = l.GitBranch
	}
	if l.IsSidechain || l.IsMeta {
		return true // a side task or termocode-irrelevant meta line
	}
	if msg.Model != "" {
		p.s.Model = msg.Model
	}
	if u := msg.Usage; u != nil && l.Type == "assistant" && (msg.ID == "" || !p.seen[msg.ID]) {
		if msg.ID != "" {
			p.seen[msg.ID] = true
		}
		in := u.Input + u.CacheCreate + u.CacheRead
		p.s.Usage.In += in
		p.s.Usage.Out += u.Output
		p.s.Usage.Context = in
	}
	// content: a plain string (a typed prompt) or blocks.
	var text string
	if json.Unmarshal(msg.Content, &text) == nil {
		if l.Type == "user" && claudeIsCommandNoise(text) {
			return true
		}
		k := User
		if l.Type == "assistant" {
			k = Assistant
		}
		p.addText(k, at, text)
		return true
	}
	var blocks []claudeBlock
	if json.Unmarshal(msg.Content, &blocks) != nil {
		return false
	}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			k := User
			if l.Type == "assistant" {
				k = Assistant
			}
			if k == User && claudeIsCommandNoise(b.Text) {
				continue
			}
			p.addText(k, at, b.Text)
		case "tool_use":
			p.calls[b.ID] = b.Name
			p.add(Turn{Kind: ToolCall, Time: at, Tool: b.Name, Args: redact(claudeToolArgs(b.Input))})
		case "tool_result":
			p.addResult(at, b.ToolUseID, claudeResultText(b.Content), b.IsError)
		}
	}
	return true
}

// claudeIsCommandNoise is a user line the CLI wrote for its own slash
// commands (not a prompt).
func claudeIsCommandNoise(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "<local-command-") || strings.HasPrefix(s, "<command-name>") ||
		strings.HasPrefix(s, "<command-message>") || strings.HasPrefix(s, "Caveat: The messages below")
}

// claudeResultText is a tool_result's content: a string or text blocks.
func claudeResultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []claudeBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		} else if b.Type == "image" {
			parts = append(parts, "[image]")
		}
	}
	return strings.Join(parts, "\n")
}

// claudeToolArgs is the one-line summary of a tool call's input.
func claudeToolArgs(raw json.RawMessage) string {
	var in map[string]any
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	str := func(k string) string { s, _ := in[k].(string); return s }
	switch {
	case str("file_path") != "" && (str("old_string") != "" || str("new_string") != ""):
		return oneLine(str("file_path"), 120) + " " + diffCount(lineCount(str("new_string")), lineCount(str("old_string")))
	case str("file_path") != "" && str("content") != "":
		return oneLine(str("file_path"), 120) + " " + diffCount(lineCount(str("content")), 0)
	case str("file_path") != "":
		return oneLine(str("file_path"), 120)
	case str("notebook_path") != "":
		return oneLine(str("notebook_path"), 120)
	case str("command") != "":
		return oneLine(str("command"), 120)
	case str("pattern") != "":
		return oneLine(str("pattern"), 120)
	case str("url") != "":
		return oneLine(str("url"), 120)
	case str("query") != "":
		return oneLine(str("query"), 120)
	case str("description") != "":
		return oneLine(str("description"), 120)
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys { // the first string there is, by name
		if s, ok := in[k].(string); ok && s != "" {
			return oneLine(s, 120)
		}
	}
	return ""
}

func diffCount(add, del int) string {
	return "+" + strconv.Itoa(add) + " -" + strconv.Itoa(del)
}
