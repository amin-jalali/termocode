package ai

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Spec (Group R2): a review tool runs without the confirm dialog, even
// when its class is not auto-approved; it sees its call id; BeforeCalls
// sees the whole step first; and the result event carries the gate and
// who decided (for the AI audit log).
func TestAgentReviewGateCallIDAndBeforeCalls(t *testing.T) {
	var gotID string
	var order []string
	tool := Tool{Name: "edit", Mutating: true, Class: ClassReview,
		Run: func(ctx context.Context, _ map[string]any) (string, error) {
			gotID = CallID(ctx)
			order = append(order, "run")
			return "reviewed", nil
		}}
	safe := Tool{Name: "look", Run: func(context.Context, map[string]any) (string, error) { return "seen", nil }}
	ask := Tool{Name: "push", Mutating: true, Class: ClassGit, Run: func(context.Context, map[string]any) (string, error) { return "pushed", nil }}
	step := Chunk{ToolCalls: []ToolCall{{ID: "r1", Name: "edit", Args: map[string]any{"path": "a"}}, {ID: "l1", Name: "look"}, {ID: "p1", Name: "push"}}}
	p := &scripted{tools: true, replies: []Chunk{step, {Delta: "ok"}}}
	a := &Agent{Provider: p, Native: true, Tools: []Tool{tool, safe, ask},
		Confirm: func(_ context.Context, t Tool, _ string) bool {
			if t.Name != "push" {
				panic("asked for " + t.Name)
			}
			return true
		},
		BeforeCalls: func(_ context.Context, calls []ToolCall) { order = append(order, fmt.Sprint("before ", len(calls))) }}
	evs, _ := collectEvents(a, nil)
	if gotID != "r1" {
		t.Errorf("call id %q", gotID)
	}
	if strings.Join(order, ",") != "before 3,run" {
		t.Errorf("order %v", order)
	}
	var gates []string
	for _, e := range evs {
		switch e.Kind {
		case EventToolStart:
			if e.Tool == "edit" && e.Args["path"] != "a" {
				t.Errorf("start args %v", e.Args)
			}
		case EventToolResult:
			gates = append(gates, e.Tool+":"+e.Gate+":"+e.Decision)
		}
	}
	want := "edit:review:user (review),look:auto:auto,push:confirm:user allowed"
	if strings.Join(gates, ",") != want {
		t.Errorf("gates %v\nwant %s", gates, want)
	}
	if !strings.Contains(SystemPrompt("base", []Tool{tool}, true), "accept or reject each change") {
		t.Error("system prompt does not explain the review")
	}
	if !strings.Contains(ToolsPromptSpec([]Tool{tool}), "reviews the change as a diff") {
		t.Error("text protocol does not say the tool is reviewed")
	}
}
