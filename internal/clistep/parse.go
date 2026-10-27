package clistep

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// parser turns one CLI's JSON lines into events and keeps the result.
// Lines that are not JSON or of an unknown type are skipped: a newer CLI
// may add events.
type parser struct {
	s    Spec
	emit func(ai.Event)

	final   string    // the final answer
	usage   *ai.Usage // the token counts
	files   []string
	failed  string    // the CLI's own error text
	limit   string    // a usage-limit event's text
	resetAt time.Time // when the limit resets (from the event)
	done    bool      // the end event came
	model   string

	texts int               // text blocks streamed so far
	tools map[string]string // tool id → tool name (Claude Code)
	seen  map[string]bool   // files already counted
}

func newParser(s Spec, emit func(ai.Event)) *parser {
	return &parser{s: s, emit: emit, tools: map[string]string{}, seen: map[string]bool{}}
}

func (p *parser) line(b []byte) {
	if len(b) == 0 || b[0] != '{' {
		return
	}
	switch p.s.CLI {
	case ai.CLIClaude:
		p.claude(b)
	case ai.CLICodex:
		p.codex(b)
	}
}

func (p *parser) text(t string) {
	if strings.TrimSpace(t) == "" {
		return
	}
	if p.texts > 0 {
		t = "\n\n" + t
	}
	p.texts++
	p.emit(ai.Event{Kind: ai.EventText, Text: t})
}

// rel is path relative to the project root when it is inside it.
func (p *parser) rel(path string) string {
	if path == "" {
		return ""
	}
	if p.s.Dir != "" && filepath.IsAbs(path) {
		if r, err := filepath.Rel(p.s.Dir, path); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
	}
	return path
}

func (p *parser) file(path string) {
	if path = p.rel(path); path != "" && !p.seen[path] {
		p.seen[path] = true
		p.files = append(p.files, path)
	}
}

// short is s on one line, at most n runes.
func short(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// lineCount is "3 lines" for a tool result.
func lineCount(s string) string {
	n := strings.Count(strings.TrimRight(s, "\n"), "\n") + 1
	if strings.TrimSpace(s) == "" {
		n = 0
	}
	if n == 1 {
		return "1 line"
	}
	return strconv.Itoa(n) + " lines"
}

// ── Claude Code: claude -p --output-format stream-json --verbose ──────────

type claudeEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Model   string `json:"model"`
	Message struct {
		Model   string            `json:"model"`
		Content []json.RawMessage `json:"content"`
	} `json:"message"`
	IsError       bool        `json:"is_error"`
	Result        string      `json:"result"`
	NumTurns      int         `json:"num_turns"`
	Usage         claudeUsage `json:"usage"`
	RateLimitInfo *struct {
		Status   string `json:"status"`
		ResetsAt int64  `json:"resetsAt"`
	} `json:"rate_limit_info"`
}

type claudeUsage struct {
	Input      int `json:"input_tokens"`
	CacheWrite int `json:"cache_creation_input_tokens"`
	CacheRead  int `json:"cache_read_input_tokens"`
	Output     int `json:"output_tokens"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     map[string]any  `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// editTools change the file in input.file_path / notebook_path.
var editTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

func (p *parser) claude(b []byte) {
	var e claudeEvent
	if json.Unmarshal(b, &e) != nil {
		return
	}
	switch e.Type {
	case "system":
		if e.Subtype == "init" && e.Model != "" {
			p.model = e.Model
		}
	case "rate_limit_event":
		if ri := e.RateLimitInfo; ri != nil && (ri.Status == "rejected" || ri.Status == "exceeded") {
			p.limit = "usage limit reached"
			if ri.ResetsAt > 0 {
				p.resetAt = time.Unix(ri.ResetsAt, 0)
			}
		}
	case "assistant":
		for _, raw := range e.Message.Content {
			var bl claudeBlock
			if json.Unmarshal(raw, &bl) != nil {
				continue
			}
			switch bl.Type {
			case "text":
				p.text(bl.Text)
			case "tool_use":
				p.tools[bl.ID] = bl.Name
				if editTools[bl.Name] {
					p.file(str(bl.Input, "file_path", "notebook_path"))
				}
				p.emit(ai.Event{Kind: ai.EventToolStart, Tool: bl.Name, Label: bl.Name, Detail: p.claudeDetail(bl), Args: bl.Input})
			}
		}
	case "user":
		for _, raw := range e.Message.Content {
			var bl claudeBlock
			if json.Unmarshal(raw, &bl) != nil || bl.Type != "tool_result" {
				continue
			}
			name := p.tools[bl.ToolUseID]
			out := blockText(bl.Content)
			detail := lineCount(out)
			switch {
			case bl.IsError:
				detail = "error: " + short(out, 80)
			case editTools[name]:
				detail = "done"
			}
			p.emit(ai.Event{Kind: ai.EventToolResult, Tool: name, Label: name, Detail: detail})
		}
	case "result":
		p.done = true
		if e.IsError {
			p.failed = e.Result
			if p.failed == "" {
				p.failed = e.Subtype
			}
			if isLimit(p.failed) && p.limit == "" {
				p.limit = p.failed
			}
			if p.limit != "" && isLimit(e.Result) {
				p.limit = e.Result // the CLI's own words
			}
		} else {
			p.final = e.Result
		}
		u := e.Usage
		if u.Input+u.CacheWrite+u.CacheRead+u.Output > 0 {
			p.usage = &ai.Usage{Model: p.model, Input: u.Input, CacheWrite: u.CacheWrite, CacheRead: u.CacheRead,
				Output: u.Output, Requests: max(e.NumTurns, 1), PeakContext: u.Input + u.CacheWrite + u.CacheRead}
		}
	}
}

// claudeDetail is the human line of a tool call: "Read internal/a.go".
func (p *parser) claudeDetail(bl claudeBlock) string {
	in := bl.Input
	switch {
	case str(in, "file_path", "notebook_path") != "":
		return bl.Name + " " + p.rel(str(in, "file_path", "notebook_path"))
	case str(in, "command") != "":
		return bl.Name + ": " + short(str(in, "command"), 80)
	case str(in, "pattern") != "":
		return bl.Name + " " + short(str(in, "pattern"), 60)
	case str(in, "url") != "":
		return bl.Name + " " + short(str(in, "url"), 80)
	case str(in, "description") != "":
		return bl.Name + ": " + short(str(in, "description"), 80)
	}
	return bl.Name
}

func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// blockText is a tool result's content: a string or a list of text
// blocks.
func blockText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, x := range parts {
			b.WriteString(x.Text)
		}
		return b.String()
	}
	return ""
}

// ── Codex: codex exec --json ──────────────────────────────────────────────

type codexEvent struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
	Item *struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Text     string `json:"text"`
		Command  string `json:"command"`
		Output   string `json:"aggregated_output"`
		ExitCode *int   `json:"exit_code"`
		Status   string `json:"status"`
		Changes  []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"changes"`
		Server  string `json:"server"`
		Tool    string `json:"tool"`
		Query   string `json:"query"`
		Message string `json:"message"`
	} `json:"item"`
	Usage *struct {
		Input  int `json:"input_tokens"`
		Cached int `json:"cached_input_tokens"`
		Output int `json:"output_tokens"`
	} `json:"usage"`
}

func (p *parser) codex(b []byte) {
	var e codexEvent
	if json.Unmarshal(b, &e) != nil {
		return
	}
	switch e.Type {
	case "error":
		p.failed = e.Message
	case "turn.failed":
		if e.Error != nil && e.Error.Message != "" {
			p.failed = e.Error.Message
		}
		p.done = true
	case "turn.completed":
		p.done = true
		if u := e.Usage; u != nil {
			add := &ai.Usage{Input: max(u.Input-u.Cached, 0), CacheRead: u.Cached, Output: u.Output, Requests: 1, PeakContext: u.Input}
			p.usage = p.usage.Add(add)
		}
	case "item.started", "item.completed":
		it := e.Item
		if it == nil {
			return
		}
		end := e.Type == "item.completed"
		switch it.Type {
		case "agent_message":
			if end {
				p.text(it.Text)
				p.final = it.Text
			}
		case "command_execution":
			cmd := short(unwrapShell(it.Command), 80)
			if !end {
				p.emit(ai.Event{Kind: ai.EventToolStart, Tool: "shell", Label: "shell", Detail: cmd})
				p.tools[it.ID] = "shell"
				return
			}
			if p.tools[it.ID] == "" { // completed without a start
				p.emit(ai.Event{Kind: ai.EventToolStart, Tool: "shell", Label: "shell", Detail: cmd})
			}
			detail := lineCount(it.Output)
			if it.ExitCode != nil {
				detail = "exit " + strconv.Itoa(*it.ExitCode) + " · " + detail
			}
			p.emit(ai.Event{Kind: ai.EventToolResult, Tool: "shell", Label: "shell", Detail: detail})
		case "file_change":
			if !end {
				return
			}
			for _, c := range it.Changes {
				p.file(c.Path)
				p.emit(ai.Event{Kind: ai.EventToolStart, Tool: "edit", Label: "edit", Detail: p.rel(c.Path)})
				p.emit(ai.Event{Kind: ai.EventToolResult, Tool: "edit", Label: "edit", Detail: c.Kind})
			}
		case "mcp_tool_call":
			name := it.Server + "." + it.Tool
			if !end {
				p.emit(ai.Event{Kind: ai.EventToolStart, Tool: name, Label: name, Detail: name})
				return
			}
			p.emit(ai.Event{Kind: ai.EventToolResult, Tool: name, Label: name, Detail: it.Status})
		case "web_search":
			if end {
				p.emit(ai.Event{Kind: ai.EventToolStart, Tool: "web_search", Label: "web search", Detail: short(it.Query, 80)})
				p.emit(ai.Event{Kind: ai.EventToolResult, Tool: "web_search", Label: "web search", Detail: "done"})
			}
		case "error":
			if end && it.Message != "" {
				p.failed = it.Message
			}
		}
	}
}

// unwrapShell turns "bash -lc 'git diff'" into "git diff".
func unwrapShell(c string) string {
	for _, pre := range []string{"bash -lc ", "/bin/bash -lc ", "sh -c ", "zsh -lc "} {
		if rest, ok := strings.CutPrefix(c, pre); ok {
			rest = strings.TrimSpace(rest)
			if len(rest) >= 2 && (rest[0] == '\'' || rest[0] == '"') && rest[len(rest)-1] == rest[0] {
				rest = rest[1 : len(rest)-1]
			}
			return rest
		}
	}
	return c
}
