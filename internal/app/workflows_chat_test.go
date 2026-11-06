package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// WFX-9: the ways into building by chat, and what the chat knows.

// Spec (plan §5.3): AI: New Workflow… (and n in the view) opens the chat
// with a starter prompt for the user to finish; nothing is sent.
func TestWorkflowNewByChat(t *testing.T) {
	m, _ := wfxRig(t)
	found := false
	for _, it := range workflowsPaletteItems() {
		found = found || (it.ID == "workflow-new" && it.Title == "AI: New Workflow…")
	}
	if !found {
		t.Fatal("no palette item AI: New Workflow…")
	}
	if _, ok := m.dispatchWorkflowsPalette("workflow-new"); !ok {
		t.Fatal("workflow-new not handled")
	}
	if got := string(m.ai.input); got != wfxNewPrompt || !strings.HasPrefix(got, "Make a workflow") {
		t.Errorf("input %q", got)
	}
	if m.ai.streaming || len(m.ai.conv.Messages) != 0 {
		t.Error("the starter prompt was sent")
	}
	m.ai.input = nil
	m.handleWorkflowsSidebarKey(wfxKey('n'))
	if string(m.ai.input) != wfxNewPrompt {
		t.Errorf("n: input %q", string(m.ai.input))
	}
}

// Spec (§5.2): c on a workflow opens the chat with its file attached and
// a hint prompt.
func TestWorkflowChangeByChat(t *testing.T) {
	m, _ := wfxRig(t)
	m.wfxS().sel = "docs-report"
	m.wfxShowPage("docs-report", true)
	if _, ok := m.handleWorkflowsPageKey(wfxKey('c')); !ok {
		t.Fatal("c not handled")
	}
	if got := string(m.ai.input); got != "Change the workflow docs-report so that " {
		t.Errorf("input %q", got)
	}
	att := false
	for _, a := range m.ai.attachments {
		att = att || strings.Contains(a.Label, "docs-report.jsonc")
	}
	if !att {
		t.Errorf("the file is not attached: %+v", m.ai.attachments)
	}
}

// Spec (§5.8): AI: New Workflow… is never run by the AI (it starts a chat
// with the user); its key card lists n and c.
func TestWorkflowNewPolicyAndKeys(t *testing.T) {
	if p, ok := aiActionPolicies["workflow-new"]; !ok || p.Gate != aiGateNever {
		t.Errorf("workflow-new policy %+v", p)
	}
	keys := map[string]bool{}
	for _, r := range wfxKeyRowsList() {
		keys[r[0]] = true
	}
	if !keys["n"] || !keys["c"] {
		t.Errorf("key card misses n or c: %v", keys)
	}
}

// Spec: the chat's system prompt has the workflow note (a schema summary
// and "ask before adding write steps") in a workspace with workflows, or
// in a chat about workflows; else it stays out.
func TestWorkflowsSystemPromptNote(t *testing.T) {
	m, ws := wfxRig(t)
	base := m.aiChatBasePrompt(ws, ai.Rules{}, "")
	for _, want := range []string{"Workflows (" + ".termocode/workflows/<name>.jsonc", `"version": 1`, "source", "write",
		"Ask the user before you add a write step", "workflow_draft"} {
		if !strings.Contains(base, want) {
			t.Errorf("prompt misses %q", want)
		}
	}
	empty := t.TempDir()
	if strings.Contains(m.aiChatBasePrompt(empty, ai.Rules{}, ""), "Ask the user before you add a write step") {
		t.Error("the note is in every chat")
	}
	m.ai.conv.Messages = append(m.ai.conv.Messages, ai.StoredMessage{Role: ai.StoredUser, Text: "Make a workflow that counts docs"})
	if !strings.Contains(m.aiChatBasePrompt(empty, ai.Rules{}, ""), "Ask the user before you add a write step") {
		t.Error("a chat about workflows has no note")
	}
}
