package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (OMX-11): "/name" runs a command file. A user command is sent at
// once (expanded); a project command waits for the project skills card,
// and when refused it is not sent and the text goes back in the input.
func TestAIChatCommandFiles(t *testing.T) {
	m, rec, root, _ := aiSkillsModel(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSkill := func(dir, file, text string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill(filepath.Join(home, ".claude", "commands"), "explain.md", "Explain $ARGUMENTS simply.\n")
	driveAIChat(t, m, m.sendAIChat("/explain main.go"), nil)
	if len(rec.reqs) != 1 || !strings.Contains(aiReqText(rec.reqs[0]), "Explain main.go simply.") {
		t.Fatalf("user command not expanded: %d reqs", len(rec.reqs))
	}

	writeSkill(filepath.Join(root, ".claude", "commands"), "ship.md", "---\ndescription: Ship it\n---\nRun the release.\n")
	m.sendAIChat("/ship now")
	if !m.confirmOpen || m.confirmKind != confirmKindAISkillsTrust {
		t.Fatal("no trust card for a project command")
	}
	if box := ansi.Strip(m.confirm.Box()); !strings.Contains(box, "/ship — Ship it") {
		t.Errorf("card:\n%s", box)
	}
	m.answerAIRulesForTest("deny") // only an info toast follows
	if len(rec.reqs) != 1 {
		t.Fatalf("refused project command was sent")
	}
	if string(m.ai.input) != "/ship now" {
		t.Errorf("input %q", string(m.ai.input))
	}
	m.openAISkillsTrust()
	m.answerAIRulesForTest("use")
	driveAIChat(t, m, m.sendAIChat("/ship now"), nil)
	if len(rec.reqs) != 2 || !strings.Contains(aiReqText(rec.reqs[1]), "Run the release.\n\nnow") {
		t.Fatalf("trusted project command not sent")
	}
	// Not a command: sent as it is.
	driveAIChat(t, m, m.sendAIChat("/usr/bin is a path"), nil)
	if len(rec.reqs) != 3 || !strings.Contains(aiReqText(rec.reqs[2]), "/usr/bin is a path") {
		t.Fatalf("plain text changed")
	}
}

// Spec (OMX-11): agent files are project profiles; they go behind the
// project profile card like ai-profiles.json.
func TestAIAgentFileProfiles(t *testing.T) {
	m, _, root, _ := aiSkillsModel(t)
	writeAISkill(t, filepath.Join(root, ".claude", "agents"), "rev.md", "reviewer2", "Reviews code", "You review.")
	s := m.aiS()
	s.reloadProfiles()
	p, ok := s.profiles.Get("reviewer2")
	if !ok || p.Source != ai.ProfileSourceProject || p.Steps[0].System != "You review." {
		t.Fatalf("agent file profile %+v %v", p, ok)
	}
	if m.aiProfileTrust(p) == aiTrustUse {
		t.Error("an agent file profile is trusted without a card")
	}
}
