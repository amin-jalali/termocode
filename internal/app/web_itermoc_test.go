package app

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webItermocKey is a fake key one model slot holds; it must never reach
// the page.
var webItermocKey = "sk-" + "test-itermoc-0123456789"

// webItermocRig is the profile test model (built-in and user profiles,
// and a project profile "house" nobody trusted) with three model slots:
// a local AI (no key needed), one with a key and one without. The hub
// has one controller connection.
func webItermocRig(t *testing.T) *webFilesRig {
	t.Helper()
	m, dir := aiProfileTestModel(t)
	m.ai.slotFake[ai.SlotQuick] = aiSlot{p: fakeAIProvider{reply: "ok"}, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-x", APIKey: webItermocKey}}
	m.ai.slotFake["cheap"] = aiSlot{p: fakeAIProvider{reply: "ok"}, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-y"}}
	h := newWebHub()
	t.Cleanup(h.stop)
	m.web = h
	r := &webFilesRig{t: t, m: m, h: h, root: dir, kept: map[*webConn][]map[string]any{}}
	r.c = r.conn("c-1", webproto.RoleController)
	return r
}

// Spec (WEBP-ITERMOC): itermoc.state shows the laptop's Itermoc view:
// its profiles (where each comes from, whether the laptop trusts it,
// which is in use) and its model slots, each saying only whether its key
// is there. The key itself never leaves the laptop.
func TestWebItermocStateShowsTheSetupWithoutKeys(t *testing.T) {
	r := webItermocRig(t)
	f := r.call(webproto.OpItermoc, &webproto.EmptyArgs{})
	if strings.Contains(fmt.Sprint(f), webItermocKey) || strings.Contains(fmt.Sprint(f), "sk-"+"test") {
		t.Fatalf("a key left the laptop: %v", f)
	}
	var st webproto.Itermoc
	webResData(t, f, &st)
	prof := map[string]webproto.ItermocProfile{}
	for _, p := range st.Profiles {
		prof[p.Name] = p
	}
	if p := prof[ai.ProfileDefault]; !p.Active || p.Source != ai.ProfileSourceBuiltin || p.Trust != aiTrustUse || p.Steps == 0 {
		t.Errorf("default profile: %+v", p)
	}
	if p := prof["house"]; p.Active || p.Source != ai.ProfileSourceProject || p.Trust != aiTrustAsk || p.Title != "House review" {
		t.Errorf("house profile: %+v", p)
	}
	slots := map[string]webproto.ItermocSlot{}
	for _, s := range st.Slots {
		slots[s.Name] = s
	}
	if s := slots[ai.SlotMain]; s.HasKey != "none" || s.Provider != ai.KindOllama || s.Model != "fake-1" {
		t.Errorf("main slot: %+v", s)
	}
	if s := slots[ai.SlotQuick]; s.HasKey != "ok" || s.Model != "gpt-x" {
		t.Errorf("quick slot: %+v", s)
	}
	if s := slots["cheap"]; s.HasKey != "missing" {
		t.Errorf("cheap slot: %+v", s)
	}
	if st.Slots[0].Name != ai.SlotMain {
		t.Errorf("slots not in the laptop's order: %+v", st.Slots)
	}
}

// Spec (WEBP-ITERMOC): itermoc.ask opens one of the laptop's own setup
// forms only after a laptop card (Cancel first, focused); Cancel runs
// nothing. The form itself is filled on the laptop.
func TestWebItermocAskOpensTheLaptopFormAfterItsCard(t *testing.T) {
	r := webItermocRig(t)
	m := r.m
	var ran []string
	old := webItermocDispatch
	webItermocDispatch = func(_ *Model, id string) tea.Cmd { ran = append(ran, id); return nil }
	t.Cleanup(func() { webItermocDispatch = old })
	for _, a := range webproto.ItermocActions {
		f := r.call(webproto.OpItermocAsk, &webproto.ItermocAskArgs{Action: a})
		if f["t"] != "res" || !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.Buttons()[0].ID != webproto.ChoiceCancel {
			t.Fatalf("%s: asked %v, card open %v", a, f, m.confirmOpen)
		}
		if card := strings.Join(strings.Fields(strings.ReplaceAll(ansi.Strip(m.confirm.View()), "│", " ")), " "); !strings.Contains(card, "on the laptop") {
			t.Errorf("%s: the card does not say the form opens on the laptop:\n%s", a, m.confirm.View())
		}
		m.confirmOpen = false
		if cmd := m.answerWebAsk(webproto.ChoiceCancel); cmd != nil || len(ran) != 0 {
			t.Fatalf("%s: Cancel ran %v", a, ran)
		}
		r.call(webproto.OpItermocAsk, &webproto.ItermocAskArgs{Action: a})
		m.confirmOpen = false
		m.answerWebAsk(webproto.ChoiceConfirm)
		if len(ran) != 1 || ran[0] != a {
			t.Fatalf("%s: ran %v", a, ran)
		}
		ran = nil
	}
}

// Spec (WEBP-ITERMOC): each action the page may ask for is a real laptop
// palette command, one the AI may never run itself (its policy has it).
func TestWebItermocActionsAreLaptopCommands(t *testing.T) {
	ids := map[string]bool{}
	for _, it := range paletteItems() {
		ids[it.ID] = true
	}
	for _, a := range webproto.ItermocActions {
		if !ids[a] {
			t.Errorf("%s is not a laptop palette command", a)
		}
		if _, ok := aiActionPolicyFor(a); !ok {
			t.Errorf("%s has no AI policy", a)
		}
	}
}
