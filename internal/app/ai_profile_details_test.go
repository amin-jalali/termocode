package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec: the profile picker ends with a "Show details" row that opens
// AI: Show Profile for the active profile.
func TestAIProfilePickerShowDetailsRow(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.openAIProfilePicker()
	m.handleAIPickerSelect(aiProfileDetailsID)
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileShow {
		t.Fatal("the row did not open the profile details")
	}
	if m.ai.activeProfile().Name != ai.ProfileDefault {
		t.Error("the row switched the profile")
	}
}
