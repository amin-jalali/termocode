package app

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Itermoc MODELS: Add slot, Edit, Remove (no JSON) ─────────────────────
//
// The slot form is a connectcard form: name, provider, model and, for the
// providers that take one, a base URL. Save writes config.json ai_models
// through ai.SaveModelSlot (every other key and the slot's unknown fields
// stay). A key is never a field: after Save the card offers Set key, the
// existing masked prompt that writes to the secret store. Remove asks with
// Keep first and names the profiles that use the slot.

// confirmKindAISlotRemove is the "Remove model slot?" card.
const confirmKindAISlotRemove confirmKindEnum = 2310

// itermocNoSlotEdit is the AI policy reason of the slot form commands.
const itermocNoSlotEdit = "a form for the user; keys and config.json stay with the user (the AI shows the AI PROVIDERS section with ai_view_open)"

// imSlotForm is the open slot form (or the "saved, set the key?" card).
type imSlotForm struct {
	edit     bool   // Edit: the name is fixed
	name     string // Edit: the slot; after Save: the saved slot
	override bool   // Edit of a built-in quick/local without an entry yet
	err      string // the inline error of the last Save
	saved    bool   // the form is done: the card offers Set key
}

// imSlotHasURL reports whether the form shows a base URL for kind.
func imSlotHasURL(kind string) bool {
	switch kind {
	case ai.KindCompatible, ai.KindOllama, ai.KindLMStudio:
		return true
	}
	return false
}

// imUserSlot is the ai_models entry of name (ok=false: none).
func (m *Model) imUserSlot(name string) (ai.ModelSlot, bool) {
	sl, ok := m.aiS().settings.Models[name]
	return sl, ok
}

// openItermocSlotAdd opens the empty form.
func (m *Model) openItermocSlotAdd() tea.Cmd {
	m.aiS()
	st := m.itermocS()
	st.form = &imSlotForm{}
	return m.showItermocSlotForm(ai.KindAnthropic, "", "", "")
}

// openItermocSlotEdit opens the form for slot name. main has its own
// screen (AI: Configure Provider…); quick and local without an entry get
// an override form.
func (m *Model) openItermocSlotEdit(name string) tea.Cmd {
	if name == ai.SlotMain || name == "" {
		return m.openAIConfigure()
	}
	st := m.itermocS()
	if sl, ok := m.imUserSlot(name); ok {
		st.form = &imSlotForm{edit: true, name: name}
		return m.showItermocSlotForm(strings.ToLower(strings.TrimSpace(sl.Provider)), sl.Model, sl.BaseURL, name)
	}
	if name != ai.SlotQuick && name != ai.SlotLocal {
		return nil
	}
	kind, model := ai.KindAnthropic, ""
	if c, ok := m.aiS().aiSlotConfig(name); ok && c.Kind != ai.KindNone {
		kind, model = c.Kind, c.Model
	}
	st.form = &imSlotForm{edit: true, name: name, override: true}
	return m.showItermocSlotForm(kind, model, "", name)
}

// showItermocSlotForm opens the form card with these first values.
func (m *Model) showItermocSlotForm(kind, model, baseURL, name string) tea.Cmd {
	sel := 0
	for i, k := range ai.Kinds {
		if k == kind {
			sel = i
		}
	}
	m.connect = connectcard.New(m.itermocSlotFormCard(sel, model, baseURL, name))
	m.connect.SetSize(m.w, m.h)
	m.connectOpen = true
	return nil
}

// itermocSlotFormCard builds the form for the provider at index sel; the
// values are the first ones of a new card (the card keeps typed text).
func (m *Model) itermocSlotFormCard(sel int, model, baseURL, name string) connectcard.Card {
	f := m.itermocS().form
	kind := ai.Kinds[clampIdx(sel, len(ai.Kinds))]
	title := "Add model slot"
	var rows []connectcard.Row
	if f.edit {
		title = "Edit model slot " + f.name
		if f.override {
			title = "Override model slot " + f.name
		}
		rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: "Name      " + f.name})
	} else {
		rows = append(rows, connectcard.Row{Kind: connectcard.Input, ID: "name", Text: "Name", Inline: true,
			Placeholder: "e.g. gpt (letters, digits, - _ .)", Value: name})
	}
	rows = append(rows, connectcard.Row{Kind: connectcard.Choice, ID: "provider", Text: "Provider",
		Options: ai.Kinds, Selected: sel, Vertical: true})
	ph := "model id"
	if d := ai.DefaultModel(kind); d != "" {
		ph = "empty = " + d
	}
	rows = append(rows, connectcard.Row{Kind: connectcard.Input, ID: "model", Text: "Model", Inline: true, Placeholder: ph, Value: model})
	if sug := ai.SuggestedModels(kind); len(sug) > 0 {
		rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: "suggested: " + strings.Join(sug, imDot())})
	}
	if imSlotHasURL(kind) {
		ph := "empty = " + ai.DefaultBaseURL(kind)
		if kind == ai.KindCompatible {
			ph = "required: http://host:port/v1"
		}
		rows = append(rows, connectcard.Row{Kind: connectcard.Input, ID: "url", Text: "Base URL", Inline: true, Placeholder: ph, Value: baseURL})
	}
	switch {
	case f.err != "":
		rows = append(rows, connectcard.Row{Kind: connectcard.Fail, Text: f.err})
	case f.override:
		rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: "Saves an entry in ai_models; Remove… brings back the built-in."})
	default:
		rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: "No key here: Set key stores it in the secret store."})
	}
	return connectcard.Card{Title: title, Rows: rows, Form: true,
		Buttons: []connectcard.Button{{ID: "cancel", Title: "Cancel"}, {ID: "save", Title: "Save", Primary: true}},
		Keys:    []string{theme.ASCIIText("←→"), "pick"}}
}

// imDot is the " · " separator (ASCII mode: " - ").
func imDot() string { return theme.ASCIIText(" · ") }

// refreshItermocSlotCard rebuilds the open form after a key or a click
// (a new provider changes the placeholder, the suggestions and the base
// URL row). The typed text stays.
func (m *Model) refreshItermocSlotCard() {
	st := m.itermoc
	if st == nil || st.form == nil || st.form.saved || !m.connectOpen {
		return
	}
	m.connect.SetCard(m.itermocSlotFormCard(m.connect.Choice("provider"), "", "", ""))
}

// handleItermocSlotMsg routes the card's messages while the slot form is
// open. ok=false: not ours.
func (m *Model) handleItermocSlotMsg(msg tea.Msg) (tea.Cmd, bool) {
	if m.itermoc == nil || m.itermoc.form == nil {
		return nil, false
	}
	switch msg := msg.(type) {
	case connectcard.ButtonMsg:
		return m.onItermocSlotButton(msg.ID), true
	case connectcard.SubmitMsg:
		return m.onItermocSlotButton("save"), true
	case connectcard.KeyMsg:
		return nil, true
	case connectcard.CloseMsg:
		return m.onItermocSlotButton("cancel"), true
	}
	return nil, false
}

// closeItermocSlotForm closes the card; nothing is written.
func (m *Model) closeItermocSlotForm() {
	m.connectOpen = false
	if m.itermoc != nil {
		m.itermoc.form = nil
	}
}

func (m *Model) onItermocSlotButton(id string) tea.Cmd {
	f := m.itermoc.form
	switch id {
	case "cancel", "later":
		m.closeItermocSlotForm()
		return nil
	case "setkey":
		name := f.name
		m.closeItermocSlotForm()
		m.itermocRefreshNow()
		return m.itermocSetKey(name)
	case "save":
		if f.saved {
			return nil
		}
		return m.saveItermocSlot()
	}
	return nil
}

// saveItermocSlot checks and writes the form. A bad field stays in the
// form as an inline error and nothing is written.
func (m *Model) saveItermocSlot() tea.Cmd {
	f := m.itermoc.form
	name := f.name
	if !f.edit {
		name = strings.TrimSpace(m.connect.InputValue("name"))
	}
	kind := ai.Kinds[clampIdx(m.connect.Choice("provider"), len(ai.Kinds))]
	sl := ai.ModelSlot{Provider: kind, Model: strings.TrimSpace(m.connect.InputValue("model"))}
	if imSlotHasURL(kind) {
		sl.BaseURL = strings.TrimSpace(m.connect.InputValue("url"))
	}
	fail := func(msg string) tea.Cmd {
		f.err = msg
		m.refreshItermocSlotCard()
		return nil
	}
	switch _, exists := m.imUserSlot(name); {
	case name == "":
		return fail("Type a name for the slot.")
	case !f.edit && exists:
		return fail("A slot named " + name + " exists; use Edit… on its row.")
	}
	if err := ai.CheckModelSlot(name, sl); err != nil {
		return fail(err.Error())
	}
	if err := ai.CheckSlotBaseURL(sl.BaseURL); err != nil {
		return fail(err.Error())
	}
	path, err := ai.ConfigPath()
	if err != nil {
		return fail(err.Error())
	}
	if err := ai.SaveModelSlot(path, name, sl); err != nil {
		return fail(ai.Redact(err.Error()))
	}
	s := m.aiS()
	s.reload()
	m.syncAIInline()
	st := m.itermocS()
	st.refresh(*m, false)
	st.collapse[imSecModels] = false
	for i, r := range st.rows() {
		if r.kind == imRowSlot && r.name == name {
			st.cursor = i
		}
	}
	st.touch()
	verb := "added"
	if f.edit {
		verb = "saved"
	}
	key := imKeyMissing
	if c, ok := s.aiSlotConfig(name); ok {
		key = itermocKeyOf(c)
	}
	if key != imKeyMissing && key != imKeyUnset {
		m.closeItermocSlotForm()
		return m.aiInfo("Model slot " + name + " " + verb)
	}
	f.saved, f.name, f.err = true, name, ""
	m.connect.SetCard(itermocSlotKeyCard(name, sl, verb, key))
	return nil
}

// itermocSlotKeyCard is the card after Save when the provider has no key:
// Set key opens the masked prompt (the secret store), Later closes.
func itermocSlotKeyCard(name string, sl ai.ModelSlot, verb string, key imKeyState) connectcard.Card {
	what := sl.Provider
	if sl.Model != "" {
		what += imDot() + sl.Model
	} else if d := ai.DefaultModel(sl.Provider); d != "" {
		what += imDot() + d + " (default)"
	}
	label := ai.KindLabel(sl.Provider)
	text := "It needs " + aOrAn(label) + " " + label + " key. The key goes to the secret store, never into config.json."
	btn := "Set key"
	switch {
	case sl.Provider == ai.KindClaudeOAuth:
		text, btn = "It needs a Claude sign-in.", "Sign in"
	case key == imKeyUnset:
		text = "Does the server need a key? It goes to the secret store, never into config.json."
	}
	return connectcard.Card{Title: "Model slot " + name + " " + verb,
		Rows: []connectcard.Row{
			{Kind: connectcard.OK, Text: theme.ASCIIText(name + " → " + what)},
			{Kind: connectcard.Text, Text: text},
		},
		Buttons: []connectcard.Button{{ID: "later", Title: "Later"}, {ID: "setkey", Title: btn, Primary: true}}}
}

func aOrAn(word string) string {
	if word != "" && strings.ContainsRune("AEIOUaeiou", rune(word[0])) {
		return "an"
	}
	return "a"
}

// ── Remove… ──────────────────────────────────────────────────────────────

// itermocSlotUsers lists the profiles whose steps or categories name slot.
func (m *Model) itermocSlotUsers(slot string) []string {
	s := m.aiS()
	var out []string
	for _, n := range s.profiles.Names() {
		p, ok := s.profiles.Get(n)
		if !ok {
			continue
		}
		uses := false
		for _, c := range p.Categories {
			uses = uses || c == slot
		}
		for _, st := range p.Steps {
			uses = uses || st.Slot == slot
		}
		if uses {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// openItermocSlotRemove asks before a user slot goes: Keep is first and
// focused. The card names the profiles that use the slot.
func (m *Model) openItermocSlotRemove(name string) tea.Cmd {
	if _, ok := m.imUserSlot(name); !ok {
		return nil
	}
	msg := "The slot leaves config.json (ai_models). The provider's key stays in the secret store."
	if name == ai.SlotQuick || name == ai.SlotLocal {
		msg += "\nThe built-in " + name + " slot comes back."
	}
	if users := m.itermocSlotUsers(name); len(users) > 0 {
		msg += "\nProfiles that use it: " + strings.Join(users, ", ") + ". They will fail preflight until you change them."
	}
	m.itermocS().removeName = name
	m.confirm = confirm.Risky("Remove model slot "+name+"?", msg,
		confirm.Button{ID: "keep", Title: "Keep"},
		confirm.Button{ID: "remove", Title: "Remove", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAISlotRemove
	return nil
}

// answerItermocSlotRemove runs the card's button (Esc is Keep).
func (m *Model) answerItermocSlotRemove(id string) tea.Cmd {
	st := m.itermocS()
	name := st.removeName
	st.removeName = ""
	if id != "remove" || name == "" {
		return nil
	}
	path, err := ai.ConfigPath()
	if err == nil {
		err = ai.RemoveModelSlot(path, name)
	}
	if err != nil {
		return m.aiToastErr("Model slot not removed", err)
	}
	m.aiS().reload()
	m.syncAIInline()
	m.itermocRefreshNow()
	return m.aiInfo("Model slot " + name + " removed")
}
