package agentlog

import (
	"encoding/json"
	"strings"
	"time"
)

// Codex: ~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<id>.jsonl. The
// first line is session_meta (id, cwd, git); then response_item lines
// (messages, function_call / custom_tool_call and their outputs,
// reasoning), event_msg lines (copies of the messages, token_count) and
// turn_context lines (the model). The oldest files have the items at the
// top level, with no wrapper.

type codexLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexItem struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Input     string          `json:"input"`
	CallID    string          `json:"call_id"`
	Output    json.RawMessage `json:"output"`
	// session_meta / turn_context
	ID    string `json:"id"`
	Cwd   string `json:"cwd"`
	Model string `json:"model"`
	Git   *struct {
		Branch string `json:"branch"`
	} `json:"git"`
	// event_msg token_count
	Info *struct {
		Total *struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"total_token_usage"`
		Last *struct {
			Input int `json:"input_tokens"`
		} `json:"last_token_usage"`
		Window int `json:"model_context_window"`
	} `json:"info"`
	RateLimits *struct {
		Primary *struct {
			UsedPercent float64 `json:"used_percent"`
			ResetsIn    int64   `json:"resets_in_seconds"`
			ResetsAt    int64   `json:"resets_at"`
		} `json:"primary"`
	} `json:"rate_limits"`
}

func (p *Parser) codexLine(raw []byte) bool {
	var l codexLine
	if json.Unmarshal(raw, &l) != nil {
		return false
	}
	at := parseTime(l.Timestamp)
	if len(l.Payload) == 0 { // the oldest files: an item per line
		var it codexItem
		if json.Unmarshal(raw, &it) != nil {
			return false
		}
		if l.Type == "" && it.ID != "" && l.Timestamp != "" { // their first line
			p.stamp(at)
			p.s.ID = it.ID
			return true
		}
		return p.codexItem(at, it)
	}
	var it codexItem
	if json.Unmarshal(l.Payload, &it) != nil {
		return false
	}
	p.stamp(at)
	switch l.Type {
	case "session_meta":
		p.s.ID, p.s.Cwd = it.ID, it.Cwd
		if it.Git != nil {
			p.s.Branch = it.Git.Branch
		}
		return true
	case "turn_context":
		if it.Model != "" {
			p.s.Model = it.Model
		}
		if it.Cwd != "" && p.s.Cwd == "" {
			p.s.Cwd = it.Cwd
		}
		return true
	case "event_msg":
		if it.Type == "token_count" {
			p.codexTokens(at, it)
		}
		return true // the rest are copies of the response items
	case "response_item":
		return p.codexItem(at, it)
	case "compacted":
		return true
	}
	return false
}

func (p *Parser) codexItem(at time.Time, it codexItem) bool {
	switch it.Type {
	case "message":
		text := codexText(it.Content)
		switch it.Role {
		case "user":
			if codexIsInjected(text) {
				return true
			}
			p.addText(User, at, text)
		case "assistant":
			p.addText(Assistant, at, text)
		}
		return true // developer / system: known, not drawn
	case "function_call", "local_shell_call":
		p.calls[it.CallID] = it.Name
		p.add(Turn{Kind: ToolCall, Time: at, Tool: it.Name, Args: redact(codexCallArgs(it.Name, it.Arguments))})
		return true
	case "custom_tool_call":
		p.calls[it.CallID] = it.Name
		args := oneLine(it.Input, 120)
		if it.Name == "apply_patch" {
			args = patchSummary(it.Input)
		}
		p.add(Turn{Kind: ToolCall, Time: at, Tool: it.Name, Args: redact(args)})
		return true
	case "function_call_output", "custom_tool_call_output":
		text, failed := codexOutput(it.Output)
		p.addResult(at, it.CallID, text, failed)
		return true
	case "reasoning", "web_search_call":
		return true
	}
	return false
}

func (p *Parser) codexTokens(at time.Time, it codexItem) {
	if in := it.Info; in != nil {
		if in.Total != nil {
			p.s.Usage.In, p.s.Usage.Out = in.Total.Input, in.Total.Output
		}
		if in.Last != nil {
			p.s.Usage.Context = in.Last.Input
		}
		if in.Window > 0 {
			p.s.Usage.Window = in.Window
		}
	}
	if rl := it.RateLimits; rl != nil && rl.Primary != nil {
		p.s.Usage.LimitPct = rl.Primary.UsedPercent
		switch {
		case rl.Primary.ResetsAt > 0:
			p.s.Usage.LimitReset = time.Unix(rl.Primary.ResetsAt, 0).UTC()
		case rl.Primary.ResetsIn > 0 && !at.IsZero():
			p.s.Usage.LimitReset = at.Add(time.Duration(rl.Primary.ResetsIn) * time.Second)
		}
	}
}

// codexText joins a message's input_text / output_text parts.
func codexText(raw json.RawMessage) string {
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	var out []string
	for _, c := range parts {
		if c.Text != "" {
			out = append(out, c.Text)
		}
	}
	return strings.Join(out, "\n")
}

// codexIsInjected is a user message the CLI adds by itself (the folder,
// the AGENTS.md text), not one the user typed.
func codexIsInjected(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "<environment_context>") || strings.HasPrefix(s, "<user_instructions>") ||
		strings.HasPrefix(s, "# AGENTS.md instructions")
}

// codexCallArgs is the one-line summary of a function call: a shell
// command without its "bash -lc" wrapper, else the first string.
func codexCallArgs(name, args string) string {
	var in map[string]any
	if json.Unmarshal([]byte(args), &in) != nil {
		return oneLine(args, 120)
	}
	if cmd, ok := in["command"].([]any); ok {
		var parts []string
		for _, c := range cmd {
			if s, ok := c.(string); ok {
				parts = append(parts, s)
			}
		}
		if len(parts) >= 3 && (parts[0] == "bash" || parts[0] == "sh" || parts[0] == "zsh") && (parts[1] == "-lc" || parts[1] == "-c") {
			parts = parts[2:]
		}
		return oneLine(strings.Join(parts, " "), 120)
	}
	if s, ok := in["command"].(string); ok {
		return oneLine(s, 120)
	}
	if s, ok := in["input"].(string); ok && name == "apply_patch" {
		return patchSummary(s)
	}
	for _, k := range []string{"path", "file", "query", "url"} {
		if s, ok := in[k].(string); ok && s != "" {
			return oneLine(s, 120)
		}
	}
	return ""
}

// patchSummary is "<first file> +N -M" of an apply_patch input.
func patchSummary(patch string) string {
	file, add, del := "", 0, 0
	for _, ln := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(ln, "*** Update File: "), strings.HasPrefix(ln, "*** Add File: "), strings.HasPrefix(ln, "*** Delete File: "):
			if file == "" {
				file = strings.TrimSpace(ln[strings.Index(ln, ":")+1:])
			}
		case strings.HasPrefix(ln, "***"):
		case strings.HasPrefix(ln, "+"):
			add++
		case strings.HasPrefix(ln, "-"):
			del++
		}
	}
	return strings.TrimSpace(oneLine(file, 100) + " " + diffCount(add, del))
}

// codexOutput is a call output's text and whether it failed: a JSON
// string that holds {"output", "metadata": {"exit_code"}}, or plain text.
func codexOutput(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		var obj struct {
			Content string `json:"content"`
			Success *bool  `json:"success"`
		}
		if json.Unmarshal(raw, &obj) == nil {
			return obj.Content, obj.Success != nil && !*obj.Success
		}
		return "", false
	}
	var wrapped struct {
		Output   *string `json:"output"`
		Metadata struct {
			ExitCode *int `json:"exit_code"`
		} `json:"metadata"`
	}
	if strings.HasPrefix(strings.TrimSpace(s), "{") && json.Unmarshal([]byte(s), &wrapped) == nil && wrapped.Output != nil {
		ec := wrapped.Metadata.ExitCode
		return *wrapped.Output, ec != nil && *ec != 0
	}
	return s, false
}
