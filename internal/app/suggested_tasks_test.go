package app

import (
	"strings"
	"testing"
)

// CMP-44: Tasks: Suggested Tasks… scans the workspace for TODO / FIXME
// comments and likely slow Go code and lists them, most useful first.
// A row opens a Cancel-first card: Open in Editor, or Ask AI to Do It
// (it only fills the chat box; nothing is sent).
func TestSuggestedTasksPickListAndCard(t *testing.T) {
	m, dir := covGitRepo(t)
	gitxWithAI(m, "")
	covWrite(t, dir, "a.go", "package a\n\nfunc f() {\n\t// TODO: cache the result\n}\n")
	covWrite(t, dir, "b.go", "package a\n// FIXME: race on close\n")
	covWrite(t, dir, "node_modules/x.js", "// FIXME not ours\n")

	found := false
	for _, e := range PaletteCatalog() {
		if e.ID == "tasks-suggest" && e.Title == "Tasks: Suggested Tasks…" {
			found = true
		}
	}
	if !found {
		t.Fatal("palette has no Tasks: Suggested Tasks…")
	}
	if p, ok := aiActionPolicies["tasks-suggest"]; !ok || p.Gate != aiGateNever {
		t.Errorf("AI policy %+v %v", p, ok)
	}

	covMsg[gitxDoneMsg](t, m.dispatchPaletteAction("tasks-suggest")).apply(m)
	if !m.pickerOpen || m.pickerKind != pickerKindGitx {
		t.Fatalf("picker open=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
	rows := m.gitx.items
	if len(rows) != 2 {
		t.Fatalf("rows %+v", rows)
	}
	if !strings.Contains(rows[0].Title, "FIXME") || !strings.Contains(rows[0].Title, "race on close") || rows[0].Hint != "b.go:2" {
		t.Errorf("first row %+v", rows[0])
	}
	if !strings.Contains(rows[1].Title, "TODO") || rows[1].Hint != "a.go:4" {
		t.Errorf("second row %+v", rows[1])
	}

	// Pick the TODO: a card, Cancel first.
	m.pickerOpen = false
	m.handleGitxPickerSelect(rows[1].ID)
	if !m.confirmOpen || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card open=%v safe=%q", m.confirmOpen, m.confirm.SafeID())
	}
	if v := stripANSI(m.confirm.View()); !strings.Contains(v, "a.go:4") || !strings.Contains(v, "cache the result") || !strings.Contains(v, "Ask AI") {
		t.Errorf("card %q", v)
	}
	// Cancel goes back to the list.
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if !m.pickerOpen || m.pickerKind != pickerKindGitx || len(m.gitx.items) != 2 {
		t.Fatalf("cancel did not go back to the list: %v", m.pickerOpen)
	}
	if len(m.aiS().input) != 0 {
		t.Error("cancel filled the chat")
	}

	// Ask AI fills the chat box only.
	m.pickerOpen = false
	m.handleGitxPickerSelect(rows[1].ID)
	m.confirmOpen = false
	m.handleConfirmSelect("ai")
	in := string(m.aiS().input)
	if !strings.Contains(in, "a.go:4") || !strings.Contains(in, "cache the result") || !strings.Contains(in, "TODO") {
		t.Errorf("chat box %q", in)
	}
	if len(m.aiS().conv.Messages) != 0 {
		t.Error("the task was sent without the user")
	}
}

// An empty workspace says so instead of an empty list.
func TestSuggestedTasksNone(t *testing.T) {
	m, _ := covGitRepo(t)
	covMsg[gitxDoneMsg](t, m.dispatchPaletteAction("tasks-suggest")).apply(m)
	if m.pickerOpen {
		t.Error("an empty list opened")
	}
}
