// Package profileform is the step form of "Edit profile…" (AH-5): a
// floating card to make or change an agent profile without JSON. It
// holds the fields (name, title, budget, the quick / deep slots and a
// list of steps), checks them with ai.ValidateProfile (the rules of
// profiles.json, never a copy of them) and shows each problem next to
// its field. It never writes a file and never sees a key: the app turns
// SaveMsg into the preview card and the write.
package profileform

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

// Slot is a model slot the form offers. KeyOK only says whether the slot
// can answer (a key is set); the form never gets the key.
type Slot struct {
	Name  string
	KeyOK bool
}

// Config is what the app tells the form.
type Config struct {
	Title       string          // the card title
	Slots       []Slot          // the model slots, in the order shown
	ParallelMax int             // ai_parallel_max: "waits for" shows at 2 or more
	Taken       map[string]bool // other profiles of the user's file (a name that is free to save over is not in here)
	Editing     bool            // changing the user's profile Name in place (Save replaces it)
}

// SaveMsg: Save… was pressed and the form is valid.
type SaveMsg struct{}

// CancelMsg: Cancel or Esc. Dirty: something was changed.
type CancelMsg struct{ Dirty bool }

// RemoveStepMsg: Remove step was pressed; the app asks, then calls
// RemoveStep.
type RemoveStepMsg struct {
	Index int
	Title string
}

// DeleteMsg: Delete profile… was pressed; the app asks.
type DeleteMsg struct{}

// KeysMsg: ? was pressed; the app shows the key card.
type KeysMsg struct{}

// What an item is.
type kind int

const (
	iName kind = iota
	iTitle
	iBudget
	iCatQ
	iCatD
	iStepHead
	iSTitle
	iSID
	iSModel
	iSTools
	iSPrompt
	iSDeps
	iSPlan
	iAdd
	iUp
	iDown
	iRemove
	iSave
	iCancel
	iDelete
)

// item is one thing the focus can sit on; step is the step index for
// the step kinds.
type item struct {
	kind kind
	step int
}

func (k kind) text() bool {
	switch k {
	case iName, iTitle, iBudget, iSTitle, iSID, iSPrompt, iSDeps:
		return true
	}
	return false
}

func (k kind) choice() bool {
	switch k {
	case iCatQ, iCatD, iSModel, iSTools:
		return true
	}
	return false
}

func (k kind) button() bool { return k >= iAdd }

// modelOpt is one entry of a step's model choice.
type modelOpt struct {
	label    string
	slot     string
	category string
}

var toolPresets = []string{agentdag.PresetNone, agentdag.PresetReadOnly, agentdag.PresetAll}

type stepState struct {
	orig   ai.ProfileStep // keeps what the form does not show (allow / deny, output, max_steps)
	id     string
	title  string
	model  int // index into Model.opts
	tools  int // index into toolPresets
	system string
	deps   string // step numbers, "1, 2"
	plan   bool
}

// Model is the open form.
type Model struct {
	cfg      Config
	w, h     int
	name     string
	origName string
	title    string
	budget   string
	catQ     int // 0: not set, then 1+slot index
	catD     int
	opts     []modelOpt // the step model choices
	steps    []stepState
	sel      int // the expanded step
	foc      item
	editing  bool
	caret    int
	scroll   int
	initial  ai.Profile // for Dirty
	initName string
	hasInit  bool
}

// New opens the form on p (a copy of a profile, or the profile to
// change) under name.
func New(name string, p ai.Profile, cfg Config) Model {
	m := Model{cfg: cfg, name: name, origName: name, title: p.Title}
	if p.BudgetUSD > 0 {
		m.budget = strconv.FormatFloat(p.BudgetUSD, 'f', -1, 64)
	}
	m.opts = []modelOpt{{label: "default (main)"}}
	for _, s := range cfg.Slots {
		m.opts = append(m.opts, modelOpt{label: s.Name, slot: s.Name})
	}
	m.opts = append(m.opts, modelOpt{label: "category quick", category: agentdag.CategoryQuick},
		modelOpt{label: "category deep", category: agentdag.CategoryDeep})
	m.catQ = m.slotIndex(p.Categories[agentdag.CategoryQuick])
	m.catD = m.slotIndex(p.Categories[agentdag.CategoryDeep])
	ids := make(map[string]int, len(p.Steps))
	for i, st := range p.Steps {
		if st.ID != "" {
			ids[st.ID] = i + 1
		}
	}
	for _, st := range p.Steps {
		ss := stepState{orig: st, id: st.ID, title: st.Title, system: st.System, plan: st.Plan, tools: len(toolPresets) - 1}
		for i, t := range toolPresets {
			if st.Tools.Preset == t {
				ss.tools = i
			}
		}
		ss.model = m.modelIndex(st.Slot, st.Category)
		var nums []string
		for _, d := range st.Deps {
			if n, ok := ids[d]; ok {
				nums = append(nums, strconv.Itoa(n))
			} else {
				nums = append(nums, d) // an unknown dep shows as written, with its error
			}
		}
		ss.deps = strings.Join(nums, ", ")
		m.steps = append(m.steps, ss)
	}
	for i := range m.steps {
		if m.steps[i].id == "" {
			m.steps[i].id = m.freeID()
		}
	}
	m.foc = item{kind: iName}
	m.initial, m.initName, m.hasInit = m.Profile(), m.name, true
	return m
}

// slotIndex is the category option for slot name (0: none).
func (m Model) slotIndex(name string) int {
	for i, s := range m.cfg.Slots {
		if s.Name == name {
			return i + 1
		}
	}
	return 0
}

// modelIndex is the option for a step's slot or category; a slot the
// form does not know is added so the user sees (and can fix) it.
func (m *Model) modelIndex(slot, cat string) int {
	for i, o := range m.opts {
		if o.slot == slot && o.category == cat && (slot != "" || cat != "" || i == 0) {
			return i
		}
	}
	if slot != "" {
		at := len(m.opts) - 2 // before the two categories
		m.opts = append(m.opts[:at], append([]modelOpt{{label: slot, slot: slot}}, m.opts[at:]...)...)
		return at
	}
	return 0
}

// SetSize sets the window size.
func (m *Model) SetSize(w, h int) {
	m.w, m.h = w, h
	m.ensureVisible()
}

// freeID is the first step-N that no step uses.
func (m Model) freeID() string {
	for n := 1; ; n++ {
		id := fmt.Sprintf("step-%d", n)
		used := false
		for _, s := range m.steps {
			if s.id == id {
				used = true
			}
		}
		if !used {
			return id
		}
	}
}

// Name is the profile name typed so far.
func (m Model) Name() string { return strings.TrimSpace(m.name) }

// Replace reports that Save updates the user's profile in place (the
// name was not changed).
func (m Model) Replace() bool { return m.cfg.Editing && m.Name() == m.origName }

// Dirty reports that something differs from what the form opened with.
func (m Model) Dirty() bool {
	return m.hasInit && (m.Name() != m.initName || !reflect.DeepEqual(m.Profile(), m.initial))
}

// Profile is the profile the fields describe (best effort: an unreadable
// number or step number is left out and shown as an error).
func (m Model) Profile() ai.Profile {
	p := ai.Profile{Name: m.Name(), Title: strings.TrimSpace(m.title)}
	if b, err := strconv.ParseFloat(strings.TrimSpace(m.budget), 64); err == nil {
		p.BudgetUSD = b
	}
	cats := map[string]string{}
	if m.catQ > 0 {
		cats[agentdag.CategoryQuick] = m.cfg.Slots[m.catQ-1].Name
	}
	if m.catD > 0 {
		cats[agentdag.CategoryDeep] = m.cfg.Slots[m.catD-1].Name
	}
	if len(cats) > 0 {
		p.Categories = cats
	}
	for i := range m.steps {
		s := m.steps[i]
		st := s.orig
		st.ID, st.Title, st.System, st.Plan = strings.TrimSpace(s.id), strings.TrimSpace(s.title), s.system, s.plan
		o := m.opts[s.model]
		st.Slot, st.Category = o.slot, o.category
		if preset := toolPresets[s.tools]; !(preset == agentdag.PresetAll && s.orig.Tools.Preset == "") {
			st.Tools.Preset = preset
		}
		deps, _ := m.parseDeps(s.deps, i)
		st.Deps = deps
		p.Steps = append(p.Steps, st)
	}
	return p
}

// parseDeps turns "1, 3" into step ids; bad is the first token that is
// not a step number.
func (m Model) parseDeps(text string, self int) (ids []string, bad string) {
	for _, tok := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		n, err := strconv.Atoi(tok)
		if err != nil || n < 1 || n > len(m.steps) {
			if bad == "" {
				bad = tok
			}
			continue
		}
		ids = append(ids, strings.TrimSpace(m.steps[n-1].id))
	}
	return ids, bad
}

// Errors maps a field to its problem: "name", "budget", "cat:quick",
// "cat:deep", "steps", "plan", and "<step>:<field>" for id, model,
// tools, prompt, deps. The text comes from ai.ValidateProfile (the rules
// of profiles.json); only what the form parses itself (the budget as a
// number, step numbers) is checked here.
func (m Model) Errors() map[string]string {
	errs := map[string]string{}
	set := func(k, msg string) {
		if _, ok := errs[k]; !ok {
			errs[k] = msg
		}
	}
	if _, err := strconv.ParseFloat(strings.TrimSpace(m.budget), 64); m.budget != "" && strings.TrimSpace(m.budget) != "" && err != nil {
		set("budget", "a number of dollars, like 0.50 (empty: no limit)")
	}
	for i, s := range m.steps {
		if _, bad := m.parseDeps(s.deps, i); bad != "" {
			set(fmt.Sprintf("%d:deps", i), fmt.Sprintf("no step %q (use step numbers, like 1, 2)", bad))
		}
	}
	if err := ai.ValidProfileName(m.Name()); err != nil {
		set("name", err.Error())
	} else if m.cfg.Taken[m.Name()] && !m.Replace() {
		set("name", fmt.Sprintf("a profile named %q exists already", m.Name()))
	}
	known := map[string]bool{}
	for _, s := range m.cfg.Slots {
		known[s.Name] = true
	}
	err := ai.ValidateProfile(m.Profile(), known, m.cfg.ParallelMax)
	var pe *ai.ProfileError
	if !errors.As(err, &pe) {
		return errs
	}
	at := -1
	for i, s := range m.steps {
		if strings.TrimSpace(s.id) == pe.Step || (pe.Step == "" && s.id == "") {
			at = i // a duplicate shows on the last one
		}
	}
	key := func(f string) string { return fmt.Sprintf("%d:%s", at, f) }
	switch {
	case pe.Step != "" && at >= 0:
		f := pe.Field
		switch f {
		case "slot", "category":
			f = "model"
		case "system":
			f = "prompt"
		case "id", "tools", "deps":
		default:
			f = "step"
		}
		set(key(f), pe.Msg)
	case strings.HasPrefix(pe.Field, "categories:"):
		set("cat:"+strings.TrimPrefix(pe.Field, "categories:"), pe.Msg)
	case pe.Field == "name" || pe.Field == "budget" || pe.Field == "plan":
		set(pe.Field, pe.Msg)
	default:
		set("steps", pe.Msg)
	}
	return errs
}

// Valid reports that Save is possible.
func (m Model) Valid() bool { return len(m.Errors()) == 0 }

// errKey is the key of Errors() for an item ("" for none).
func errKey(it item) string {
	switch it.kind {
	case iName:
		return "name"
	case iBudget:
		return "budget"
	case iCatQ:
		return "cat:quick"
	case iCatD:
		return "cat:deep"
	case iSID:
		return fmt.Sprintf("%d:id", it.step)
	case iSModel:
		return fmt.Sprintf("%d:model", it.step)
	case iSTools:
		return fmt.Sprintf("%d:tools", it.step)
	case iSPrompt:
		return fmt.Sprintf("%d:prompt", it.step)
	case iSDeps:
		return fmt.Sprintf("%d:deps", it.step)
	case iSPlan:
		return "plan"
	}
	return ""
}

// ── Items and focus ─────────────────────────────────────────────────────

// showDeps: "waits for" shows when steps may run side by side, or when a
// step already has deps (so the user can clear them).
func (m Model) showDeps(i int) bool {
	return m.cfg.ParallelMax >= 2 || strings.TrimSpace(m.steps[i].deps) != ""
}

// items lists everything the focus can reach, in order.
func (m Model) items() []item {
	out := []item{{kind: iName}, {kind: iTitle}, {kind: iBudget}, {kind: iCatQ}, {kind: iCatD}}
	for i := range m.steps {
		out = append(out, item{iStepHead, i})
		if i != m.sel {
			continue
		}
		out = append(out, item{iSTitle, i}, item{iSID, i}, item{iSModel, i}, item{iSTools, i}, item{iSPrompt, i})
		if m.showDeps(i) {
			out = append(out, item{iSDeps, i})
		}
		out = append(out, item{iSPlan, i})
	}
	out = append(out, item{kind: iAdd}, item{kind: iUp}, item{kind: iDown}, item{kind: iRemove}, item{kind: iSave}, item{kind: iCancel})
	if m.cfg.Editing {
		out = append(out, item{kind: iDelete})
	}
	return out
}

func (m *Model) setFocus(it item) {
	m.editing = false
	if it.kind >= iStepHead && it.kind <= iSPlan && it.step < len(m.steps) {
		m.sel = it.step
	}
	m.foc = it
}

func (m *Model) move(d int) {
	m.editing = false
	items := m.items()
	at := 0
	for i, it := range items {
		if it == m.foc {
			at = i
		}
	}
	m.setFocus(items[(at+d+len(items))%len(items)])
}

// text is the text of a text item.
func (m *Model) text(it item) *string {
	switch it.kind {
	case iName:
		return &m.name
	case iTitle:
		return &m.title
	case iBudget:
		return &m.budget
	}
	if it.step >= len(m.steps) {
		return nil
	}
	s := &m.steps[it.step]
	switch it.kind {
	case iSTitle:
		return &s.title
	case iSID:
		return &s.id
	case iSPrompt:
		return &s.system
	case iSDeps:
		return &s.deps
	}
	return nil
}

// cycle changes a choice (or the planner box) by d.
func (m *Model) cycle(it item, d int) {
	wrap := func(v, n int) int { return ((v+d)%n + n) % n }
	switch it.kind {
	case iCatQ:
		m.catQ = wrap(m.catQ, len(m.cfg.Slots)+1)
	case iCatD:
		m.catD = wrap(m.catD, len(m.cfg.Slots)+1)
	case iSModel:
		m.steps[it.step].model = wrap(m.steps[it.step].model, len(m.opts))
	case iSTools:
		m.steps[it.step].tools = wrap(m.steps[it.step].tools, len(toolPresets))
	case iSPlan:
		m.steps[it.step].plan = !m.steps[it.step].plan
	}
}

// ── Step list operations ────────────────────────────────────────────────

// AddStep appends a step and focuses its title.
func (m *Model) AddStep() {
	n := len(m.steps) + 1
	m.steps = append(m.steps, stepState{id: m.freeID(), title: fmt.Sprintf("Step %d", n), tools: 1})
	m.sel = len(m.steps) - 1
	m.setFocus(item{iSTitle, m.sel})
	m.ensureVisible()
}

// MoveStep moves the expanded step by d (-1 up, 1 down).
func (m *Model) MoveStep(d int) {
	i, j := m.sel, m.sel+d
	if j < 0 || j >= len(m.steps) {
		return
	}
	ids := m.depIDs() // "waits for" shows step numbers: keep them on the same steps
	m.steps[i], m.steps[j] = m.steps[j], m.steps[i]
	ids[i], ids[j] = ids[j], ids[i]
	m.sel = j
	m.renumber(ids)
	m.ensureVisible()
}

// depIDs is every step's "waits for" as step ids.
func (m Model) depIDs() [][]string {
	out := make([][]string, len(m.steps))
	for i, s := range m.steps {
		out[i], _ = m.parseDeps(s.deps, i)
	}
	return out
}

// renumber writes ids (the deps of m.steps[k]) back as step numbers.
func (m *Model) renumber(ids [][]string) {
	byID := map[string]int{}
	for k, s := range m.steps {
		byID[strings.TrimSpace(s.id)] = k + 1
	}
	for k := range m.steps {
		var nums []string
		for _, d := range ids[k] {
			if n, ok := byID[d]; ok {
				nums = append(nums, strconv.Itoa(n))
			}
		}
		m.steps[k].deps = strings.Join(nums, ", ")
	}
}

// RemoveStep removes step i (after the app asked) and drops it from the
// other steps' "waits for".
func (m *Model) RemoveStep(i int) {
	if i < 0 || i >= len(m.steps) {
		return
	}
	ids := m.depIDs()
	gone := strings.TrimSpace(m.steps[i].id)
	m.steps = append(m.steps[:i:i], m.steps[i+1:]...)
	ids = append(ids[:i:i], ids[i+1:]...)
	for k := range ids {
		var keep []string
		for _, d := range ids[k] {
			if d != gone {
				keep = append(keep, d)
			}
		}
		ids[k] = keep
	}
	m.renumber(ids)
	m.sel = min(max(m.sel, 0), len(m.steps)-1)
	m.sel = max(m.sel, 0)
	m.setFocus(item{kind: iAdd})
	m.ensureVisible()
}

// ── Keys ────────────────────────────────────────────────────────────────

func emit(v tea.Msg) tea.Cmd { return func() tea.Msg { return v } }

// Update handles a key.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	var cmd tea.Cmd
	if m.editing {
		m = m.editKey(k)
	} else {
		m, cmd = m.navKey(k)
	}
	m.ensureVisible()
	return m, cmd
}

func (m Model) navKey(k tea.KeyMsg) (Model, tea.Cmd) {
	it := m.foc
	switch k.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		return m, emit(CancelMsg{Dirty: m.Dirty()})
	case tea.KeyTab, tea.KeyDown:
		m.move(1)
	case tea.KeyShiftTab, tea.KeyUp:
		m.move(-1)
	case tea.KeyLeft, tea.KeyRight:
		d := 1
		if k.Type == tea.KeyLeft {
			d = -1
		}
		if it.kind.choice() || it.kind == iSPlan {
			m.cycle(it, d)
		} else if it.kind.button() {
			m.move(d)
		}
	case tea.KeyEnter, tea.KeySpace:
		return m.activate(it)
	case tea.KeyRunes:
		if k.Paste {
			return m, nil
		}
		switch string(k.Runes) {
		case "a":
			m.AddStep()
		case "x":
			return m, m.removeCmd()
		case "K":
			m.MoveStep(-1)
		case "J":
			m.MoveStep(1)
		case "?":
			return m, emit(KeysMsg{})
		case "j":
			m.move(1)
		case "k":
			m.move(-1)
		}
	}
	return m, nil
}

func (m Model) removeCmd() tea.Cmd {
	if len(m.steps) == 0 {
		return nil
	}
	s := m.steps[m.sel]
	t := strings.TrimSpace(s.title)
	if t == "" {
		t = s.id
	}
	return emit(RemoveStepMsg{Index: m.sel, Title: t})
}

// activate is Enter (or a click) on an item.
func (m Model) activate(it item) (Model, tea.Cmd) {
	switch {
	case it.kind.text():
		m.editing = true
		m.caret = len([]rune(*m.text(it)))
	case it.kind.choice() || it.kind == iSPlan:
		m.cycle(it, 1)
	case it.kind == iStepHead:
		m.setFocus(it)
	default:
		return m.press(it.kind)
	}
	return m, nil
}

// press runs a button.
func (m Model) press(b kind) (Model, tea.Cmd) {
	switch b {
	case iAdd:
		m.AddStep()
	case iUp:
		m.MoveStep(-1)
	case iDown:
		m.MoveStep(1)
	case iRemove:
		return m, m.removeCmd()
	case iCancel:
		return m, emit(CancelMsg{Dirty: m.Dirty()})
	case iDelete:
		return m, emit(DeleteMsg{})
	case iSave:
		if m.Valid() {
			return m, emit(SaveMsg{})
		}
		m.focusFirstError() // a disabled Save shows what to fix
	}
	return m, nil
}

// focusFirstError moves the focus to the first item with an error.
func (m *Model) focusFirstError() {
	errs := m.Errors()
	for _, it := range m.allItems() {
		if _, ok := errs[errKey(it)]; ok && errKey(it) != "" {
			m.setFocus(it)
			return
		}
	}
	for k := range errs { // an error without a field of its own: the first step
		_ = k
		if len(m.steps) > 0 {
			m.setFocus(item{iStepHead, 0})
		}
		return
	}
}

// allItems is items() with every step expanded.
func (m Model) allItems() []item {
	var out []item
	cur := m.sel
	for i := -1; i < len(m.steps); i++ {
		if i >= 0 {
			m.sel = i
		}
		for _, it := range m.items() {
			if i < 0 && it.kind >= iStepHead && it.kind <= iSPlan {
				continue
			}
			if i >= 0 && (it.kind < iStepHead || it.kind > iSPlan || it.step != i) {
				continue
			}
			out = append(out, it)
		}
	}
	m.sel = cur
	return out
}

// ── Text editing ────────────────────────────────────────────────────────

func (m Model) editKey(k tea.KeyMsg) Model {
	it := m.foc
	p := m.text(it)
	if p == nil {
		m.editing = false
		return m
	}
	r := []rune(*p)
	m.caret = min(max(m.caret, 0), len(r))
	multi := it.kind == iSPrompt
	insert := func(in []rune) {
		clean := make([]rune, 0, len(in))
		for _, c := range in {
			switch {
			case c == '\n' || c == '\r':
				if multi {
					clean = append(clean, '\n')
				} else {
					clean = append(clean, ' ')
				}
			case c == '\t':
				clean = append(clean, ' ', ' ')
			case c >= 0x20 && c != 0x7f:
				clean = append(clean, c)
			}
		}
		r = append(r[:m.caret:m.caret], append(clean, r[m.caret:]...)...)
		m.caret += len(clean)
	}
	switch k.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.editing = false
		return m
	case tea.KeyTab:
		m.move(1)
		return m
	case tea.KeyShiftTab:
		m.move(-1)
		return m
	case tea.KeyEnter:
		if !multi {
			m.editing = false
			return m
		}
		insert([]rune{'\n'})
	case tea.KeyBackspace:
		if m.caret > 0 {
			r = append(r[:m.caret-1], r[m.caret:]...)
			m.caret--
		}
	case tea.KeyDelete:
		if m.caret < len(r) {
			r = append(r[:m.caret], r[m.caret+1:]...)
		}
	case tea.KeyLeft:
		m.caret = max(m.caret-1, 0)
	case tea.KeyRight:
		m.caret = min(m.caret+1, len(r))
	case tea.KeyHome, tea.KeyCtrlA:
		m.caret = lineStart(r, m.caret)
	case tea.KeyEnd, tea.KeyCtrlE:
		m.caret = lineEnd(r, m.caret)
	case tea.KeyUp, tea.KeyDown:
		if multi {
			m.caret = moveLine(r, m.caret, map[tea.KeyType]int{tea.KeyUp: -1, tea.KeyDown: 1}[k.Type])
		}
	case tea.KeySpace:
		insert([]rune{' '})
	case tea.KeyRunes:
		insert(k.Runes)
	}
	*p = string(r)
	return m
}

func lineStart(r []rune, c int) int {
	for c > 0 && r[c-1] != '\n' {
		c--
	}
	return c
}

func lineEnd(r []rune, c int) int {
	for c < len(r) && r[c] != '\n' {
		c++
	}
	return c
}

// moveLine moves the caret one line up or down, keeping its column.
func moveLine(r []rune, c, d int) int {
	col := c - lineStart(r, c)
	if d < 0 {
		s := lineStart(r, c)
		if s == 0 {
			return c
		}
		ps := lineStart(r, s-1)
		return min(ps+col, s-1)
	}
	e := lineEnd(r, c)
	if e >= len(r) {
		return c
	}
	ns := e + 1
	return min(ns+col, lineEnd(r, ns))
}
