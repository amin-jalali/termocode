package profileform

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// AIP-2 gap: a step that runs on a provider's model and is then moved to
// another choice must drop provider + model, or the profile names a
// provider and a slot at once (a file error).
func TestFormModelChoiceClearsProvider(t *testing.T) {
	p := ai.Profile{Steps: []ai.ProfileStep{{ID: "a", Title: "A", Provider: ai.KindOpenAI, Model: "gpt-5-mini"}}}
	m := New("prov", p, cfg(1))
	m.SetSize(120, 40)
	if got := m.Profile().Steps[0]; got.Provider != ai.KindOpenAI || got.Model != "gpt-5-mini" {
		t.Fatalf("opened step %+v", got)
	}
	m = focusOn(m, item{iSModel, 0})
	for i := 0; i < 6; i++ {
		m, _ = keys(m, "right")
		st := m.Profile().Steps[0]
		if st.Provider != "" && (st.Slot != "" || st.Category != "" || st.CLI != "") {
			t.Fatalf("after %d moves the step names a provider and more: %+v (%v)", i+1, st, m.Errors())
		}
	}
	if !m.Valid() {
		t.Errorf("errors %v", m.Errors())
	}
}

func pickGroups() []Group {
	return []Group{
		{Kind: ai.KindAnthropic, Name: "Anthropic", Tag: "API · pay per use", Models: []string{"claude-haiku-5", "claude-sonnet-5-5"}},
		{Kind: ai.KindOpenAI, Name: "OpenAI", Tag: "your plan", CLI: ai.CLICodex, Models: []string{"gpt-6-astra", "gpt-5.6-luna"}},
		{Kind: ai.KindOpenRouter, Name: "OpenRouter", Tag: "API · pay per use", Models: []string{"gpt-5-mini", "llama-4"}},
		{Kind: ai.KindOllama, Name: "Ollama", Tag: "local · free", Models: []string{"llama3.2:3b", "qwen2.5-coder:7b"}},
	}
}

func pickModel(p ai.Profile) Model {
	c := cfg(1)
	c.Groups = pickGroups()
	m := New("p", p, c)
	m.SetSize(120, 50)
	return m
}

func oneStep() ai.Profile {
	return ai.Profile{Steps: []ai.ProfileStep{{ID: "a", Title: "Review"}}}
}

// The picker lists every model of every connected AI, grouped by AI with
// its cost tag, after the chat's model and the automatic choices, and
// ends with "+ Add another AI".
func TestPickerListsAllGroups(t *testing.T) {
	m := pickModel(oneStep())
	m = focusOn(m, item{iSModel, 0})
	m, msgs := keys(m, "enter")
	if len(msgs) != 0 {
		t.Fatalf("enter on Model opens the picker only: %v", msgs)
	}
	if step, open := m.PickerOpen(); !open || step != 0 {
		t.Fatalf("picker %d %v", step, open)
	}
	out := plain(m)
	for _, want := range []string{"Model for step 1 · Review", "Search", "Same model as the chat", "Automatic: quick", "Automatic: deep",
		"ANTHROPIC", "API · pay per use", "claude-sonnet-5-5", "OPENAI", "your plan", "gpt-6-astra",
		"OLLAMA", "local · free", "llama3.2:3b", "FROM CONFIG.JSON", "gpt", "+ Add another AI (OpenAI, Llama…)"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	if strings.Contains(strings.ToLower(out), "slot") {
		t.Errorf("the picker says slot:\n%s", out)
	}
}

// Search filters by model id and by AI name; nothing found says so.
func TestPickerSearch(t *testing.T) {
	m := pickModel(oneStep())
	m.OpenPicker(0)
	m, _ = keys(m, "m", "i", "n", "i")
	out := plain(m)
	if !strings.Contains(out, "gpt-5-mini") || strings.Contains(out, "claude-sonnet") || strings.Contains(out, "ANTHROPIC") {
		t.Errorf("search mini:\n%s", out)
	}
	if strings.Contains(out, "Use “") || strings.Contains(out, "Other model id") {
		t.Errorf("a search that finds models offers no free id:\n%s", out)
	}
	m.SetPickerQuery("ollama")
	out = plain(m)
	if !strings.Contains(out, "qwen2.5-coder:7b") || strings.Contains(out, "gpt-5-mini") {
		t.Errorf("search by AI name:\n%s", out)
	}
	m.SetPickerQuery("zzz")
	out = plain(m)
	if !strings.Contains(out, "No model matches “zzz”.") || !strings.Contains(out, "+ Add another AI") {
		t.Errorf("empty search:\n%s", out)
	}
	m, _ = keys(m, "backspace", "backspace", "backspace")
	if m.pick.q != "" || !strings.Contains(plain(m), "claude-sonnet-5-5") {
		t.Errorf("backspace clears the search: %q", m.pick.q)
	}
}

// Enter writes provider + model (a sign-in AI: cli + model) into the step
// and clears what it ran on before; Profile() has it and the form is valid.
func TestPickerEnterSavesModel(t *testing.T) {
	p := ai.Profile{Steps: []ai.ProfileStep{{ID: "a", Title: "Review", Category: "quick"}}}
	m := pickModel(p)
	m.OpenPicker(0)
	m.SetPickerQuery("gpt-5-mini")
	m, _ = keys(m, "enter")
	st := m.Profile().Steps[0]
	if st.Provider != ai.KindOpenRouter || st.Model != "gpt-5-mini" || st.Slot != "" || st.Category != "" || st.CLI != "" {
		t.Fatalf("step %+v", st)
	}
	if _, open := m.PickerOpen(); open || m.foc != (item{iSModel, 0}) {
		t.Errorf("the picker closes on the Model button: %+v", m.foc)
	}
	if !m.Valid() || !m.Dirty() {
		t.Errorf("valid %v dirty %v %v", m.Valid(), m.Dirty(), m.Errors())
	}
	if out := plain(m); !strings.Contains(out, "gpt-5-mini · OpenRouter") {
		t.Errorf("button:\n%s", out)
	}
	m.OpenPicker(0)
	m.SetPickerQuery("astra")
	m, _ = keys(m, "enter")
	st = m.Profile().Steps[0]
	if st.CLI != ai.CLICodex || st.Model != "gpt-6-astra" || st.Provider != "" {
		t.Fatalf("cli step %+v", st)
	}
	if !m.Valid() {
		t.Errorf("errors %v", m.Errors())
	}
}

// A sign-in AI also takes a model id that is not in the list.
func TestPickerOtherModelID(t *testing.T) {
	m := pickModel(oneStep())
	m.OpenPicker(0)
	other := func() int {
		for k, o := range m.pickRows() {
			if o.other && o.ch.cli == ai.CLICodex {
				return k
			}
		}
		t.Fatal("no Other model id row in the Codex group")
		return -1
	}
	m, _ = m.pickChoose(other())
	if _, open := m.PickerOpen(); !open || !strings.Contains(plain(m), "Type the model id in Search") {
		t.Fatalf("empty search: the picker stays and says why:\n%s", plain(m))
	}
	m.SetPickerQuery("gpt-7-nova")
	if out := plain(m); !strings.Contains(out, "Use “gpt-7-nova”") {
		t.Fatalf("no Use row:\n%s", out)
	}
	m, _ = m.pickChoose(other())
	if st := m.Profile().Steps[0]; st.CLI != ai.CLICodex || st.Model != "gpt-7-nova" {
		t.Errorf("step %+v", st)
	}
}

// An old step on a named model (config.json) opens and saves unchanged;
// the picker shows it marked; Esc closes only the picker.
func TestPickerOldSlotStep(t *testing.T) {
	p := ai.Profile{Steps: []ai.ProfileStep{{ID: "a", Title: "Review", Slot: "gpt"}, {ID: "b", Title: "Fix", Slot: "local"}}}
	m := pickModel(p)
	if got := m.Profile(); got.Steps[0].Slot != "gpt" || got.Steps[1].Slot != "local" || m.Dirty() {
		t.Fatalf("opened %+v dirty %v", got.Steps, m.Dirty())
	}
	m.OpenPicker(1)
	out := plain(m)
	if !strings.Contains(out, "FROM CONFIG.JSON") || !strings.Contains(out, theme.GlyphOK.String()+" local") {
		t.Errorf("old named model:\n%s", out)
	}
	m, msgs := keys(m, "esc")
	if len(msgs) != 0 {
		t.Errorf("esc in the picker never cancels the form: %v", msgs)
	}
	if _, open := m.PickerOpen(); open || m.Dirty() || m.foc != (item{iSModel, 1}) {
		t.Errorf("after esc: dirty %v foc %+v", m.Dirty(), m.foc)
	}
}

// "+ Add another AI" asks the app for the Add AI card for this step.
func TestPickerAddAnother(t *testing.T) {
	m := pickModel(oneStep())
	m.OpenPicker(0)
	m.pick.cur = firstChoosable(m.pickRows())
	m, _ = keys(m, "up") // from the first row up wraps to the last: Add
	_, msgs := keys(m, "enter")
	if len(msgs) != 1 || msgs[0] != (AddAIMsg{Step: 0, Picker: true}) {
		t.Fatalf("msgs %#v", msgs)
	}
}

// Mouse: every row of the picker is a hit target, and a click chooses.
func TestPickerMouse(t *testing.T) {
	m := pickModel(oneStep())
	m.OpenPicker(0)
	titles := map[string]ButtonRect{}
	for _, r := range m.ButtonRects() {
		titles[r.Title] = r
	}
	for _, want := range []string{"Same model as the chat", "claude-sonnet-5-5", "gpt-6-astra", "llama3.2:3b", "+ Add another AI (OpenAI, Llama…)"} {
		if _, ok := titles[want]; !ok {
			t.Errorf("no hit target %q (%v)", want, titles)
		}
	}
	r := titles["llama3.2:3b"]
	m, _ = m.HandleMouse(r.X+1, r.Y, tea.MouseActionPress, tea.MouseButtonLeft)
	if st := m.Profile().Steps[0]; st.Provider != ai.KindOllama || st.Model != "llama3.2:3b" {
		t.Errorf("click: %+v", st)
	}
	// The Model button's middle opens the picker; its arrows still step.
	_, tg := m.frame()
	ox, oy := m.Origin()
	for _, t0 := range tg {
		if t0.it == (item{iSModel, 0}) && t0.opt == 0 {
			m, _ = m.HandleMouse(ox+1+t0.x+1, oy+1+t0.y, tea.MouseActionPress, tea.MouseButtonLeft)
			break
		}
	}
	if _, open := m.PickerOpen(); !open {
		t.Error("a click on the Model button opens the picker")
	}
}

// The picker fits a small window, and the cursor row stays in view.
func TestPickerFits80x24(t *testing.T) {
	m := pickModel(oneStep())
	m.SetSize(80, 24)
	m.OpenPicker(0)
	lines := strings.Split(plain(m), "\n")
	if len(lines) > 24 {
		t.Errorf("%d lines", len(lines))
	}
	for _, l := range lines {
		if runewidth.StringWidth(l) > 80 {
			t.Errorf("too wide: %q", l)
		}
	}
	for i := 0; i < 20; i++ {
		m, _ = keys(m, "down")
		rows := m.pickRows()
		label := rows[m.pick.cur].label
		if !strings.Contains(plain(m), "▸ ") || !strings.Contains(plain(m), label) {
			t.Fatalf("cursor row %q not shown:\n%s", label, plain(m))
		}
	}
}
