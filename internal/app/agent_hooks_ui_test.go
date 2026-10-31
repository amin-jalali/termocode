package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Spec (OMX-34): Agent: State Hooks… shows the file and the lines that
// change with Not now first; Not now writes nothing; Install writes the
// hooks; picking it again offers Remove.
func TestAgentHooksUI(t *testing.T) {
	m, _, _, _ := aiSkillsModel(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	old := agentHooksExe
	agentHooksExe = func() string { return "/usr/bin/termocode" }
	t.Cleanup(func() { agentHooksExe = old })
	m.openAgentHooks()
	if m.pickerKind != pickerKindAgentHooks {
		t.Fatal("no picker")
	}
	m.onAgentHooksPicked("claude")
	if !m.confirmOpen || m.confirmKind != confirmKindAgentHooks || m.confirm.SafeID() != "cancel" {
		t.Fatal("no Not-now-first card")
	}
	if box := ansi.Strip(m.confirm.Box()); !strings.Contains(box, "settings.json") || !strings.Contains(box, "hooks.Stop") {
		t.Errorf("card:\n%s", box)
	}
	path := filepath.Join(home, ".claude", "settings.json")
	m.answerAIRulesForTest("cancel")
	if _, err := os.Stat(path); err == nil {
		t.Fatal("Not now wrote the file")
	}
	m.onAgentHooksPicked("claude")
	m.answerAIRulesForTest("apply")
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "/usr/bin/termocode agent report done --agent claude") {
		t.Fatalf("file %s", b)
	}
	m.onAgentHooksPicked("claude")
	if !strings.Contains(ansi.Strip(m.confirm.Box()), "Remove") {
		t.Error("installed hooks do not offer Remove")
	}
}

// Spec (OMX-34): the palette entry Agent: State Hooks… opens the picker,
// also before an AI provider is set up (it only changes agent CLI files).
func TestAgentHooksPaletteEntryOpensPicker(t *testing.T) {
	m := aiTestModel(t)
	t.Setenv("HOME", t.TempDir())
	id := ""
	for _, e := range aiPaletteItems() {
		if strings.HasPrefix(e.title, "Agent: State Hooks") {
			id = e.id
		}
	}
	if _, ok := m.dispatchAIPalette(id); !ok || m.pickerKind != pickerKindAgentHooks {
		t.Fatalf("palette id %q: ok=%v picker=%v", id, ok, m.pickerKind)
	}
}
