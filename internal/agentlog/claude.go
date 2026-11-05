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
	Type        string         `json:"type"`
	SessionID   string         `json:"sessionId"`
	Cwd         string         `json:"cwd"`
	GitBranch   string         `json:"gitBranch"`
	Timestamp   string         `json:"timestamp"`
	IsSidechain bool           `json:"isSidechain"`
	IsMeta      bool           `json:"isMeta"`
	Message     *claudeMessage `json:"message"`
}

// claudeMessage is decoded in the same pass as its line (one JSON scan
// per line: the lines can be large tool results). Content is a string
// or a list of blocks (maps).
type claudeMessage struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Model   string `json:"model"`
	Content any    `json:"content"`
	Usage   *struct {
		Input       int `json:"input_tokens"`
		CacheCreate int `json:"cache_creation_input_tokens"`
		CacheRead   int `json:"cache_read_input_tokens"`
		Output      int `json:"output_tokens"`
	} `json:"usage"`
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
	msg := l.Message
	if msg == nil {
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
	k := User
	if l.Type == "assistant" {
		k = Assistant
	}
	switch c := msg.Content.(type) {
	case string: // a typed prompt
		if k == User && claudeIsCommandNoise(c) {
			return true
		}
		p.addText(k, at, c)
		return true
	case []any:
		for _, el := range c {
			b, _ := el.(map[string]any)
			switch str(b, "type") {
			case "text":
				if t := str(b, "text"); k != User || !claudeIsCommandNoise(t) {
					p.addText(k, at, t)
				}
			case "tool_use":
				name := str(b, "name")
				p.calls[str(b, "id")] = name
				in, _ := b["input"].(map[string]any)
				p.add(Turn{Kind: ToolCall, Time: at, Tool: name, Args: redact(claudeToolArgs(in))})
			case "tool_result":
				failed, _ := b["is_error"].(bool)
				p.addResult(at, str(b, "tool_use_id"), claudeResultText(b["content"]), failed)
			}
		}
		return true
	}
	return false
}

// str is m[k] as a string ("" when missing or not a string).
func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// claudeIsCommandNoise is a user line the CLI wrote for its own slash
// commands (not a prompt).
func claudeIsCommandNoise(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "<local-command-") || strings.HasPrefix(s, "<command-name>") ||
		strings.HasPrefix(s, "<command-message>") || strings.HasPrefix(s, "Caveat: The messages below")
}

// claudeResultText is a tool_result's content: a string or text blocks.
func claudeResultText(c any) string {
	switch c := c.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, el := range c {
			b, _ := el.(map[string]any)
			switch str(b, "type") {
			case "text":
				parts = append(parts, str(b, "text"))
			case "image":
				parts = append(parts, "[image]")
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// claudeToolArgs is the one-line summary of a tool call's input.
func claudeToolArgs(in map[string]any) string {
	get := func(k string) string { return str(in, k) }
	switch {
	case get("file_path") != "" && (get("old_string") != "" || get("new_string") != ""):
		return oneLine(get("file_path"), 120) + " " + diffCount(lineCount(get("new_string")), lineCount(get("old_string")))
	case get("file_path") != "" && get("content") != "":
		return oneLine(get("file_path"), 120) + " " + diffCount(lineCount(get("content")), 0)
	case get("file_path") != "":
		return oneLine(get("file_path"), 120)
	case get("notebook_path") != "":
		return oneLine(get("notebook_path"), 120)
	case get("command") != "":
		return oneLine(get("command"), 120)
	case get("pattern") != "":
		return oneLine(get("pattern"), 120)
	case get("url") != "":
		return oneLine(get("url"), 120)
	case get("query") != "":
		return oneLine(get("query"), 120)
	case get("description") != "":
		return oneLine(get("description"), 120)
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
