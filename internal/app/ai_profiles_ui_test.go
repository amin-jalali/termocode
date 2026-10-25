package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/profileform"
	"github.com/amin-jalali/termocode/internal/theme"
)

func aiToastText(m *Model) string {
	var b strings.Builder
	for _, c := range m.toast.Cards(100) {
		for _, l := range c.Lines {
			b.WriteString(ansi.Strip(l) + "\n")
		}
	}
	return b.String()
}

// aiProfileTestModel is a configured chat with a project profile
// "house" in .termocode/ai-profiles.json.
func aiProfileTestModel(t *testing.T) (*Model, string) {
	t.Helper()
	m, dir := aiIDETestModel(t, fakeAIProvider{reply: "ok"})
	if err := os.MkdirAll(filepath.Join(dir, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	proj := `{"profiles": {"house": {"title": "House review", "steps": [
		{"id": "rev", "title": "Reviewer", "system": "Follow the house style.\nSecond line.", "tools": {"preset": "read-only"}}]}}}`
	if err := os.WriteFile(filepath.Join(dir, ".termocode", "ai-profiles.json"), []byte(proj), 0o644); err != nil {
		t.Fatal(err)
	}
	m.ai.loadProfiles(ai.Credentials{})
	m.ai.slotFake = map[string]aiSlot{ai.SlotMain: {p: fakeAIProvider{reply: "ok"}, cfg: ai.Config{Kind: ai.KindOllama, Model: "fake-1"}}}
	m.actionsOpen, m.actionsPinned = true, true
	return m, dir
}

func TestAIProfileBufferSaved(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	path, _ := ai.ProfilesPath()
	if c := m.onAIProfilesSaved(filepath.Join(filepath.Dir(path), "other.json")); c != nil || m.confirmOpen || aiToastText(m) != "" {
		t.Fatal("saving another file had an effect")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	initial := `{"profiles":{"zeta":{"steps":[{"id":"one","system":"same prompt","tools":{"preset":"all"}}]}}}`
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	m.ai.reloadProfiles()
	good := `{"profiles":{"zeta":{"steps":[{"id":"one","system":"same prompt","tools":{"preset":"read-only"}}]}}}`
	if err := os.WriteFile(path, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	m.onAIProfilesSaved(path)
	if !strings.Contains(aiToastText(m), "Profiles updated: zeta") {
		t.Fatalf("toast: %q", aiToastText(m))
	}
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileSave {
		t.Fatal("updated profile card missing")
	}
	if c := m.onAIProfilesSaved(path); c != nil {
		t.Fatal("unexpected command")
	}
	if err := os.WriteFile(path, []byte(`{"profiles":{`), 0o644); err != nil {
		t.Fatal(err)
	}
	m.onAIProfilesSaved(path)
	parseToast := strings.Join(strings.Fields(aiToastText(m)), " ")
	if !strings.Contains(parseToast, "profiles.json, line 1") {
		t.Fatalf("parse error toast: %q", aiToastText(m))
	}
}

// Spec (§2.1): the chat header shows "✦ <profile> ▾" after the title; a
// click on it opens the profile picker.
func TestAIProfileChipInHeader(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	w := m.actionsColumnWidth()
	head := ansi.Strip(m.renderActionsHeader(w))
	chip := aiSpark() + " default " + aiFoldGlyph(true)
	i := strings.Index(head, chip)
	if i < 0 || !strings.Contains(head[:i], "AI CHAT") {
		t.Fatalf("no profile chip after the title: %q", head)
	}
	m.ai.profile = ai.ProfileReview
	head = ansi.Strip(m.renderActionsHeader(w))
	i = strings.Index(head, aiSpark()+" review "+aiFoldGlyph(true))
	if i < 0 {
		t.Fatalf("chip does not follow the profile: %q", head)
	}
	x := m.w - w + ansi.StringWidth(head[:i]) + 2
	if id := m.hitActionsButton(x, 0); id != "profile" {
		t.Fatalf("hit at chip = %q", id)
	}
	if id := m.aiPanelTargetAt(x, 0); id != "ai:profile" {
		t.Fatalf("target at chip = %q", id)
	}
	m.handleAIPanelMouse(tea.MouseMsg{X: x, Y: 0, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.pickerOpen || m.pickerKind != pickerKindAIProfile {
		t.Fatal("a click on the chip did not open the profile picker")
	}
	if it, ok := m.picker.Current(); !ok || it.ID != ai.ProfileReview {
		t.Errorf("picker cursor on %q, want the active profile", it.ID)
	}
	// The chip is the AI accent while a run is active, TextSecondary
	// otherwise.
	if tok := m.aiProfileChipTok(); tok != theme.TextSecondary {
		t.Errorf("idle chip colour %v", tok)
	}
	m.ai.run = &agentrun.Handle{}
	if tok := m.aiProfileChipTok(); tok != theme.AIAccent {
		t.Errorf("chip colour %v while a run is active", tok)
	}
	// The memo keys on the profile: a switch shows at once.
	m.ai.run = nil
	edge := func(s string) string { return s }
	before := m.actionsHeaderEdged(w, edge)
	m.ai.profile = ai.ProfileFast
	if after := m.actionsHeaderEdged(w, edge); after == before || !strings.Contains(ansi.Strip(after), " fast ") {
		t.Errorf("memo kept the old chip: %q", ansi.Strip(after))
	}
	// A narrow header drops the chip, never the buttons.
	if head := ansi.Strip(m.renderActionsHeader(24)); strings.Contains(head, "fast") {
		t.Errorf("chip in a 24-cell header: %q", head)
	}
}

// Spec (F1): Alt+Shift+P cycles the profiles with a toast; the choice is
// kept per workspace in the session file and restored.
func TestAIProfileCycleKey(t *testing.T) {
	km := keymap.Default()
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'P'}, Alt: true},
	} {
		if a := km.Match(k); a != keymap.ActionAINextProfile {
			t.Fatalf("%q → %v, want ActionAINextProfile", k.String(), a)
		}
	}
	if aiActionForKey(keymap.ActionAINextProfile) != "ai-profile-next" {
		t.Fatal("key not mapped to ai-profile-next")
	}
	m, _ := aiProfileTestModel(t)
	names := m.ai.profiles.Names()
	if names[0] != ai.ProfileDefault || names[1] != ai.ProfileReview {
		t.Fatalf("names %v", names)
	}
	m.dispatchAIPalette("ai-profile-next")
	if m.ai.activeProfile().Name != ai.ProfileReview {
		t.Fatalf("after next: %q", m.ai.activeProfile().Name)
	}
	if txt := aiToastText(m); !strings.Contains(txt, "Profile: review") {
		t.Errorf("toast %q", txt)
	}
	if got := loadSession().AIProfile; got != ai.ProfileReview {
		t.Errorf("session profile %q", got)
	}
	s := newAIState()
	if s.activeProfile().Name != ai.ProfileReview {
		t.Errorf("restored profile %q", s.activeProfile().Name)
	}
	// Status badge: the profile shows when it is not default.
	if label, _ := m.aiStatus(); !strings.Contains(label, "review") {
		t.Errorf("badge %q has no profile", label)
	}
	// The cycle wraps; an untrusted project profile asks first.
	m.ai.profile = names[len(names)-1]
	if names[len(names)-1] == "house" {
		m.ai.profile = names[len(names)-2]
		m.dispatchAIPalette("ai-profile-next")
		if !m.confirmOpen || m.confirmKind != confirmKindAIProfileTrust {
			t.Fatal("untrusted project profile: no trust card")
		}
		if m.ai.activeProfile().Name == "house" {
			t.Fatal("switched before trust")
		}
		m.closeConfirmForTest()
		m.ai.profile = "house"
	}
	m.dispatchAIPalette("ai-profile-next")
	if m.ai.activeProfile().Name != ai.ProfileDefault {
		t.Errorf("cycle did not wrap: %q", m.ai.activeProfile().Name)
	}
	if label, _ := m.aiStatus(); strings.Contains(label, "default") {
		t.Errorf("badge %q names the default profile", label)
	}
}

func (m *Model) closeConfirmForTest() {
	m.confirmOpen = false
	m.ai.profTrust = nil
}

// Spec (F1): AI: Switch Profile… lists the profiles; picking one
// switches.
func TestAIProfileSwitchPicker(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.dispatchAIPalette("ai-profile-switch")
	if !m.pickerOpen || m.pickerKind != pickerKindAIProfile {
		t.Fatal("no picker")
	}
	m.handleAIPickerSelect(ai.ProfileFast)
	if m.ai.activeProfile().Name != ai.ProfileFast {
		t.Errorf("picked fast, active %q", m.ai.activeProfile().Name)
	}
}

// Spec (F4): the first use of a project profile shows a trust card with
// its steps and the first line of each prompt; Use runs it and is
// remembered for this workspace; Not in this project goes back to
// default and keeps the message in the input.
func TestAIProjectProfileTrust(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.ai.profile = "house"
	cmd := m.sendAIChat("check my code")
	_ = cmd
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileTrust {
		t.Fatal("no trust card before the first use")
	}
	if len(m.ai.conv.Messages) != 0 {
		t.Fatal("message sent before trust")
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"house", "Reviewer", "Follow the house style.", "Use", "Not in this project"} {
		if !strings.Contains(box, want) {
			t.Errorf("trust card misses %q:\n%s", want, box)
		}
	}
	if strings.Contains(box, "Second line.") {
		t.Error("trust card shows more than the first prompt line")
	}
	driveAIChat(t, m, m.answerAIProfileTrustForTest("use"), nil)
	if len(m.ai.conv.Messages) != 2 || m.ai.conv.Messages[0].Text != "check my code" {
		t.Fatalf("message not sent after Use: %+v", m.ai.conv.Messages)
	}
	// Remembered: no card the second time.
	driveAIChat(t, m, m.sendAIChat("again"), nil)
	if m.confirmOpen {
		t.Fatal("asked again after Use")
	}

	// A changed profile asks again; Not in this project → default.
	m2, dir := aiProfileTestModel(t)
	_ = dir
	m2.ai.profile = "house"
	m2.sendAIChat("hello")
	m2.answerAIProfileTrustForTest("deny")
	if m2.ai.activeProfile().Name != ai.ProfileDefault {
		t.Errorf("after Not in this project: %q", m2.ai.activeProfile().Name)
	}
	if string(m2.ai.input) != "hello" || len(m2.ai.conv.Messages) != 0 {
		t.Errorf("message lost: input %q, %d messages", string(m2.ai.input), len(m2.ai.conv.Messages))
	}
	if sess := loadSession(); sess.AIProfilesRefused["house"] == "" {
		t.Errorf("refusal not stored: %+v", sess.AIProfilesRefused)
	}
	// A refused project profile is left out of the cycle.
	for range m2.ai.profiles.Names() {
		m2.dispatchAIPalette("ai-profile-next")
		if m2.confirmOpen {
			t.Fatal("cycle stopped at a refused project profile")
		}
	}
	// AI: Project Profiles Trust… lists it and can trust it again.
	m2.dispatchAIPalette("ai-profile-trust")
	if !m2.pickerOpen || m2.pickerKind != pickerKindAIProfileTrust {
		t.Fatal("no trust picker")
	}
	m2.handleAIPickerSelect("house")
	if !m2.confirmOpen || m2.confirmKind != confirmKindAIProfileTrust {
		t.Fatal("trust picker did not open the card")
	}
	m2.answerAIProfileTrustForTest("use")
	if sess := loadSession(); sess.AIProfilesTrusted["house"] == "" || sess.AIProfilesRefused["house"] != "" {
		t.Errorf("trust not stored: %+v / %+v", sess.AIProfilesTrusted, sess.AIProfilesRefused)
	}
}

func (m *Model) answerAIProfileTrustForTest(id string) tea.Cmd {
	m.confirmOpen = false
	cmd, _ := m.handleAIConfirm(id)
	return cmd
}

// Spec (F3): AI: Edit Profiles opens profiles.json, created with a
// working example when missing; AI: New Profile from Current… saves the
// active profile under a new name and switches to it.
func TestAIProfileEditAndNew(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	path, _ := ai.ProfilesPath()
	m.dispatchAIPalette("ai-profile-edit")
	b, err := os.ReadFile(path)
	if err != nil || string(b) != ai.ExampleProfilesJSON {
		t.Fatalf("profiles.json not created with the example: %v %q", err, b)
	}
	if err := os.WriteFile(path, []byte(`{"profiles": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m.dispatchAIPalette("ai-profile-edit")
	if b, _ := os.ReadFile(path); string(b) != `{"profiles": {}}` {
		t.Fatal("an existing profiles.json was overwritten")
	}

	m.ai.profile = ai.ProfileReview
	m.dispatchAIPalette("ai-profile-new")
	if !m.profileFormOpen || m.profileForm.Name() != "my-review" {
		t.Fatal("no step form for the new profile")
	}
	m.handleAIProfileFormMsg(profileform.SaveMsg{})
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileSave {
		t.Fatal("new profile did not show its preview")
	}
	if b, _ := os.ReadFile(path); string(b) != `{"profiles": {}}` {
		t.Fatalf("profile file changed before confirmation: %q", b)
	}
	m.confirmOpen = false
	m.handleAIConfirm("save")
	if m.ai.activeProfile().Name != "my-review" || m.profileFormOpen {
		t.Fatalf("active %q after New Profile (form open %v)", m.ai.activeProfile().Name, m.profileFormOpen)
	}
	if p := m.ai.activeProfile(); len(p.Steps) != 2 || p.Source != ai.ProfileSourceUser {
		t.Errorf("saved profile %+v", p)
	}

	// AI: Edit Model Slots opens the AI config.json.
	m.dispatchAIPalette("ai-models-edit")
	cfg, _ := ai.ConfigPath()
	if _, err := os.Stat(cfg); err != nil {
		t.Errorf("config.json not there: %v", err)
	}
}

func TestAIProfileSaveCancelAndEsc(t *testing.T) {
	for _, esc := range []bool{false, true} {
		m, _ := aiProfileTestModel(t)
		path, _ := ai.ProfilesPath()
		_ = os.Remove(path)
		m.openAIProfileNewForm()
		m.handleAIProfileFormMsg(profileform.SaveMsg{})
		m.confirmOpen = false
		if esc {
			m.onAIConfirmClosed()
		} else {
			m.handleAIConfirm("cancel")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("cancel wrote profile: %v", err)
		}
		if !m.profileFormOpen {
			t.Error("cancelling the preview must bring the form back")
		}
	}
}

// Spec (§2.1): every profile id is in the palette; the AI may never run
// them, except the switch, which asks first (class settings).
func TestAIProfilePalettePolicies(t *testing.T) {
	ids := map[string]bool{}
	for _, e := range aiPaletteItems() {
		ids[e.id] = true
	}
	for _, id := range []string{"ai-profile-switch", "ai-profile-next", "ai-profile-edit", "ai-profile-new", "ai-profile-trust", "ai-models-edit"} {
		if !ids[id] {
			t.Errorf("%s not in the palette", id)
		}
		p, ok := aiActionPolicyFor(id)
		switch {
		case !ok:
			t.Errorf("%s has no policy", id)
		case id == "ai-profile-switch":
			if p.Gate != aiGateConfirm || p.Class != ai.ClassSettings {
				t.Errorf("%s policy %+v", id, p)
			}
		case p.Gate != aiGateNever:
			t.Errorf("%s policy %+v, want never", id, p)
		}
	}
}
