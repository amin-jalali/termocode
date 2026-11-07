package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/bugspec"
)

// Spec (CMP-79): AI: New Bugfix Spec… asks for the bug's title and writes
// .termocode/specs/<slug>/bugfix.md from the template (an existing spec
// is opened, never overwritten). AI: Fix Bug from Spec… lists the specs;
// a spec with an empty section is not sent (the toast says which is
// empty); a filled one opens the chat with the test-first fix prompt and
// the spec attached, for the user to send. The AI runs neither command.
func TestBugspecFlow(t *testing.T) {
	m := aiTestModel(t)
	root := t.TempDir()
	t.Chdir(root)
	ids := map[string]string{}
	for _, it := range bugspecPaletteItems() {
		ids[it.ID] = it.Title
	}
	if ids[bugspecNewPaletteID] != "AI: New Bugfix Spec…" || ids[bugspecFixPaletteID] != "AI: Fix Bug from Spec…" {
		t.Fatalf("palette %v", ids)
	}
	for id := range ids {
		if p, ok := aiActionPolicyFor(id); !ok || p.Gate != aiGateNever {
			t.Errorf("%s policy %+v %v", id, p, ok)
		}
	}

	// No spec yet: the fix command says how to make one.
	if _, ok := m.dispatchBugspecPalette(bugspecFixPaletteID); !ok || m.pickerOpen {
		t.Fatalf("no specs: picker %v", m.pickerOpen)
	}

	if _, ok := m.dispatchBugspecPalette(bugspecNewPaletteID); !ok || !m.promptOpen || m.promptKind != promptKindBugspecTitle {
		t.Fatalf("prompt %v %v", m.promptOpen, m.promptKind)
	}
	m.promptOpen = false
	m.onBugspecTitle("Two toasts at once")
	path := bugspec.PathFor(root, "Two toasts at once")
	body, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(body), "## Unchanged behavior") {
		t.Fatalf("spec %q %v", body, err)
	}
	if err := os.WriteFile(path, []byte("# Bugfix: Two toasts at once\n## Current behavior\nonly one shows\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.onBugspecTitle("Two toasts at once")
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "only one shows") {
		t.Fatal("an existing spec was overwritten")
	}

	m.dispatchBugspecPalette(bugspecFixPaletteID)
	if !m.pickerOpen || m.pickerKind != pickerKindBugspec {
		t.Fatalf("picker %v %v", m.pickerOpen, m.pickerKind)
	}
	view := ansi.Strip(m.picker.View())
	if !strings.Contains(view, "Two toasts at once") || !strings.Contains(view, "2 empty") {
		t.Errorf("picker:\n%s", view)
	}
	id := bugspecPickPfx + filepath.ToSlash(filepath.Join(bugspec.Dir, "two-toasts-at-once", bugspec.FileName))
	m.pickerOpen = false
	m.onBugspecPicked(id)
	if len(m.ai.input) != 0 {
		t.Errorf("a spec with empty sections filled the chat: %q", string(m.ai.input))
	}

	full := "# Bugfix: Two toasts at once\n## Current behavior\nonly one shows\n## Expected behavior\nboth show in order\n## Unchanged behavior\none toast closes after 4 s\n"
	if err := os.WriteFile(path, []byte(full), 0o644); err != nil {
		t.Fatal(err)
	}
	m.onBugspecPicked(id)
	in := string(m.ai.input)
	if !strings.Contains(in, "guard") || !strings.Contains(in, "both show in order") || !strings.Contains(in, "one toast closes after 4 s") {
		t.Errorf("input %q", in)
	}
	att := false
	for _, a := range m.ai.attachments {
		att = att || strings.Contains(a.Label, "bugfix.md")
	}
	if !att {
		t.Errorf("the spec is not attached: %+v", m.ai.attachments)
	}
	if m.ai.streaming || len(m.ai.conv.Messages) != 0 {
		t.Error("the prompt was sent")
	}
	m.onBugspecPicked(bugspecPickPfx + "../../etc/passwd")
	if strings.Contains(string(m.ai.input), "passwd") {
		t.Error("a path outside the specs folder was read")
	}
}
