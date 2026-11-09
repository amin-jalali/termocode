package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AI parity: specs makes and reads specs as Spec: New… / New Bugfix…
// do (no card: the GUI asks only for the goal); approving a phase stays
// with the user. prompt_actions lists, reads and adds saved prompts at
// once, imports after a card (Cancel first in the GUI) and asks before
// an export writes over a file.
func TestAISpecsTool(t *testing.T) {
	m := aiTestModel(t)
	t.Chdir(t.TempDir()) // the workspace root
	tl := aiToolOf(t, &m, "specs")
	if tl.Mutating || tl.Gate != nil {
		t.Error("specs asks; Spec: New… has no card")
	}
	out, err := aiToolDo(t, tl, map[string]any{"op": "new", "goal": "Export the report as CSV"})
	if err != nil || !strings.Contains(out, "requirements.md") {
		t.Fatalf("new: %q %v", out, err)
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "new_bugfix", "goal": "Toast flickers"}); err != nil {
		t.Fatal(err)
	}
	out, err = aiToolDo(t, tl, map[string]any{"op": "list"})
	if err != nil || !strings.Contains(out, "export-the-report-as-csv") || !strings.Contains(out, "bugfix") {
		t.Fatalf("list: %q %v", out, err)
	}
	out, err = aiToolDo(t, tl, map[string]any{"op": "read", "name": "export-the-report-as-csv"})
	if err != nil || !strings.Contains(out, "Next: Requirements") || !strings.Contains(out, "user approves") {
		t.Fatalf("read: %q %v", out, err)
	}
}

func TestAIPromptActionsTool(t *testing.T) {
	m := aiTestModel(t) // a fake config folder
	tl := aiToolOf(t, &m, "prompt_actions")
	for op, ask := range map[string]bool{"list": false, "read": false, "add": false, "import": true} {
		if got, class := tl.Gate(map[string]any{"op": op, "path": "x"}); got != ask || class != "" {
			t.Errorf("%s: ask=%v class=%q, want %v and no Always allow", op, got, class, ask)
		}
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "add", "name": "Review/My changes", "prompt": "Review the diff."}); err != nil {
		t.Fatal(err)
	}
	if out, err := aiToolDo(t, tl, map[string]any{"op": "read", "name": "Review/My changes"}); err != nil || !strings.Contains(out, "Review the diff.") {
		t.Fatalf("read: %q %v", out, err)
	}
	dir := t.TempDir()
	to := filepath.Join(dir, "pa.json")
	if ask, _ := tl.Gate(map[string]any{"op": "export", "path": to}); ask {
		t.Error("export to a new file asks")
	}
	if _, err := aiToolDo(t, tl, map[string]any{"op": "export", "path": to}); err != nil {
		t.Fatal(err)
	}
	if ask, class := tl.Gate(map[string]any{"op": "export", "path": to}); !ask || class != "" {
		t.Error("export over a file does not ask")
	}
	src := filepath.Join(dir, "in.json")
	if err := os.WriteFile(src, []byte(`{"version":1,"actions":[{"name":"Write tests","prompt":"Write tests."}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := aiToolDo(t, tl, map[string]any{"op": "import", "path": src}); err != nil || !strings.Contains(out, "1 added") {
		t.Fatalf("import: %q %v", out, err)
	}
	if out, _ := aiToolDo(t, tl, map[string]any{"op": "list"}); !strings.Contains(out, "write tests") || !strings.Contains(out, "review/my changes") {
		t.Errorf("list: %s", out)
	}
}
