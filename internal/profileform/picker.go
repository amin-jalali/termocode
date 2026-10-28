package profileform

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// The model picker (AIP-6): a step's Model button opens one searchable
// list of every model of every connected AI, grouped by AI with a tag
// that says how it is paid for. A choice is saved as the step's
// provider + model (a sign-in AI: cli + model). The chat's model and
// the two automatic choices come first; named models from config.json
// come last, and only when there are some (or the step uses one).
// "+ Add another AI" asks the app for the Add AI card; on the way back
// the picker opens again on the same step.

// Group is one connected AI in the picker.
type Group struct {
	Kind   string   // ai.Kind*
	Name   string   // "Anthropic"
	Tag    string   // ai.CostTag: "your plan", "API · pay per use", "local · free"
	CLI    string   // set: the AI runs through this sign-in CLI (a step saves cli + model)
	Models []string // model ids, in order
	Needs  string   // why it cannot run now ("needs sign-in"); "" ready
}

// choice is what a step runs on: one of slot, category, cli or provider
// (with model), or nothing (the chat's model).
type choice struct {
	slot, category, cli, provider, model string
}

// pickOpt is one row of the picker.
type pickOpt struct {
	head  bool   // a group title (not chosen)
	label string // what the row says
	hint  string // quiet text after it (a group: its tag)
	ch    choice
	group string // the group's name (search matches it)
	other bool   // "Other model id": the search text is the model id
	add   bool   // + Add another AI
}

// pickState is the open picker.
type pickState struct {
	open   bool
	step   int
	q      string // the search text
	cur    int    // index into the shown rows
	scroll int
	note   string // one line under the search (why Enter did nothing)
}

// OpenPicker opens the model picker on step i.
func (m *Model) OpenPicker(i int) {
	if i < 0 || i >= len(m.steps) {
		return
	}
	m.setFocus(item{iSModel, i})
	m.pick = pickState{open: true, step: i}
	rows := m.pickRows()
	m.pick.cur = firstChoosable(rows)
	for k, o := range rows { // start on the step's own choice
		if o.choosable() && !o.other && !o.add && o.ch == m.steps[i].ch {
			m.pick.cur = k
			break
		}
	}
	m.pickVisible()
}

// PickerOpen reports the picker shown, and for which step.
func (m Model) PickerOpen() (int, bool) { return m.pick.step, m.pick.open }

// SetPickerQuery types q into the picker's search (tests, the demo).
func (m *Model) SetPickerQuery(q string) {
	m.pick.q = q
	m.pick.cur = firstChoosable(m.pickRows())
	m.pick.scroll, m.pick.note = 0, ""
}

func (o pickOpt) choosable() bool { return !o.head }

// firstChoosable is the first model row (else the first row that can be
// chosen at all: "Use …", Add).
func firstChoosable(rows []pickOpt) int {
	for k, o := range rows {
		if o.choosable() && !o.other && !o.add {
			return k
		}
	}
	for k, o := range rows {
		if o.choosable() {
			return k
		}
	}
	return 0
}

// isConfigSlot: the built-in slots are not "from config.json".
func isConfigSlot(name string) bool {
	return name != ai.SlotMain && name != ai.SlotQuick && name != ai.SlotLocal
}

// allOpts is every row of step i's picker before the search.
func (m Model) allOpts(i int) []pickOpt {
	cur := choice{}
	if i >= 0 && i < len(m.steps) {
		cur = m.steps[i].ch
	}
	out := []pickOpt{
		{label: "Same model as the chat", hint: "what the chat uses"},
		{label: "Automatic: quick", hint: "a fast, low-cost model", ch: choice{category: agentdag.CategoryQuick}},
		{label: "Automatic: deep", hint: "the strongest model", ch: choice{category: agentdag.CategoryDeep}},
	}
	known := func(c choice) bool {
		for _, o := range out {
			if !o.head && !o.other && o.ch == c {
				return true
			}
		}
		return false
	}
	for _, g := range m.cfg.Groups {
		hint := g.Tag
		if g.Needs != "" {
			hint += " · " + g.Needs
		}
		out = append(out, pickOpt{head: true, label: strings.ToUpper(g.Name), hint: hint, group: g.Name})
		for _, id := range g.Models {
			c := choice{provider: g.Kind, model: id}
			if g.CLI != "" {
				c = choice{cli: g.CLI, model: id}
			}
			out = append(out, pickOpt{label: id, ch: c, group: g.Name})
		}
		if g.CLI != "" || len(g.Models) == 0 {
			c := choice{provider: g.Kind}
			if g.CLI != "" {
				c = choice{cli: g.CLI}
			}
			out = append(out, pickOpt{label: "Other model id…", ch: c, group: g.Name, other: true})
		}
	}
	var slots []string
	for _, s := range m.cfg.Slots {
		if isConfigSlot(s.Name) {
			slots = append(slots, s.Name)
		}
	}
	if cur.slot != "" && !known(cur) {
		have := false
		for _, s := range slots {
			have = have || s == cur.slot
		}
		if !have {
			slots = append(slots, cur.slot)
		}
	}
	if len(slots) > 0 {
		out = append(out, pickOpt{head: true, label: "FROM CONFIG.JSON", hint: "named models", group: "config.json"})
		for _, s := range slots {
			hint := ""
			for _, c := range m.cfg.Slots {
				if c.Name == s && !c.KeyOK {
					hint = "no key"
				}
			}
			out = append(out, pickOpt{label: s, hint: hint, ch: choice{slot: s}, group: "config.json"})
		}
	}
	if !known(cur) && (cur.provider != "" || cur.cli != "") {
		// The step's own model of an AI that is not connected (or not in
		// the list): it stays choosable, under what it needs.
		val, _ := m.choiceLabel(cur)
		row := []pickOpt{{head: true, label: "THIS STEP NOW", hint: m.needs(i), group: ""},
			{label: val, ch: cur}}
		out = append(out[:3:3], append(row, out[3:]...)...)
	}
	return out
}

// pickRows is the picker's rows for its search text, with the
// "+ Add another AI" row last. A group shows when one of its rows
// matches. "Other model id…" shows without a search; when a search finds
// no model and reads like a model id, it turns into "Use <text>".
func (m Model) pickRows() []pickOpt {
	raw := strings.TrimSpace(m.pick.q)
	out := m.filterRows(strings.ToLower(raw), false)
	if raw != "" && !anyReal(out) && ai.ValidModelID(raw) {
		out = m.filterRows(strings.ToLower(raw), true)
	}
	return append(out, pickOpt{label: "+ Add another AI (OpenAI, Llama…)", add: true})
}

// filterRows is the rows that match q; use: the "Other" rows read
// "Use <text>" (q is not empty).
func (m Model) filterRows(q string, use bool) []pickOpt {
	all := m.allOpts(m.pick.step)
	match := func(r pickOpt) bool {
		switch {
		case r.other:
			return q == "" || use
		case q == "":
			return true
		}
		return strings.Contains(strings.ToLower(r.label), q) || strings.Contains(strings.ToLower(r.group), q)
	}
	var out []pickOpt
	for k := 0; k < len(all); k++ {
		o := all[k]
		if !o.head {
			if match(o) {
				out = append(out, o)
			}
			continue
		}
		var keep []pickOpt
		for k+1 < len(all) && !all[k+1].head {
			k++
			r := all[k]
			if !match(r) {
				continue
			}
			if r.other && use {
				r.label = "Use “" + strings.TrimSpace(m.pick.q) + "”"
			}
			keep = append(keep, r)
		}
		if len(keep) > 0 {
			out = append(out, o)
			out = append(out, keep...)
		}
	}
	return out
}

func anyReal(rows []pickOpt) bool {
	for _, r := range rows {
		if !r.head && !r.other && !r.add {
			return true
		}
	}
	return false
}

// pickEmpty reports that no model matches the search (only "Use …" rows
// and Add are left).
func (m Model) pickEmpty(rows []pickOpt) bool {
	return strings.TrimSpace(m.pick.q) != "" && !anyReal(rows)
}

// cycleModel moves step i's choice by d through the picker's list
// (← → on the Model button): the models, the automatic choices, the
// named models; never "Other" or Add.
func (m *Model) cycleModel(i, d int) {
	var list []choice
	for _, o := range m.allOpts(i) {
		if !o.head && !o.other && !o.add {
			list = append(list, o.ch)
		}
	}
	if len(list) == 0 {
		return
	}
	at := -1
	for k, c := range list {
		if c == m.steps[i].ch {
			at = k
		}
	}
	switch {
	case at < 0 && d > 0:
		at = 0
	case at < 0:
		at = len(list) - 1
	default:
		at = ((at+d)%len(list) + len(list)) % len(list)
	}
	m.steps[i].ch = list[at]
}

// pickerKey handles a key while the picker is open: typing searches,
// ↑ ↓ move, Enter chooses, Esc goes back to the form.
func (m Model) pickerKey(k tea.KeyMsg) (Model, tea.Cmd) {
	rows := m.pickRows()
	move := func(d int) {
		for n, c := 0, m.pick.cur; n < len(rows); n++ {
			c = (c + d + len(rows)) % len(rows)
			if rows[c].choosable() {
				m.pick.cur = c
				break
			}
		}
		m.pick.note = ""
	}
	switch k.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.closePicker()
		return m, nil
	case tea.KeyDown, tea.KeyTab, tea.KeyCtrlN:
		move(1)
	case tea.KeyUp, tea.KeyShiftTab, tea.KeyCtrlP:
		move(-1)
	case tea.KeyPgDown:
		for n := 0; n < 8; n++ {
			move(1)
		}
	case tea.KeyPgUp:
		for n := 0; n < 8; n++ {
			move(-1)
		}
	case tea.KeyEnter:
		return m.pickChoose(m.pick.cur)
	case tea.KeyBackspace:
		r := []rune(m.pick.q)
		if len(r) > 0 {
			m.SetPickerQuery(string(r[:len(r)-1]))
		}
	case tea.KeyCtrlU:
		m.SetPickerQuery("")
	case tea.KeySpace:
		m.SetPickerQuery(m.pick.q + " ")
	case tea.KeyRunes:
		var clean []rune
		for _, c := range k.Runes {
			if c >= 0x20 && c != 0x7f && c != '\n' && c != '\r' {
				clean = append(clean, c)
			}
		}
		if len(clean) > 0 {
			m.SetPickerQuery(m.pick.q + string(clean))
		}
	}
	m.pickVisible()
	return m, nil
}

// pickChoose takes row k: a model is saved into the step (and clears
// what it ran on before), "Use <text>" takes the search text as the
// model id, Add asks the app for the Add AI card.
func (m Model) pickChoose(k int) (Model, tea.Cmd) {
	rows := m.pickRows()
	if k < 0 || k >= len(rows) || !rows[k].choosable() {
		return m, nil
	}
	o := rows[k]
	step := m.pick.step
	switch {
	case o.add:
		m.closePicker()
		return m, emit(AddAIMsg{Step: step, Picker: true})
	case o.other:
		id := strings.TrimSpace(m.pick.q)
		if id == "" {
			m.pick.note = "Type the model id in Search, then choose this row."
			return m, nil
		}
		if !ai.ValidModelID(id) {
			m.pick.note = "A model id uses letters, digits and . _ : / @ + -"
			return m, nil
		}
		o.ch.model = id
	}
	m.steps[step].ch = o.ch
	m.closePicker()
	return m, nil
}

// closePicker goes back to the form on the step's Model button.
func (m *Model) closePicker() {
	step := m.pick.step
	m.pick = pickState{}
	if step >= 0 && step < len(m.steps) {
		m.setFocus(item{iSModel, step})
	}
	m.ensureVisible()
}

// groupFor is the connected AI a choice runs on.
func (m Model) groupFor(c choice) (Group, bool) {
	for _, g := range m.cfg.Groups {
		switch {
		case c.cli != "" && g.CLI == c.cli:
			return g, true
		case c.provider != "" && g.CLI == "" && g.Kind == c.provider:
			return g, true
		}
	}
	return Group{}, false
}

// choiceLabel is what the Model button says for c, and its quiet tag.
func (m Model) choiceLabel(c choice) (val, tag string) {
	g, ok := m.groupFor(c)
	if ok {
		tag = g.Tag
	}
	switch {
	case c.cli != "":
		title := ai.CLITitle(c.cli)
		if title == "" {
			return c.cli, ""
		}
		if c.model == "" {
			return title + " · its default model", "your plan"
		}
		return c.model + " · " + title, "your plan"
	case c.provider != "":
		return c.model + " · " + ai.ProviderName(c.provider), tag
	case c.category != "":
		return "Automatic: " + c.category, ""
	case c.slot != "":
		return c.slot, ""
	}
	return "Same model as the chat", ""
}

// needs is why step i cannot run now ("" it can): its sign-in CLI is
// missing or signed out, or its AI is not connected.
func (m Model) needs(i int) string {
	if i < 0 || i >= len(m.steps) {
		return ""
	}
	c := m.steps[i].ch
	switch {
	case c.cli != "":
		title := ai.CLITitle(c.cli)
		if title == "" {
			return "" // an unknown CLI: its error shows
		}
		switch m.cfg.CLIs[c.cli] {
		case CLIMissing:
			return title + " is not installed"
		case CLILoggedOut:
			return title + " is not signed in"
		}
	case c.provider != "":
		if _, ok := m.groupFor(c); !ok && ai.ProviderName(c.provider) != c.provider { // an unknown kind: its error shows
			return "needs " + ai.ProviderName(c.provider)
		}
	}
	return ""
}

// fixMsg is the Add AI request of step i's Fix.
func (m Model) fixMsg(i int) AddAIMsg {
	c := m.steps[i].ch
	if c.cli != "" {
		return AddAIMsg{Step: i, Kind: ai.CLIKind(c.cli), Method: ai.MethodCLI}
	}
	return AddAIMsg{Step: i, Kind: c.provider}
}
