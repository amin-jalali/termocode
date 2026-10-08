package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// EventKind tags what the agent reports while it works.
type EventKind int

const (
	EventText       EventKind = iota // streamed assistant text (Text)
	EventToolStart                   // a tool is about to run (Tool, Detail = human line)
	EventToolResult                  // a tool finished (Tool, Detail = result / "declined")
	EventDone                        // the turn is complete
	EventError                       // the turn failed (Detail)
)

// Event is one agent progress report.
type Event struct {
	Kind   EventKind
	Text   string
	Tool   string
	Detail string
}

// ConfirmFunc asks the user to allow a mutating tool call. It may block
// (the TUI answers through a channel); return false to decline.
type ConfirmFunc func(ctx context.Context, t Tool, human string) bool

// DefaultMaxSteps caps tool round-trips per user turn.
const DefaultMaxSteps = 6

// Agent runs a tool-calling loop on top of a Provider. Port of mobocode's
// ai_agent.dart: with Native it uses the provider's function calling
// (OpenAI tools / Anthropic tool_use); otherwise the model calls one tool
// per step with a fenced ```json {"tool":…,"args":…}``` block. Mutating
// tools must clear Confirm unless their class is in AutoApprove.
type Agent struct {
	Provider    Provider
	Tools       []Tool
	Model       string
	MaxSteps    int
	AutoApprove map[string]bool
	Confirm     ConfirmFunc
	Native      bool
}

// SystemPrompt appends the tool protocol to base. Native tool calling only
// needs a short hint; the text protocol needs the whole catalog.
func SystemPrompt(base string, tools []Tool, native bool) string {
	if len(tools) == 0 {
		return base
	}
	if native {
		return base + "\n\nYou can call IDE tools (files, search, git, LSP, shell). " +
			"Mutating tools ask the user first; if one is declined, do not retry it."
	}
	return base + "\n\nFor project actions (read or create files, search, run a command, git), call a tool: " +
		"reply with ONLY a fenced json block: ```json {\"tool\":\"<name>\",\"args\":{...}} ``` — one tool per reply. " +
		"You will get the result in the next message. Tools:\n" + ToolsPromptSpec(tools)
}

// Run executes one user turn. history must already end with the user
// message (and start with the system prompt). It returns the messages the
// turn added (assistant replies and tool results) so the caller can keep
// the conversation going. emit is called synchronously from Run's
// goroutine.
func (a *Agent) Run(ctx context.Context, history []Message, emit func(Event)) []Message {
	msgs := append([]Message(nil), history...)
	start := len(msgs)
	steps := a.MaxSteps
	if steps <= 0 {
		steps = DefaultMaxSteps
	}
	var specs []ToolSpec
	if a.Native {
		for _, t := range a.Tools {
			specs = append(specs, t.Spec())
		}
	}
	for step := 0; step < steps; step++ {
		req := Request{Messages: msgs, Model: a.Model, Tools: specs}
		var buf strings.Builder
		prose := 0 // 0 undecided, 1 stream as text, -1 looks like tool json
		var last Chunk
		for c := range a.Provider.Stream(ctx, req) {
			if c.Delta != "" {
				buf.WriteString(c.Delta)
				switch {
				case a.Native:
					emit(Event{Kind: EventText, Text: c.Delta})
				case prose == 0:
					lead := strings.TrimLeft(buf.String(), " \t\r\n")
					if lead != "" {
						if strings.HasPrefix(lead, "{") || strings.HasPrefix(lead, "```") {
							prose = -1
						} else {
							prose = 1
							emit(Event{Kind: EventText, Text: lead})
						}
					}
				case prose == 1:
					emit(Event{Kind: EventText, Text: c.Delta})
				}
			}
			if c.Done {
				last = c
			}
		}
		if last.Err != nil {
			emit(Event{Kind: EventError, Detail: Redact(last.Err.Error())})
			return msgs[start:]
		}
		reply := buf.String()
		var calls []ToolCall
		if a.Native {
			calls = last.ToolCalls
		} else if tc, ok := ParseTextToolCall(reply); ok {
			tc.ID = fmt.Sprintf("text-%d", step)
			calls = []ToolCall{tc}
		}
		asst := Message{Role: RoleAssistant, Content: reply}
		if a.Native {
			asst.ToolCalls = calls
			asst.Blocks = last.Blocks
		}
		msgs = append(msgs, asst)
		if len(calls) == 0 {
			if prose == -1 {
				emit(Event{Kind: EventText, Text: reply})
			}
			emit(Event{Kind: EventDone})
			return msgs[start:]
		}
		for _, call := range calls {
			result := a.runTool(ctx, call, emit)
			if a.Native {
				msgs = append(msgs, Message{Role: RoleTool, ToolCallID: call.ID, ToolName: call.Name, Content: result})
			} else {
				msgs = append(msgs, Message{Role: RoleUser, Content: "[tool result] " + call.Name + ":\n" + result})
			}
		}
		if ctx.Err() != nil {
			emit(Event{Kind: EventError, Detail: ctx.Err().Error()})
			return msgs[start:]
		}
	}
	emit(Event{Kind: EventText, Text: fmt.Sprintf("\n(stopped after %d tool steps)", steps)})
	emit(Event{Kind: EventDone})
	return msgs[start:]
}

// runTool executes one call through the confirm gate and returns the text
// fed back to the model.
func (a *Agent) runTool(ctx context.Context, call ToolCall, emit func(Event)) (result string) {
	t, ok := FindTool(a.Tools, call.Name)
	if !ok {
		names := make([]string, 0, len(a.Tools))
		for _, t := range a.Tools {
			names = append(names, t.Name)
		}
		return fmt.Sprintf("tool %q does not exist. Available: %s.", call.Name, strings.Join(names, ", "))
	}
	args := call.Args
	if args == nil {
		args = map[string]any{}
	}
	human := t.Human(args)
	emit(Event{Kind: EventToolStart, Tool: t.Name, Detail: human})
	if t.Mutating && !(t.Class != "" && a.AutoApprove[t.Class]) {
		if a.Confirm == nil || !a.Confirm(ctx, t, human) {
			emit(Event{Kind: EventToolResult, Tool: t.Name, Detail: "declined by user"})
			return t.Name + ": the user declined this action. Do not retry it; continue without it or ask what to do instead."
		}
	}
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprintf("error: %v", r)
			emit(Event{Kind: EventToolResult, Tool: t.Name, Detail: result})
		}
	}()
	out, err := t.Run(ctx, args)
	if err != nil {
		out = "error: " + Redact(err.Error())
	}
	emit(Event{Kind: EventToolResult, Tool: t.Name, Detail: out})
	return out
}

var toolFenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// ParseTextToolCall extracts {"tool":…,"args":…} from a reply, tolerating
// a ```json fence, a bare fence or a naked top-level object.
func ParseTextToolCall(reply string) (ToolCall, bool) {
	var js string
	if m := toolFenceRe.FindStringSubmatch(reply); m != nil {
		js = m[1]
	} else if t := strings.TrimSpace(reply); strings.HasPrefix(t, "{") && strings.Contains(t, `"tool"`) {
		js = t
	}
	if js == "" {
		return ToolCall{}, false
	}
	var obj struct {
		Tool string         `json:"tool"`
		Args map[string]any `json:"args"`
	}
	if err := json.Unmarshal([]byte(js), &obj); err != nil || obj.Tool == "" {
		return ToolCall{}, false
	}
	if obj.Args == nil {
		obj.Args = map[string]any{}
	}
	return ToolCall{Name: obj.Tool, Args: obj.Args}, true
}

// FirstCodeBlock returns the body of the first fenced code block in s
// (ok=false when there is none).
func FirstCodeBlock(s string) (string, bool) {
	i := strings.Index(s, "```")
	if i < 0 {
		return "", false
	}
	rest := s[i+3:]
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		return "", false
	}
	rest = rest[nl+1:]
	if j := strings.Index(rest, "```"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimRight(rest, "\n"), true
}
