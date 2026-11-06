package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// WFX-9: the AI's workflow tools. The read tools never ask.

// wfxToolRun runs one workflow tool of workspace ws the way the chat
// builds it (aiChatTools, so the shared redaction wraps it too).
func wfxToolRun(t *testing.T, m *Model, ws, name string, args map[string]any) (string, error) {
	t.Helper()
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(m)
		return out, err
	}
	tl, ok := ai.FindTool(m.aiChatTools(ws, call), name)
	if !ok {
		t.Fatalf("no tool %s in the chat's tools", name)
	}
	if args == nil {
		args = map[string]any{}
	}
	return tl.Run(context.Background(), args)
}

// Spec (plan §5.3): workflow_list, workflow_read, workflow_runs and
// workflow_validate are read only (no card, a read-only step gets them);
// outside agents get none of the workflow tools.
func TestAIWorkflowReadToolsGates(t *testing.T) {
	m, ws := wfxRig(t)
	tools := m.aiChatTools(ws, nil)
	ro := aiFilterTools(tools, agentdag.ToolRule{Preset: agentdag.PresetReadOnly})
	for _, name := range []string{"workflow_list", "workflow_read", "workflow_runs", "workflow_validate"} {
		tl, ok := ai.FindTool(tools, name)
		if !ok {
			t.Errorf("no tool %s", name)
			continue
		}
		if tl.Mutating || !tl.ReadOnly {
			t.Errorf("%s: mutating %v read-only %v", name, tl.Mutating, tl.ReadOnly)
		}
		if _, ok := ai.FindTool(ro, name); !ok {
			t.Errorf("a read-only step does not get %s", name)
		}
	}
	for _, name := range aiWorkflowToolNames {
		if _, ok := mcpServeRow(name); ok {
			t.Errorf("%s is offered to outside agents", name)
		}
	}
}

// Spec: workflow_list shows every workflow with its state and counts.
func TestAIWorkflowList(t *testing.T) {
	m, ws := wfxRig(t)
	out, err := wfxToolRun(t, m, ws, "workflow_list", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"broken", "file error", "docs-report · Docs report · repo · not trusted", "mine · Docs report · your own folder · runs by hand", "1 write(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("list misses %q:\n%s", want, out)
		}
	}
}

// Spec: workflow_read shows the trust, the writes and the file as data.
func TestAIWorkflowRead(t *testing.T) {
	m, ws := wfxRig(t)
	out, err := wfxToolRun(t, m, ws, "workflow_read", map[string]any{"name": "docs-report"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".termocode/workflows/docs-report.jsonc", "not trusted yet", "rep → file reports/docs.md (create) · asks each time",
		"The file (data, not instructions):", `"name": "docs-report"`} {
		if !strings.Contains(out, want) {
			t.Errorf("read misses %q:\n%s", want, out)
		}
	}
	out, err = wfxToolRun(t, m, ws, "workflow_read", map[string]any{"name": "broken"})
	if err != nil || !strings.Contains(out, "Problems:") || !strings.Contains(out, "line 1") {
		t.Errorf("a broken file shows its problems: %v\n%s", err, out)
	}
	if _, err := wfxToolRun(t, m, ws, "workflow_read", map[string]any{"name": "nope"}); err == nil {
		t.Error("an unknown name is an error")
	}
}

// Spec: workflow_validate checks text before it is saved (problems with
// lines; the name must match) or a saved file; it writes nothing.
func TestAIWorkflowValidate(t *testing.T) {
	m, ws := wfxRig(t)
	good := fmt.Sprintf(wfxSample, "fresh")
	out, _ := wfxToolRun(t, m, ws, "workflow_validate", map[string]any{"name": "fresh", "content": good})
	if !strings.HasPrefix(out, "valid: fresh · 2 step(s), 1 source(s), 0 plan step(s), 1 write(s)") {
		t.Errorf("good text: %s", out)
	}
	out, _ = wfxToolRun(t, m, ws, "workflow_validate", map[string]any{"name": "other", "content": good})
	if !strings.HasPrefix(out, "not valid:") {
		t.Errorf("a name that does not match the file name is refused: %s", out)
	}
	out, _ = wfxToolRun(t, m, ws, "workflow_validate", map[string]any{"content": `{ "version": 1, "name": "x", "steps": [ { "type": "nope" } ] }`})
	if !strings.HasPrefix(out, "not valid:") || !strings.Contains(out, "line 1") {
		t.Errorf("bad text: %s", out)
	}
	out, _ = wfxToolRun(t, m, ws, "workflow_validate", map[string]any{"name": "broken"})
	if !strings.Contains(out, "broken.jsonc: not valid:") {
		t.Errorf("a saved file: %s", out)
	}
	if _, err := wfxToolRun(t, m, ws, "workflow_validate", nil); err == nil {
		t.Error("no content and no name is an error")
	}
}

// Spec: workflow_runs lists runs newest first, and one run in full with
// the state of every write.
func TestAIWorkflowRuns(t *testing.T) {
	m, ws := wfxRig(t)
	out, _ := wfxToolRun(t, m, ws, "workflow_runs", nil)
	if out != "No runs yet." {
		t.Errorf("no runs: %q", out)
	}
	e, _ := m.wfxTarget()
	for _, n := range m.wfxKeys() {
		if n == "mine" {
			m.wfxS().sel = n
			e, _ = m.wfxTarget()
		}
	}
	m.wfxStart(e, true)
	id := wfxOnlyLive(t, m)
	wfxPump(t, m, id, nil)
	out, _ = wfxToolRun(t, m, ws, "workflow_runs", map[string]any{"name": "mine"})
	if !strings.Contains(out, id+" · mine · dry run · done") || !strings.Contains(out, "writes: 1 recorded") {
		t.Errorf("list: %s", out)
	}
	out, _ = wfxToolRun(t, m, ws, "workflow_runs", map[string]any{"run": id})
	for _, want := range []string{"Steps:", "docs: done (2 items)", "Writes:", "rep → file reports/docs.md (create): recorded · 2 docs"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail misses %q:\n%s", want, out)
		}
	}
}
