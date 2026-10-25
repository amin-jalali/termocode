package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/profileform"
)

// formSend sends msg through the whole Update and feeds the form's own
// answer (SaveMsg, CancelMsg …) back once.
func formSend(m *Model, msg tea.Msg) {
	nm, cmd := m.Update(msg)
	*m = nm.(Model)
	for _, out := range formCmdMsgs(cmd) {
		switch out.(type) {
		case profileform.SaveMsg, profileform.CancelMsg, profileform.RemoveStepMsg, profileform.DeleteMsg, profileform.KeysMsg:
			nm, _ = m.Update(out)
			*m = nm.(Model)
		}
	}
}

// formCmdMsgs runs cmd (and the commands of a batch) and returns what
// they answered within a moment (timers are left alone).
func formCmdMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if b, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range b {
				out = append(out, formCmdMsgs(c)...)
			}
			return out
		}
		return []tea.Msg{msg}
	case <-time.After(30 * time.Millisecond):
		return nil
	}
}

func formKeys(m *Model, ks ...string) {
	for _, k := range ks {
		switch k {
		case "enter":
			formSend(m, tea.KeyMsg{Type: tea.KeyEnter})
		case "esc":
			formSend(m, tea.KeyMsg{Type: tea.KeyEsc})
		case "tab":
			formSend(m, tea.KeyMsg{Type: tea.KeyTab})
		case "right":
			formSend(m, tea.KeyMsg{Type: tea.KeyRight})
		case "down":
			formSend(m, tea.KeyMsg{Type: tea.KeyDown})
		default:
			formSend(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		}
	}
}

func formProfilesFile(t *testing.T, content string) string {
	t.Helper()
	path, _ := ai.ProfilesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const formSeed = `{"keep_me": {"x": 1}, "profiles": {
  "mine": {"title": "Mine", "future_key": true, "steps": [{"id": "a", "title": "A", "system": "p", "tools": {"preset": "read-only"}}]},
  "other": {"steps": [{"id": "o", "extra": 1}]}}}`

// Spec (AH-5): Edit a built-in profile makes a copy under a new name; the
// form opens on it, Save shows the preview card, and its Save writes the
// copy into profiles.json (other profiles and top-level keys stay), then
// switches to it. The result round-trips through ai.ParseProfiles.
func TestProfileFormBuiltinEditSavesCopy(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	path := formProfilesFile(t, formSeed)
	m.openAIProfileEditForm(ai.ProfileReview)
	if !m.profileFormOpen || m.profileForm.Name() != "my-review" || m.profileForm.Replace() {
		t.Fatalf("form: open=%v name=%q replace=%v", m.profileFormOpen, m.profileForm.Name(), m.profileForm.Replace())
	}
	formKeys(m, "enter") // edit the name: my-review → my-revie2
	formSend(m, tea.KeyMsg{Type: tea.KeyBackspace})
	formKeys(m, "2", "esc")
	want := m.profileForm.Name()
	if want == "my-review" {
		t.Fatal("the name was not edited")
	}
	before, _ := os.ReadFile(path)
	m.handleAIProfileFormMsg(profileform.SaveMsg{})
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileSave {
		t.Fatal("no preview card")
	}
	if now, _ := os.ReadFile(path); string(now) != string(before) {
		t.Fatal("the file changed before the preview's Save")
	}
	m.confirmOpen = false
	m.handleAIConfirm("save")

	b, _ := os.ReadFile(path)
	ps, err := ai.ParseProfiles(b, path, ai.ProfileSourceUser, m.ai.slotKnown, 1)
	if err != nil {
		t.Fatalf("saved file does not parse: %v\n%s", err, b)
	}
	byName := map[string]ai.Profile{}
	for _, p := range ps {
		byName[p.Name] = p
	}
	got, ok := byName[want]
	if !ok || len(byName) != 3 {
		t.Fatalf("profiles after save: %v\n%s", byName, b)
	}
	var builtin ai.Profile
	for _, p := range ai.BuiltinProfiles() {
		if p.Name == ai.ProfileReview {
			builtin = p
		}
	}
	if !reflect.DeepEqual(got.Steps, builtin.Steps) || got.Title != builtin.Title {
		t.Errorf("copy differs from the built-in:\n got %+v\nwant %+v", got, builtin)
	}
	var doc map[string]json.RawMessage
	_ = json.Unmarshal(b, &doc)
	if _, ok := doc["keep_me"]; !ok || !strings.Contains(string(b), `"future_key"`) || !strings.Contains(string(b), `"extra"`) {
		t.Errorf("unknown keys or other profiles lost:\n%s", b)
	}
	if m.profileFormOpen || m.ai.activeProfile().Name != want {
		t.Errorf("after save: form open %v, active %q", m.profileFormOpen, m.ai.activeProfile().Name)
	}
}

// Spec (AH-5): Edit on the user's own profile saves in place (same name,
// replaced), keeping the keys of that profile the form does not know.
func TestProfileFormEditInPlace(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	path := formProfilesFile(t, formSeed)
	m.ai.reloadProfiles()
	m.openAIProfileEditForm("mine")
	if !m.profileForm.Replace() || m.profileForm.Name() != "mine" {
		t.Fatalf("replace=%v name=%q", m.profileForm.Replace(), m.profileForm.Name())
	}
	formKeys(m, "down", "enter", "!", "esc") // Title: Mine!
	if !m.profileForm.Dirty() {
		t.Fatal("form not dirty after typing")
	}
	m.handleAIProfileFormMsg(profileform.SaveMsg{})
	m.confirmOpen = false
	m.handleAIConfirm("save")
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "Mine!") || !strings.Contains(string(b), `"future_key"`) || !strings.Contains(string(b), `"other"`) {
		t.Errorf("in-place save:\n%s", b)
	}
	if strings.Count(string(b), `"mine"`) != 1 {
		t.Errorf("profile duplicated:\n%s", b)
	}
	if m.ai.activeProfile().Name != "mine" {
		t.Errorf("active %q", m.ai.activeProfile().Name)
	}
}

// Spec (AH-5): Cancel, and Esc after edits, leave profiles.json byte for
// byte as it was; Esc after edits asks first with Keep editing first.
func TestProfileFormCancelWritesNothing(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	path := formProfilesFile(t, formSeed)
	want, _ := os.ReadFile(path)
	m.ai.reloadProfiles()
	m.openAIProfileEditForm("mine")
	formKeys(m, "enter", "z", "esc", "esc") // edit the name, stop editing, Esc
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileFormDiscard {
		t.Fatal("Esc after edits did not ask")
	}
	if m.confirm.SafeID() != "keep" {
		t.Errorf("safe button %q, want keep", m.confirm.SafeID())
	}
	m.confirmOpen = false
	m.handleAIConfirm("keep")
	if !m.profileFormOpen {
		t.Fatal("Keep editing closed the form")
	}
	formKeys(m, "esc")
	m.confirmOpen = false
	m.handleAIConfirm("discard")
	if m.profileFormOpen {
		t.Error("Discard left the form open")
	}
	if got, _ := os.ReadFile(path); string(got) != string(want) {
		t.Errorf("file changed:\n%s", got)
	}
	// No edit: Esc closes at once.
	m.openAIProfileEditForm("mine")
	formKeys(m, "esc")
	if m.profileFormOpen || m.confirmOpen {
		t.Error("Esc without edits must close at once")
	}
}

// Spec (AH-5): removing a step asks with the safe button first and
// focused; only Remove takes the step out. Deleting the profile asks the
// same way and removes only that profile.
func TestProfileFormRemoveAndDeleteAskFirst(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	path := formProfilesFile(t, formSeed)
	m.ai.reloadProfiles()
	m.openAIProfileEditForm(ai.ProfileReview)
	n := len(m.profileForm.Profile().Steps)
	formKeys(m, "x")
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileFormStep || m.confirm.SafeID() != "keep" {
		t.Fatalf("remove question: open=%v kind=%v safe=%q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	m.confirmOpen = false
	m.handleAIConfirm("keep")
	if len(m.profileForm.Profile().Steps) != n {
		t.Fatal("Keep removed the step")
	}
	formKeys(m, "x")
	m.confirmOpen = false
	m.handleAIConfirm("remove")
	if len(m.profileForm.Profile().Steps) != n-1 {
		t.Fatal("Remove did not remove the step")
	}

	m.openAIProfileEditForm("mine")
	m.handleAIProfileFormMsg(profileform.DeleteMsg{})
	if m.confirmKind != confirmKindAIProfileFormDelete || m.confirm.SafeID() != "keep" {
		t.Fatalf("delete question: kind=%v safe=%q", m.confirmKind, m.confirm.SafeID())
	}
	m.confirmOpen = false
	m.handleAIConfirm("keep")
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"mine"`) {
		t.Fatal("Keep deleted the profile")
	}
	m.handleAIProfileFormMsg(profileform.DeleteMsg{})
	m.confirmOpen = false
	m.handleAIConfirm("delete")
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), `"mine"`) || !strings.Contains(string(b), `"other"`) || !strings.Contains(string(b), "keep_me") {
		t.Errorf("after delete:\n%s", b)
	}
	if m.profileFormOpen {
		t.Error("form still open after the profile was deleted")
	}
}

// Spec (AH-5): a name that is taken or built in is an inline error and
// Save is refused (no preview card).
func TestProfileFormNameErrorsBlockSave(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	formProfilesFile(t, formSeed)
	m.ai.reloadProfiles()
	m.profileForm = profileform.New("mine", ai.BuiltinProfiles()[1], profileform.Config{
		Title: "New profile", Slots: m.aiFormSlots(), ParallelMax: 1, Taken: map[string]bool{"mine": true}})
	m.profileForm.SetSize(m.w, m.h)
	m.profileFormOpen = true
	out := ansi.Strip(m.profileForm.Box())
	if !strings.Contains(out, `"mine" exists already`) {
		t.Errorf("taken name not shown:\n%s", out)
	}
	for _, b := range m.profileForm.ButtonRects() {
		if b.Title == "Save…" {
			formSend(m, tea.MouseMsg{X: b.X + 2, Y: b.Y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		}
	}
	if m.confirmOpen {
		t.Error("Save with a taken name opened the preview card")
	}
}

// Spec (AH-5): the form never shows a key: a slot shows a mark, and a
// key planted in the slot's config is nowhere in the frame, the preview
// or the written file.
func TestProfileFormNeverShowsKeys(t *testing.T) {
	const secret = "sk-FORMSECRET-123456"
	m, _ := aiProfileTestModel(t)
	m.ai.cfg.APIKey = secret
	m.ai.slotFake = map[string]aiSlot{
		ai.SlotMain: {p: fakeAIProvider{reply: "ok"}, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "m", APIKey: secret}},
		"gpt":       {p: fakeAIProvider{reply: "ok"}, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "g"}},
	}
	m.ai.slotKnown = map[string]bool{ai.SlotMain: true, ai.SlotQuick: true, ai.SlotLocal: true, "gpt": true}
	path := formProfilesFile(t, `{"profiles": {}}`)
	m.openAIProfileNewForm()
	frames := []string{ansi.Strip(m.View())}
	m.handleAIProfileFormMsg(profileform.SaveMsg{})
	frames = append(frames, ansi.Strip(m.View()))
	m.confirmOpen = false
	m.handleAIConfirm("save")
	b, _ := os.ReadFile(path)
	frames = append(frames, string(b))
	for i, f := range frames {
		if strings.Contains(f, secret) {
			t.Errorf("frame %d shows the key", i)
		}
	}
	if !strings.Contains(frames[0], "key") {
		t.Errorf("no key mark in the form:\n%s", frames[0])
	}
}

// Spec (AH-5): at 80x24 the form and the cards on it never overflow.
func TestProfileFormFits80x24(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.w, m.h = 80, 24
	m.openAIProfileEditForm(ai.ProfileReview)
	check := func(what string) {
		t.Helper()
		lines := strings.Split(ansi.Strip(m.profileForm.Box()), "\n")
		if len(lines) > 24 {
			t.Errorf("%s: %d rows", what, len(lines))
		}
		for _, l := range lines {
			if runewidth.StringWidth(l) > 80 {
				t.Errorf("%s: row of %d cells: %q", what, runewidth.StringWidth(l), l)
			}
		}
	}
	check("opened")
	for i := 0; i < 30; i++ {
		formKeys(m, "tab")
		check("tab")
	}
}

// Spec (AH-5): a click on Cancel of a clean form closes it; Edit a
// profile… lists every profile and the palette entry opens that list.
func TestProfileFormMouseAndRoutes(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.dispatchAIPalette("ai-profile-form")
	if !m.pickerOpen || m.pickerKind != pickerKindAIProfileEdit {
		t.Fatal("the palette entry did not open the edit list")
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "review"})
	if !m.profileFormOpen {
		t.Fatal("picking a profile did not open the form")
	}
	var cancel profileform.ButtonRect
	for _, b := range m.profileForm.ButtonRects() {
		if b.Title == "Cancel" {
			cancel = b
		}
	}
	if cancel.W == 0 {
		t.Fatal("no Cancel button")
	}
	formSend(m, tea.MouseMsg{X: cancel.X + 2, Y: cancel.Y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.profileFormOpen {
		t.Error("click on Cancel did not close the clean form")
	}
}
