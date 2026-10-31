package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

func TestAIDoctorResolutionAndSecrets(t *testing.T) {
	s := &aiState{cfg: ai.Config{Kind: ai.KindOpenAI, Model: "main-model", APIKey: "do-not-show-this"}}
	s.slots = ai.NewSlotPool(map[string]ai.Config{"quick": {Kind: ai.KindOllama, Model: "small"}}, nil)
	s.profiles.Add(ai.Profile{Name: "test", Steps: []ai.ProfileStep{
		{ID: "write"}, {ID: "check", Category: "quick"}, {ID: "missing", Slot: "absent"}, {ID: "external", CLI: ai.CLICodex},
	}})
	body := s.aiDoctorReport()
	for _, want := range []string{"test", "write", "main-model", "quick", "small", "not needed", "missing", "not found", "present", "external", "CLI login", "Fallback", "none"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
	if strings.Contains(body, s.cfg.APIKey) {
		t.Fatal("key exposed")
	}
}

func TestAIDoctorPage(t *testing.T) {
	m, _, _, _ := aiSkillsModel(t)
	m.w, m.h = 100, 40
	m.openAIDoctor()
	if !m.previewOpen || !strings.Contains(m.preview.View(), "AI Doctor") {
		t.Fatal("doctor page not opened")
	}
}
