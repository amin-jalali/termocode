package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// agentPackDir is a small pack folder: one agent, one skill, one rule.
func agentPackDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "demo")
	for path, text := range map[string]string{
		"pack.json":            `{"name":"demo","version":"1.0.0"}`,
		"agents/go-review.md":  "---\nname: go-review\ndescription: Reviews Go.\n---\nReview Go code.\n",
		"skills/tdd/SKILL.md":  "---\nname: tdd\ndescription: Test first.\n---\nWrite the test first.\n",
		"rules/common/fmt.md":  "Run gofmt.\n",
		"commands/plan.md":     "---\ndescription: Plan.\n---\nMake a plan.\n",
		"README.md":            "not in the pack\n",
		"agents/sub/nested.md": "not an agent\n",
	} {
		p := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Spec (OMX-46): Agent: Agent Packs… → Install a pack… asks for the
// source, then the agent, then the parts; the preview card lists every
// file it adds or replaces and the token cost, with Cancel first and
// focused. Cancel writes nothing; Install writes the files; the pack is
// then listed as pinned and Enter on it opens the undo card (Cancel
// first), whose Undo puts the old file back.
func TestAgentPacksUI(t *testing.T) {
	m, _, _, _ := aiSkillsModel(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	mine := filepath.Join(home, ".claude", "agents", "go-review.md")
	if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("my own\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m.openAgentPacks()
	if !m.pickerOpen || m.pickerKind != pickerKindAgentPacks {
		t.Fatal("no Agent Packs picker")
	}
	m.onAgentPacksPicked("install")
	if !m.promptOpen || m.promptKind != promptKindAgentPacksSource {
		t.Fatal("no source prompt")
	}
	runApply(m, m.onAgentPackSource(agentPackDir(t)))
	if !m.pickerOpen || m.pickerKind != pickerKindAgentPacksTarget {
		t.Fatalf("no agent picker (toast %q)", toastText(*m))
	}
	m.onAgentPacksTargetPicked("claude")
	if !m.pickerOpen || m.pickerKind != pickerKindAgentPacksParts {
		t.Fatal("no parts picker")
	}
	m.onAgentPacksPartsPicked("all")
	if !m.confirmOpen || m.confirmKind != confirmKindAgentPacksInstall || m.confirm.SafeID() != "cancel" {
		t.Fatal("no Cancel-first install card")
	}
	if b := m.confirm.Buttons(); b[0].ID != "cancel" {
		t.Errorf("Cancel is not the first button: %+v", b)
	}
	box := ansi.Strip(m.confirm.Box())
	for _, w := range []string{"demo 1.0.0", "~/.claude/agents/go-review.md", "replaces your file", "~/.claude/skills/tdd/SKILL.md", "tokens"} {
		if !strings.Contains(box, w) {
			t.Errorf("card misses %q:\n%s", w, box)
		}
	}
	m.answerAIRulesForTest("cancel")
	if b, _ := os.ReadFile(mine); string(b) != "my own\n" {
		t.Fatal("Cancel wrote the file")
	}

	runApply(m, m.onAgentPackSource(agentPackDir(t)))
	m.onAgentPacksTargetPicked("claude")
	m.onAgentPacksPartsPicked("all")
	m.answerAIRulesForTest("install")
	if b, _ := os.ReadFile(mine); !strings.Contains(string(b), "Review Go code") {
		t.Fatalf("Install did not write: %q", b)
	}

	m.openAgentPacks()
	if box := ansi.Strip(m.picker.Box()); !strings.Contains(box, "demo 1.0.0") || !strings.Contains(box, "pinned") {
		t.Errorf("the installed pack is not listed as pinned:\n%s", box)
	}
	m.onAgentPacksPicked("undo:demo@claude")
	if !m.confirmOpen || m.confirmKind != confirmKindAgentPacksUndo || m.confirm.SafeID() != "cancel" {
		t.Fatal("no Cancel-first undo card")
	}
	if box := ansi.Strip(m.confirm.Box()); !strings.Contains(box, "~/.claude/agents/go-review.md") {
		t.Errorf("undo card:\n%s", box)
	}
	m.answerAIRulesForTest("undo")
	if b, _ := os.ReadFile(mine); string(b) != "my own\n" {
		t.Fatalf("Undo did not put the old file back: %q", b)
	}
}

// Spec (OMX-46): Codex gets only skills and commands: the parts picker
// says agents and rules are skipped there.
func TestAgentPacksCodexSkipsAgentsAndRules(t *testing.T) {
	m, _, _, _ := aiSkillsModel(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	runApply(m, m.onAgentPackSource(agentPackDir(t)))
	m.onAgentPacksTargetPicked("codex")
	box := ansi.Strip(m.picker.Box())
	if !strings.Contains(box, "Skills") || !strings.Contains(box, "Commands") || strings.Contains(box, "Agents (") || strings.Contains(box, "Rules (") {
		t.Errorf("codex parts:\n%s", box)
	}
	if !strings.Contains(box, "no agent or rule files") {
		t.Errorf("no skip note:\n%s", box)
	}
}

// Spec (OMX-46): a source that is not a folder, or a git address without
// a version, shows a plain message and opens no card.
func TestAgentPacksBadSource(t *testing.T) {
	m, _, _, _ := aiSkillsModel(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	runApply(m, m.onAgentPackSource("https://example.com/pack.git"))
	if m.confirmOpen || (m.pickerOpen && m.pickerKind == pickerKindAgentPacksTarget) {
		t.Fatal("a source without a version went on")
	}
	if !strings.Contains(toastText(*m), "@") {
		t.Errorf("toast %q", toastText(*m))
	}
}

// Spec (OMX-46): the palette entry opens the picker, also before an AI
// provider is set up (it only changes agent CLI files).
func TestAgentPacksPaletteEntryOpensPicker(t *testing.T) {
	m := aiTestModel(t)
	t.Setenv("HOME", t.TempDir())
	id := ""
	for _, e := range aiPaletteItems() {
		if strings.HasPrefix(e.title, "Agent: Agent Packs") {
			id = e.id
		}
	}
	if _, ok := m.dispatchAIPalette(id); !ok || m.pickerKind != pickerKindAgentPacks {
		t.Fatalf("palette id %q: ok=%v picker=%v", id, ok, m.pickerKind)
	}
}
