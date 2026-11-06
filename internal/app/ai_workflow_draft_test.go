package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// WFX-9: workflow_draft writes a workflow file after a card that shows
// the file (a diff for a change) and the writes it will do; Cancel is
// first and focused, and Create trusts exactly the bytes it wrote.

// wfxDraftRig is wfxRig with the chat's root in the workspace.
func wfxDraftRig(t *testing.T) (*Model, string) {
	t.Helper()
	m, ws := wfxRig(t)
	t.Chdir(ws)
	m.output = newOutputStore()
	m.ai.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
	m.ai.loadProfiles(ai.Credentials{})
	m.ai.slotFake = map[string]aiSlot{}
	m.actionsOpen, m.actionsPinned = true, true
	return m, ws
}

// Spec: the card puts Cancel first (with the focus) and names Create;
// the tool always asks (no Always allow class) and a read-only step
// does not get it.
func TestAIWorkflowDraftGate(t *testing.T) {
	m, ws := wfxRig(t)
	tl, ok := ai.FindTool(m.aiChatTools(ws, nil), "workflow_draft")
	if !ok {
		t.Fatal("no workflow_draft")
	}
	if !tl.Mutating || tl.ReadOnly || tl.Class != "" {
		t.Errorf("mutating %v read-only %v class %q", tl.Mutating, tl.ReadOnly, tl.Class)
	}
	reply := make(chan bool, 1)
	m.onAIChatConfirm(aiChatConfirmMsg{gen: m.ai.chatGen, tool: tl, human: "x", reply: reply, ch: make(chan tea.Msg, 1)})
	var titles []string
	for _, b := range m.confirm.Buttons() {
		titles = append(titles, b.Title)
	}
	if got := strings.Join(titles, " · "); got != "Cancel · Create" {
		t.Errorf("buttons %q", got)
	}
	if b := m.confirm.Buttons(); b[0].ID != "deny" {
		t.Errorf("first (focused) button %q, want Cancel (deny)", b[0].ID)
	}
	m.answerAIToolConfirm("deny")
	if <-reply {
		t.Error("Cancel allowed")
	}
}

// Spec: a new file: the card shows its path, its lines and its writes;
// Cancel writes nothing; Create writes the file, which is then valid
// and trusted for exactly these bytes.
func TestAIWorkflowDraftCreate(t *testing.T) {
	m, ws := wfxDraftRig(t)
	content := fmt.Sprintf(wfxSample, "fresh") + "\n"
	path := filepath.Join(ws, ".termocode", "workflows", "fresh.jsonc")
	args := map[string]any{"name": "fresh", "content": content}

	res, card := aiWriteTurn(t, m, "workflow_draft", args, "deny")
	if !card.opened {
		t.Fatal("no card")
	}
	for _, want := range []string{".termocode/workflows/fresh.jsonc (new, 8 lines)", "trusts this exact version",
		"rep → file reports/docs.md (create) · asks each time", `+   "version": 1, "name": "fresh", "title": "Docs report",`} {
		if !strings.Contains(card.body, want) {
			t.Errorf("card misses %q:\n%s", want, card.body)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Cancel wrote the file (%v); result %q", err, res)
	}

	res, _ = aiWriteTurn(t, m, "workflow_draft", args, "allow")
	if !strings.Contains(res, "Created .termocode/workflows/fresh.jsonc") {
		t.Errorf("result %q", res)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != content {
		t.Fatalf("file %q (%v)", got, err)
	}
	e, ok := wire.Find(m.wfxS().list, "fresh")
	if !ok || e.Broken() || !e.Trusted {
		t.Errorf("the view has it: found %v broken %v trusted %v", ok, e.Broken(), e.Trusted)
	}
	if left, _ := filepath.Glob(filepath.Join(ws, ".termocode", "workflows", ".*")); len(left) > 0 {
		t.Errorf("temp files left: %v", left)
	}
}

// Spec: a change shows a diff against the file on disk; the new bytes
// are trusted, a file the user had not trusted before too (the card
// showed it).
func TestAIWorkflowDraftChange(t *testing.T) {
	m, _ := wfxDraftRig(t)
	content := strings.Replace(fmt.Sprintf(wfxSample, "docs-report"), `"title": "Docs report"`, `"title": "Docs count"`, 1)
	res, card := aiWriteTurn(t, m, "workflow_draft", map[string]any{"name": "docs-report", "content": content}, "allow")
	for _, want := range []string{"Change .termocode/workflows/docs-report.jsonc", `-   "version": 1, "name": "docs-report", "title": "Docs report",`,
		`+   "version": 1, "name": "docs-report", "title": "Docs count",`} {
		if !strings.Contains(card.body, want) {
			t.Errorf("card misses %q:\n%s", want, card.body)
		}
	}
	if !strings.Contains(res, "Saved the change to .termocode/workflows/docs-report.jsonc") {
		t.Errorf("result %q", res)
	}
	e, _ := wire.Find(m.wfxS().list, "docs-report")
	if !e.Trusted || e.File == nil || e.File.Title != "Docs count" {
		t.Errorf("trusted %v file %+v", e.Trusted, e.File)
	}
}

// Spec: a draft that is not valid, has a bad name or holds a key never
// reaches the card; the model gets the problems with their lines.
func TestAIWorkflowDraftRefused(t *testing.T) {
	m, ws := wfxDraftRig(t)
	key := "sk-" + "test0123456789abcdefghijklmnop"
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"name": "bad", "content": `{ "version": 1, "name": "bad", "steps": [ { "type": "nope" } ] }`}, "line 1"},
		{map[string]any{"name": "Bad Name", "content": fmt.Sprintf(wfxSample, "x")}, "name"},
		{map[string]any{"name": "other", "content": fmt.Sprintf(wfxSample, "x")}, "not valid"},
		{map[string]any{"name": "leak", "content": strings.Replace(fmt.Sprintf(wfxSample, "leak"), `"{{len(steps.docs.items)}} docs"`, `"`+key+`"`, 1)}, "not valid"},
		{map[string]any{"name": "empty"}, "content"},
	} {
		res, card := aiWriteTurn(t, m, "workflow_draft", tc.args, "allow")
		if card.opened {
			t.Errorf("%v: a card opened", tc.args["name"])
		}
		if !strings.Contains(res, tc.want) {
			t.Errorf("%v: result %q, want %q", tc.args["name"], res, tc.want)
		}
		if strings.Contains(res, key) {
			t.Errorf("the key is echoed: %q", res)
		}
	}
	if ents, _ := os.ReadDir(filepath.Join(ws, ".termocode", "workflows")); len(ents) != 2 {
		t.Errorf("files written: %d entries", len(ents))
	}
}
