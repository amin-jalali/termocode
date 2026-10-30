package app

import (
	"context"
	"strings"
	"testing"
)

// Every AI palette command (ai_state.go dispatchAIPalette) is handled:
// with a provider and without an editor none of them falls through or
// crashes; a stop with an action running says it stopped.
func TestCovAIPaletteEveryCommandHandled(t *testing.T) {
	m := aiTestModel(t)
	t.Chdir(t.TempDir())
	m.problems = newProblemsPanel()
	m.output = newOutputStore()
	m.aiS().provider = covScriptedProvider{}
	n := 0
	for _, it := range paletteItems() {
		if !strings.HasPrefix(it.ID, "ai-") {
			continue
		}
		n++
		_, ok := m.dispatchAIPalette(it.ID)
		if !ok {
			_, ok = m.dispatchItermocPalette(it.ID) // ai-add, ai-model-slot-add: the Itermoc view's
		}
		if !ok {
			t.Errorf("%s is not handled", it.ID)
		}
		m.pickerOpen, m.promptOpen, m.confirmOpen, m.connectOpen, m.addAIOpen, m.profileFormOpen, m.reviewOpen = false, false, false, false, false, false, false
	}
	if n < 40 {
		t.Errorf("only %d AI palette commands", n)
	}
	if _, ok := m.dispatchAIPalette("ai-no-such-command"); ok {
		t.Error("an unknown id was handled")
	}
	s := m.aiS()
	_, cancel := context.WithCancel(context.Background())
	s.actionCancel, s.actionBusy = cancel, "edit"
	if m.aiCancelAll() == nil || s.actionBusy != "" || !strings.Contains(covToastText(&m), "AI stopped") {
		t.Errorf("stop: busy %q toast %q", s.actionBusy, covToastText(&m))
	}
	if m.aiToastErr("x", nil) != nil {
		t.Error("a nil error made a toast")
	}
	s.provider = nil
	m.onAIBadgeClick()
	if !m.pickerOpen || m.pickerKind != pickerKindAIProvider {
		t.Error("the badge without a provider did not open Configure")
	}
	m.pickerOpen = false
	s.provider = covScriptedProvider{}
	before := m.actionsPanelVisible()
	m.onAIBadgeClick()
	if m.actionsPanelVisible() == before {
		t.Error("the badge did not toggle the chat")
	}
}
