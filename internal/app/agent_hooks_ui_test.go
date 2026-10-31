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
