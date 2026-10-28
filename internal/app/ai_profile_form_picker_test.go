package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/addai"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/profileform"
)

// pickerFormModel is a model with the step form open on one step, no AI
// connected, and fake CLI checks (no real claude / codex runs).
func pickerFormModel(t *testing.T, p ai.Profile, states map[string]clistep.Status) *Model {
	t.Helper()
	m := addAITestModel(t)
	oldCheck, oldInst := aiCLICheck, aiCLIInstalled
	t.Cleanup(func() { aiCLICheck, aiCLIInstalled = oldCheck, oldInst })
	aiCLICheck = func(_ context.Context, cli string, _ []string) clistep.Status { return states[cli] }
	aiCLIInstalled = func(string) bool { return false }
	m.aiS().reload()
	cmd := m.openAIProfileForm("mine", p, false)
	if cmd == nil {
		t.Fatal("opening the form checks the CLIs")
	}
	m.handleAIMsg(cmd())
	return m
}

// pickerRun runs cmd and what it gives back, through the Add AI card's
// handler or the AI handler (the CLI check).
func pickerRun(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		out := cmd()
		if out == nil {
			return
		}
		next, ok := m.handleAddAIMsg(out)
		if !ok {
			next, ok = m.handleAIMsg(out)
		}
		if !ok {
			t.Fatalf("%T is not handled", out)
		}
		cmd = next
	}
}

// formEmit sends a key to the form and runs what it asks the app.
func formEmit(t *testing.T, m *Model, k tea.KeyMsg) {
	t.Helper()
	nm, cmd := m.Update(k)
	*m = nm.(Model)
	for _, out := range formCmdMsgs(cmd) {
		if msg, ok := out.(profileform.AddAIMsg); ok {
			c, handled := m.handleAIProfileFormMsg(msg)
			if !handled {
				t.Fatalf("%T is not handled", msg)
			}
			pickerRun(t, m, c)
		}
	}
}

// Spec (AIP-6 accept): from the picker a user adds Ollama and comes back
// to the same step with the picker open; the form keeps its changes, and
// the new AI's models are in the list.
func TestProfileFormAddAnotherAIRoundTrip(t *testing.T) {
	m := pickerFormModel(t, ai.Profile{Steps: []ai.ProfileStep{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}}}, nil)
	f := &fakeAddAIList{}
	f.install(t, addAIServer(t))
	m.profileForm.FocusModel(1)
	formEmit(t, m, tea.KeyMsg{Type: tea.KeyRight}) // a change: Automatic: quick
	if !m.profileForm.Dirty() {
		t.Fatal("no change")
	}
	formEmit(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if step, open := m.profileForm.PickerOpen(); !open || step != 1 {
		t.Fatalf("picker %d %v", step, open)
	}
	m.profileForm.SetPickerQuery("zzz") // nothing matches: the cursor is on + Add another AI
	formEmit(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.addAIOpen || !m.profileFormOpen || m.addAI.Step() != addai.StepPick {
		t.Fatalf("add card %v form %v step %v", m.addAIOpen, m.profileFormOpen, m.addAI.Step())
	}
	addAIKeyT(t, m, tea.KeyDown)
	addAIKeyT(t, m, tea.KeyDown) // Ollama
	addAIKeyT(t, m, tea.KeyEnter)
	if s := addAICardText(m); m.addAI.Step() != addai.StepDone || !strings.Contains(s, "Back to the profile") {
		t.Fatalf("done card:\n%s", s)
	}
	nm, cmd := m.routeToAddAI(connectcard.ButtonMsg{ID: "done"})
	*m = nm
	pickerRun(t, m, cmd)
	if m.addAIOpen || !m.profileFormOpen {
		t.Fatalf("card %v form %v", m.addAIOpen, m.profileFormOpen)
	}
	if step, open := m.profileForm.PickerOpen(); !open || step != 1 {
		t.Fatalf("back on step 2 with the picker open: %d %v", step, open)
	}
	out := ansi.Strip(m.profileForm.Box())
	if !strings.Contains(out, "OLLAMA") || !strings.Contains(out, "llama3.2:3b") || !strings.Contains(out, "local · free") {
		t.Fatalf("the new AI's models:\n%s", out)
	}
	if got := m.profileForm.Profile().Steps[1].Category; got != "quick" || !m.profileForm.Dirty() {
		t.Errorf("the form lost its change: %q", got)
	}
	m.profileForm.SetPickerQuery("llama3.2")
	formEmit(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if st := m.profileForm.Profile().Steps[1]; st.Provider != ai.KindOllama || st.Model != "llama3.2:3b" || st.Category != "" {
		t.Errorf("step %+v", st)
	}
	if !m.profileForm.Valid() {
		t.Errorf("errors %v", m.profileForm.Errors())
	}
}

// Spec (AIP-6 accept): a user adds a step, picks gpt-5-mini · OpenAI
// from the picker, and the saved profile step has provider + model.
func TestProfileFormPickSavesProviderModel(t *testing.T) {
	m := pickerFormModel(t, ai.Profile{Steps: []ai.ProfileStep{{ID: "a", Title: "A"}}}, nil)
	pp, _ := ai.ProvidersPath()
	if err := ai.SaveProvider(pp, ai.ProviderEntry{Kind: ai.KindOpenAI, Method: ai.MethodKey}); err != nil {
		t.Fatal(err)
	}
	cache := ai.ModelsCache{}
	cache.Put(ai.KindOpenAI, []ai.ModelInfo{{ID: "gpt-5"}, {ID: "gpt-5-mini"}}, time.Now())
	if err := ai.SaveModelsCache(cache); err != nil {
		t.Fatal(err)
	}
	m.aiS().reload()
	pickerRun(t, m, m.openAIProfileForm("mine", ai.Profile{Steps: []ai.ProfileStep{{ID: "a", Title: "A"}}}, false))
	path := formProfilesFile(t, `{"profiles": {}}`)

	formEmit(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}) // Add step: its picker opens
	if step, open := m.profileForm.PickerOpen(); !open || step != 1 {
		t.Fatalf("picker %d %v", step, open)
	}
	out := ansi.Strip(m.profileForm.Box())
	if !strings.Contains(out, "OPENAI") || !strings.Contains(out, "API · pay per use") || !strings.Contains(out, "gpt-5-mini") {
		t.Fatalf("picker:\n%s", out)
	}
	for _, r := range "5-mini" {
		formEmit(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	formEmit(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if out := ansi.Strip(m.profileForm.Box()); !strings.Contains(out, "gpt-5-mini · OpenAI") {
		t.Fatalf("form:\n%s", out)
	}
	m.handleAIProfileFormMsg(profileform.SaveMsg{})
	if !m.confirmOpen {
		t.Fatalf("no preview card (errors %v)", m.profileForm.Errors())
	}
	m.confirmOpen = false
	m.handleAIConfirm("save")
	b, _ := os.ReadFile(path)
	ps, err := ai.ParseProfiles(b, path, ai.ProfileSourceUser, m.ai.slotKnown, 1)
	if err != nil || len(ps) != 1 || len(ps[0].Steps) != 2 {
		t.Fatalf("saved %v %v\n%s", ps, err, b)
	}
	if st := ps[0].Steps[1]; st.Provider != ai.KindOpenAI || st.Model != "gpt-5-mini" || st.Slot != "" || st.Category != "" {
		t.Errorf("saved step %+v\n%s", st, b)
	}
}

// Fix on a step whose AI is not connected opens the Add AI card at that
// AI; Esc there goes back to the step (no picker: it did not come from it).
func TestProfileFormFixOpensAddAI(t *testing.T) {
	p := ai.Profile{Steps: []ai.ProfileStep{{ID: "a", Title: "A", Provider: ai.KindAnthropic, Model: ai.ModelClaudeSonnet}}}
	m := pickerFormModel(t, p, nil)
	m.profileForm.FocusFix(0)
	if out := ansi.Strip(m.profileForm.Box()); !strings.Contains(out, "needs Anthropic") || !strings.Contains(out, "Fix") {
		t.Fatalf("form:\n%s", out)
	}
	formEmit(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.addAIOpen || m.addAI.Kind() != ai.KindAnthropic || m.addAI.Step() != addai.StepMethod {
		t.Fatalf("card %v kind %q step %v", m.addAIOpen, m.addAI.Kind(), m.addAI.Step())
	}
	addAIKeyT(t, m, tea.KeyEsc)
	if m.addAIOpen || !m.profileFormOpen {
		t.Fatalf("card %v form %v", m.addAIOpen, m.profileFormOpen)
	}
	if _, open := m.profileForm.PickerOpen(); open {
		t.Error("Fix does not come back to a picker")
	}
}

// A CLI step that is missing or signed out shows Fix (never Install /
// Log in, never a terminal tab); Fix starts that AI's sign-in in the
// Add AI card, and a sign-in that ends there makes the step ready.
func TestProfileFormCLIStepFix(t *testing.T) {
	m := pickerFormModel(t, cliProfile(), map[string]clistep.Status{ai.CLIClaude: clistep.StatusMissing, ai.CLICodex: clistep.StatusLoggedOut})
	m.profileForm.FocusStep(0)
	out := ansi.Strip(m.profileForm.Box())
	if !strings.Contains(out, "Claude Code is not installed") || !strings.Contains(out, "Fix") || strings.Contains(out, "Log in") || strings.Contains(out, "Install ") {
		t.Fatalf("form:\n%s", out)
	}
	m.profileForm.FocusFix(0)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = nm.(Model)
	var fix []profileform.AddAIMsg
	for _, out := range formCmdMsgs(cmd) {
		if msg, ok := out.(profileform.AddAIMsg); ok {
			fix = append(fix, msg)
		}
	}
	if len(fix) != 1 || fix[0] != (profileform.AddAIMsg{Step: 0, Kind: ai.KindAnthropic, Method: ai.MethodCLI}) {
		t.Fatalf("msgs %#v", fix)
	}
	c, _ := m.handleAIProfileFormMsg(fix[0])
	_ = c // the card asks first: nothing runs before Install and sign in
	if !m.addAIOpen || m.addAI.Step() != addai.StepSignIn || m.addAI.Kind() != ai.KindAnthropic {
		t.Fatalf("card %v step %v kind %q", m.addAIOpen, m.addAI.Step(), m.addAI.Kind())
	}
}
