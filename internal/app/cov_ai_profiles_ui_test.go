package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// AI profiles UI (ai_profiles_ui.go): Work until done back and forth,
// unknown profiles, the save card's answers, the profile files that are
// made when missing, and picker rows that are not profiles.

func TestCovAIProfilesUntilDoneAndUnknown(t *testing.T) {
	m := aiTestModel(t)
	m.aiS().reloadProfiles()
	m.toggleAIUntilDone()
	if m.aiS().activeProfile().Name != ai.ProfileUntilDone {
		t.Fatalf("active = %q", m.aiS().activeProfile().Name)
	}
	m.aiS().prevProfile = "gone-profile"
	m.toggleAIUntilDone()
	if got := m.aiS().activeProfile().Name; got != ai.ProfileDefault || !strings.Contains(covToastText(&m), "Work until done: off · profile "+ai.ProfileDefault) {
		t.Errorf("back to %q: %q", got, covToastText(&m))
	}
	if m.selectAIProfile("no-such") == nil || !strings.Contains(covToastText(&m), "No profile no-such") {
		t.Errorf("unknown profile: %q", covToastText(&m))
	}
	if got := aiUntilDoneSummary(ai.Profile{BudgetUSD: 2}); !strings.HasSuffix(got, "budget $2.00") {
		t.Errorf("budget summary = %q", got)
	}
	if got := aiUntilDoneSummary(ai.Profile{}); !strings.HasSuffix(got, "no budget") {
		t.Errorf("no budget summary = %q", got)
	}
	if m.openAIProfileTrustPicker() == nil || m.pickerOpen {
		t.Error("a trust picker without project profiles")
	}
	m.pickerKind = pickerKindAIProfile
	for _, id := range []string{aiProfileNewID, aiProfileFormID, aiProfileEditID} {
		if cmd, ok := m.handleAIProfilePickerAction(id); !ok || cmd != nil {
			t.Errorf("→ on %s: %v %v", id, ok, cmd != nil)
		}
	}
	if _, ok := m.handleAIProfilePickerAction(aiProfileDetailsID); !ok {
		t.Error("→ on details not taken")
	}
	m.pickerKind = pickerKindAIProfileTrust
	if cmd, ok := m.handleAIProfilePickerSelect("no-such"); !ok || cmd != nil {
		t.Error("trust picker on a missing profile did something")
	}
	m.pickerKind = 0
	if _, ok := m.handleAIProfilePickerAction("x"); ok {
		t.Error("→ outside the profile picker was taken")
	}
}

// The save card: Cancel says not saved; Save writes profiles.json and
// switches; a file that can not be written warns.
func TestCovAIProfileSaveChoice(t *testing.T) {
	m := aiTestModel(t)
	if m.onAIProfileSaveChoice("save") == nil || !strings.Contains(covToastText(&m), "Profile not saved") {
		t.Errorf("nothing pending: %q", covToastText(&m))
	}
	p := covBuiltinProfile(ai.ProfileReview)
	p.Name, p.Source = "my-review", ai.ProfileSourceUser
	m.aiS().profileSave = &aiProfileSavePending{name: p.Name, profile: p}
	m.onAIProfileSaveChoice("save")
	if m.aiS().activeProfile().Name != "my-review" {
		t.Errorf("active = %q", m.aiS().activeProfile().Name)
	}
	path, _ := ai.ProfilesPath()
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "my-review") {
		t.Errorf("profiles.json:\n%s", b)
	}
	_ = os.Remove(path)
	_ = os.MkdirAll(path, 0o755)
	m.aiS().profileSave = &aiProfileSavePending{name: "other", profile: p}
	if m.onAIProfileSaveChoice("save") == nil || !strings.Contains(covToastText(&m), "Profile not saved") {
		t.Errorf("unwritable: %q", covToastText(&m))
	}
	m.aiS().profileSave = &aiProfileSavePending{name: "x", profile: p, fromForm: true}
	if m.onAIProfileSaveChoice("cancel") != nil || m.aiS().profileSave != nil {
		t.Error("cancel from the form did something")
	}
}

// Edit Profiles and Edit Model Slots make their files when missing.
func TestCovAIProfileFilesMadeWhenMissing(t *testing.T) {
	m := aiTestModel(t)
	path, _ := ai.ProfilesPath()
	_ = os.Remove(path)
	if m.editAIProfiles() == nil {
		t.Error("no hint toast")
	}
	if b, err := os.ReadFile(path); err != nil || len(b) == 0 {
		t.Errorf("profiles.json: %v", err)
	}
	cfg, _ := ai.ConfigPath()
	_ = os.Remove(cfg)
	m.editAIModelSlots()
	b, err := os.ReadFile(cfg)
	if err != nil || !strings.Contains(string(b), `"ai_models"`) {
		t.Errorf("config.json: %q %v", b, err)
	}
	if fi, _ := os.Stat(cfg); fi.Mode().Perm() != 0o600 {
		t.Errorf("config.json mode %v", fi.Mode().Perm())
	}
	// Saving another file is not a profiles change.
	if m.onAIProfilesSaved(filepath.Join(t.TempDir(), "x.json")) != nil {
		t.Error("an unrelated save did something")
	}
	_ = os.WriteFile(path, []byte(`{"profiles": [ {"name": "bad"`), 0o644)
	m.onAIProfilesSaved(path)
	if !strings.Contains(covToastText(&m), "Profiles not updated") {
		t.Errorf("broken file toast = %q", covToastText(&m))
	}
}
