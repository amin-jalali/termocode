package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// aiReqToolNames are the names of the tools request r sends.
func aiReqToolNames(r ai.Request) []string {
	names := make([]string, len(r.Tools))
	for i, t := range r.Tools {
		names[i] = t.Name
	}
	return names
}

// Spec (lean request): a chat request sends a small core set of tools;
// the rest wait in named groups. tool_search loads a group, its tools
// are sent from the next step on, and a loaded group stays for the rest
// of the chat.
func TestAIChatToolGroupsLoadOnSearch(t *testing.T) {
	p := &recordingAIProvider{replies: []ai.Chunk{
		toolCall("tool_search", map[string]any{"query": "db"}),
		{Delta: "loaded"},
		{Delta: "second turn"},
	}}
	m, _ := aiHeadTestModel(t, p)
	m.ai.conv.KeepGoingOff = true
	aiHeadSend(t, m, "how many tables are there?")
	aiHeadSend(t, m, "and now?")
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.reqs) != 3 {
		t.Fatalf("requests %d, want 3", len(p.reqs))
	}
	first := aiReqToolNames(p.reqs[0])
	if n := len(first); n < 10 || n > 16 {
		t.Errorf("first request sends %d tools, want a core set of 10-16: %v", n, first)
	}
	for _, want := range []string{"read_file", "search", "glob", "list_dir", "edit_file", "write_file", "run_command", "git_status", "git_diff", "todo_write", "tool_search"} {
		if !slices.Contains(first, want) {
			t.Errorf("core tool %s missing: %v", want, first)
		}
	}
	if slices.Contains(first, "db_query") {
		t.Errorf("db_query sent before its group was loaded: %v", first)
	}
	for i, r := range p.reqs[1:] {
		if !slices.Contains(aiReqToolNames(r), "db_query") {
			t.Errorf("request %d: the loaded db group is not sent: %v", i+2, aiReqToolNames(r))
		}
	}
	res := aiReqText(p.reqs[1])
	if !strings.Contains(res, "db_query") {
		t.Errorf("tool_search result does not name the loaded tools:\n%s", res)
	}
	desc := ""
	for _, tl := range p.reqs[0].Tools {
		if tl.Name == "tool_search" {
			desc = tl.Description
		}
	}
	for _, g := range []string{"db", "git", "containers"} {
		if !strings.Contains(desc, g) {
			t.Errorf("tool_search does not list group %q: %s", g, desc)
		}
	}
	// The system prompt does not change when a group loads (the head
	// stays cacheable within the chat).
	if p.reqs[0].Messages[0].Content != p.reqs[2].Messages[0].Content {
		t.Error("the system prompt changed after a group was loaded")
	}
}

// Spec (lean request): a call to a tool of a group that is not loaded
// yet still runs (the model knows the name from the prompt), and its
// group loads with it.
func TestAIChatToolGroupsDirectCallLoads(t *testing.T) {
	p := &recordingAIProvider{replies: []ai.Chunk{
		toolCall("db_connections", map[string]any{}),
		{Delta: "done"},
	}}
	m, _ := aiHeadTestModel(t, p)
	m.ai.conv.KeepGoingOff = true
	aiHeadSend(t, m, "list my databases")
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.reqs) != 2 {
		t.Fatalf("requests %d, want 2", len(p.reqs))
	}
	if res := aiReqText(p.reqs[1]); strings.Contains(res, "unknown tool") {
		t.Errorf("the hidden tool did not run:\n%s", res)
	}
	if !slices.Contains(aiReqToolNames(p.reqs[1]), "db_query") {
		t.Errorf("calling db_connections did not load the db group: %v", aiReqToolNames(p.reqs[1]))
	}
}

// Spec (lean request): what the user attached says which group is
// needed: a database attachment sends the db group from the first
// request.
func TestAIChatToolGroupsAttachPreloads(t *testing.T) {
	p := &recordingAIProvider{}
	m, _ := aiHeadTestModel(t, p)
	m.ai.conv.KeepGoingOff = true
	m.ai.attachments = []aiAttachment{{Label: "db Local (c1)", Text: "users: id, name"}}
	aiHeadSend(t, m, "what is in this table?")
	p.mu.Lock()
	defer p.mu.Unlock()
	if !slices.Contains(aiReqToolNames(p.reqs[0]), "db_query") {
		t.Errorf("a database attachment did not preload the db group: %v", aiReqToolNames(p.reqs[0]))
	}
}

// Spec (lean request): ai_tool_groups "off" sends every tool, as before.
func TestAIChatToolGroupsOff(t *testing.T) {
	p := &recordingAIProvider{}
	m, _ := aiHeadTestModel(t, p)
	m.ai.conv.KeepGoingOff = true
	m.ai.settings.ToolGroups = ai.ToolGroupsOff
	aiHeadSend(t, m, "hello")
	p.mu.Lock()
	defer p.mu.Unlock()
	names := aiReqToolNames(p.reqs[0])
	if len(names) < 100 || slices.Contains(names, "tool_search") {
		t.Errorf("off: %d tools (tool_search in: %v), want every tool and no tool_search", len(names), slices.Contains(names, "tool_search"))
	}
}
