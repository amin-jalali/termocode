package ai

import (
	"context"
	"strings"
	"testing"
)

// countTool is a read-only tool that counts its runs.
func countTool(runs *int) Tool {
	return Tool{Name: "peek", ReadOnly: true, Run: func(context.Context, map[string]any) (string, error) {
		*runs++
		return "same answer", nil
	}}
}

func peekCall(id, path string) Chunk {
	return Chunk{ToolCalls: []ToolCall{{ID: id, Name: "peek", Args: map[string]any{"path": path, "n": 1}}}}
}

// The same call (same tool, same arguments) the 4th time in a row is not
// run: the model gets a note instead and the user sees it too.
func TestAgentRepeatedCallIsStopped(t *testing.T) {
	runs := 0
	p := &scripted{tools: true, replies: []Chunk{
		peekCall("1", "a"), peekCall("2", "a"), peekCall("3", "a"), peekCall("4", "a"),
		{Delta: "I will try something else"},
	}}
	evs, _ := collectEvents(&Agent{Provider: p, Native: true, Tools: []Tool{countTool(&runs)}}, nil)
	if runs != 3 {
		t.Fatalf("tool ran %d times, want 3", runs)
	}
	last := p.seen[4][len(p.seen[4])-1]
	if last.Role != RoleTool || !strings.Contains(last.Content, "4 times in a row") {
		t.Fatalf("the model was not told: %+v", last)
	}
	note := false
	for _, e := range evs {
		if e.Kind == EventToolNote && strings.Contains(e.Detail, "4 times in a row") {
			note = true
		}
	}
	if !note {
		t.Fatal("the user was not told")
	}
	if e := evs[len(evs)-1]; e.Kind != EventDone {
		t.Fatalf("turn did not end normally: %+v", e)
	}
}

// After the note, the same call once more ends the turn with a note.
func TestAgentRepeatedCallAfterTheNoteEndsTheTurn(t *testing.T) {
	runs := 0
	p := &scripted{tools: true, replies: []Chunk{
		peekCall("1", "a"), peekCall("2", "a"), peekCall("3", "a"), peekCall("4", "a"), peekCall("5", "a"),
		{Delta: "never asked"},
	}}
	evs, _ := collectEvents(&Agent{Provider: p, Native: true, Tools: []Tool{countTool(&runs)}}, nil)
	if p.calls != 5 || runs != 3 {
		t.Fatalf("model asked %d times, tool ran %d times; want 5 and 3", p.calls, runs)
	}
	var text string
	for _, e := range evs {
		if e.Kind == EventText {
			text += e.Text
		}
	}
	if !strings.Contains(text, "Stopped") || !strings.Contains(text, "peek") {
		t.Fatalf("no stop note: %q", text)
	}
	if e := evs[len(evs)-1]; e.Kind != EventDone || e.StepLimit {
		t.Fatalf("last event %+v", e)
	}
}

// Other arguments, or another call between, start the count again;
// RepeatLimit sets the count and a negative one turns it off.
func TestAgentRepeatLimitCountsOnlyARun(t *testing.T) {
	runs := 0
	p := &scripted{tools: true, replies: []Chunk{
		peekCall("1", "a"), peekCall("2", "a"), peekCall("3", "a"), peekCall("4", "b"),
		peekCall("5", "a"), peekCall("6", "a"), peekCall("7", "a"),
	}}
	collectEvents(&Agent{Provider: p, Native: true, Tools: []Tool{countTool(&runs)}}, nil)
	if runs != 7 {
		t.Fatalf("tool ran %d times, want 7", runs)
	}
	runs = 0
	p = &scripted{tools: true, replies: []Chunk{peekCall("1", "a"), peekCall("2", "a"), peekCall("3", "a")}}
	collectEvents(&Agent{Provider: p, Native: true, Tools: []Tool{countTool(&runs)}, RepeatLimit: 2}, nil)
	if runs != 1 {
		t.Fatalf("limit 2: tool ran %d times, want 1", runs)
	}
	runs = 0
	p = &scripted{tools: true, replies: []Chunk{peekCall("1", "a"), peekCall("2", "a"), peekCall("3", "a"), peekCall("4", "a"), peekCall("5", "a")}}
	collectEvents(&Agent{Provider: p, Native: true, Tools: []Tool{countTool(&runs)}, RepeatLimit: -1}, nil)
	if runs != 5 {
		t.Fatalf("off: tool ran %d times, want 5", runs)
	}
}

func TestAgentRepeatStopIsOnTheDoneEvent(t *testing.T) {
	runs := 0
	p := &scripted{tools: true, replies: []Chunk{peekCall("1", "a"), peekCall("2", "a"), peekCall("3", "a")}}
	evs, _ := collectEvents(&Agent{Provider: p, Native: true, Tools: []Tool{countTool(&runs)}, RepeatLimit: 2}, nil)
	if e := evs[len(evs)-1]; e.Kind != EventDone || !e.RepeatStop {
		t.Fatalf("last event %+v, want Done with RepeatStop", e)
	}
}
