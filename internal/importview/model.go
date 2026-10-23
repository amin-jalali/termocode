// Package importview is the review panel of "Import from another editor":
// two columns (what the other editor has → what termocode will set),
// grouped into Settings / Keys / Theme / Snippets / Extensions, with a toggle per row.
// It only shows and selects; the app writes the files after a final
// confirm (internal/app/import_editor.go).
package importview

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/migrate"
)

// ApplyMsg asks the app to confirm and write the selected rows.
type ApplyMsg struct{}

// CloseMsg closes the panel without writing anything.
type CloseMsg struct{}

// rowState says how a row behaves.
type rowState uint8

const (
	stateToggle rowState = iota // can be turned on and off
	stateInfo                   // a note or a tool suggestion; never written
	stateSkip                   // not imported, or nothing would change
)

// section is one group of rows.
type section struct {
	name string
	rows []int // indexes into Model.rows
}

// row is one plan row as the panel shows it.
type row struct {
	plan     int // index into the plan rows
	state    rowState
	on       bool
	conflict bool   // replaces a value or key the user has now
	clash    bool   // a key or snippet that is already taken: the one row kind shown as a conflict
	left     string // the other editor's text
	right    string // termocode's side
	reason   string // why it is off or not imported
	with     string // key rows: the title of the command the key has now
	key      string // key rows: only one selected row per key
	target   string // setting rows: only one selected row per target
}

// line is one body line: a section header (row < 0) or a row.
type line struct {
	sec int
	row int // -1: the section header
}

// Model is the review panel.
type Model struct {
	plan     migrate.Plan
	title    string // the source editor's name
	dir      string // where its files are (shown in the header)
	rows     []row
	sections []section
	lines    []line
	cursor   int // index into lines
	scroll   int // first body line shown
	w, h     int

	actionTitleOf func(name string) string // see WithActionTitle
}

// New builds the panel from a plan. Rows the plan marks Apply start on.
func New(plan migrate.Plan, dir string, opts ...Option) Model {
	m := Model{plan: plan, title: clean(plan.Source.Title), dir: clean(dir)}
	for _, o := range opts {
		o(&m)
	}
	if m.title == "" {
		m.title = clean(plan.Source.ID)
	}
	order := []struct {
		name  string
		kinds []migrate.RowKind
	}{
		{"Settings", []migrate.RowKind{migrate.RowSetting}},
		{"Keys", []migrate.RowKind{migrate.RowKey}},
		{"Theme", []migrate.RowKind{migrate.RowTheme}},
		{"Snippets", []migrate.RowKind{migrate.RowSnippet}},
		{"Extensions", []migrate.RowKind{migrate.RowExtension}},
		{"Other", []migrate.RowKind{migrate.RowVimrc}},
	}
	for _, o := range order {
		var idx []int
		for i, pr := range plan.Rows {
			for _, k := range o.kinds {
				if pr.Kind == k {
					idx = append(idx, i)
				}
			}
		}
		if len(idx) == 0 {
			continue
		}
		var sec section
		sec.name = o.name
		for _, i := range idx {
			m.rows = append(m.rows, m.makeRow(i))
			sec.rows = append(sec.rows, len(m.rows)-1)
		}
		// Rows that can change something first, then notes, then rows
		// that are not imported; the plan order stays inside each group.
		sort.SliceStable(sec.rows, func(a, b int) bool {
			return m.rows[sec.rows[a]].state < m.rows[sec.rows[b]].state
		})
		m.sections = append(m.sections, sec)
	}
	// Two selected rows must not write the same key or setting: the
	// first one stays on.
	for i := range m.rows {
		for j := 0; j < i && m.rows[i].on; j++ {
			if m.rows[j].on && m.same(i, j) {
				m.rows[i].on = false
			}
		}
	}
	for s, sec := range m.sections {
		m.lines = append(m.lines, line{sec: s, row: -1})
		for _, r := range sec.rows {
			m.lines = append(m.lines, line{sec: s, row: r})
		}
	}
	return m
}

func (m Model) makeRow(i int) row {
	pr := m.plan.Rows[i]
	r := row{plan: i, left: clean(pr.SourceText), reason: clean(pr.Reason), state: stateSkip}
	// "VS Code default: ctrl+p" → "ctrl+p  (VS Code default)": the key
	// comes first, so a narrow card still shows it.
	if editor, key, ok := strings.Cut(r.left, " default: "); ok && pr.Kind == migrate.RowKey {
		r.left = key + "  (" + editor + " default)"
	}
	cur := m.plan.Current
	switch pr.Kind {
	case migrate.RowSetting:
		if pr.Target == "" {
			break
		}
		r.target = pr.Target
		old, had := cur.Config[pr.Target]
		label := settingLabel(pr.Target)
		if had && reflect.DeepEqual(old, pr.Value) {
			r.right = fmt.Sprintf("%s = %s", label, settingValue(pr.Value))
			break
		}
		r.state = stateToggle
		r.right = fmt.Sprintf("%s = %s", label, settingValue(pr.Value))
		if had {
			r.conflict = true
			r.right = fmt.Sprintf("%s  %s → %s", label, settingValue(old), settingValue(pr.Value))
		}
	case migrate.RowKey:
		if pr.Action == "" || pr.Key == "" {
			break
		}
		r.key = pr.Key
		r.right = m.keyText(pr)
		if pr.ConflictWith != "" {
			r.with = m.actionTitle(pr.ConflictWith)
		}
		if pr.Apply || pr.Conflict {
			r.state = stateToggle
			r.conflict = pr.Conflict
			r.clash = pr.Conflict
		}
	case migrate.RowTheme:
		if pr.Target == "" {
			break
		}
		r.right = themeText(pr)
		if pr.Target == cur.ThemeID {
			break
		}
		r.state = stateToggle
		if cur.ThemeID != "" {
			r.conflict = true
			r.right = themeName(cur.ThemeID) + " → " + themeText(pr)
		}
	case migrate.RowSnippet:
		if pr.Trigger == "" {
			break
		}
		r.right = clean(pr.Equivalent)
		if pr.Apply || pr.Conflict {
			r.state = stateToggle
			r.conflict = pr.Conflict
			r.clash = pr.Conflict
		}
	case migrate.RowExtension, migrate.RowVimrc:
		switch {
		case len(pr.Tools) > 0:
			r.state = stateInfo
			r.right = "suggests " + strings.Join(pr.Tools, ", ") + " · install it yourself"
		case pr.Equivalent != "" && pr.Equivalent != "no equivalent" && pr.Equivalent != "not imported":
			r.state = stateInfo
			r.right = clean(pr.Equivalent)
		}
	}
	if r.state == stateToggle {
		r.on = pr.Apply
	}
	if r.state == stateSkip {
		switch {
		case r.right == "" || strings.HasPrefix(pr.Equivalent, "no equivalent") || pr.Equivalent == "not imported":
			r.right = "not imported"
			if r.reason != "" {
				r.right += " · " + r.reason
			}
		case r.reason != "":
			r.right = r.reason
		}
	}
	return r
}

// keyText is the key row's termocode side, with command titles in place
// of the action names: "ctrl+alt+o → Go to File (conflicts with …)".
func (m Model) keyText(pr migrate.ReviewRow) string {
	s := clean(pr.Equivalent)
	if pr.Action != "" {
		s = strings.Replace(s, "→ "+pr.Action, "→ "+m.actionTitle(pr.Action), 1)
	}
	if pr.ConflictWith != "" {
		s = strings.Replace(s, "conflicts with "+pr.ConflictWith, "conflicts with "+m.actionTitle(pr.ConflictWith), 1)
	}
	return s
}

// themeText is the theme row's termocode side with the theme's name in
// place of its id; a closest-name suggestion keeps its note.
func themeText(pr migrate.ReviewRow) string {
	s := clean(pr.Equivalent)
	if pr.Target != "" && strings.HasPrefix(s, pr.Target) {
		return themeName(pr.Target) + s[len(pr.Target):]
	}
	return s
}

// clean drops control characters (escape sequences in a hostile file
// must not reach the terminal) and folds tabs and new lines to spaces.
func clean(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// same reports whether rows i and j write the same key or setting.
func (m Model) same(i, j int) bool {
	a, b := m.rows[i], m.rows[j]
	return (a.key != "" && a.key == b.key) || (a.target != "" && a.target == b.target)
}

// taken reports whether another selected row writes the same key or
// setting as row i.
func (m Model) taken(i int) bool {
	for j := range m.rows {
		if j != i && m.rows[j].on && m.same(i, j) {
			return true
		}
	}
	return false
}

// exclusive turns off the other rows that write the same key or setting
// as row i.
func (m *Model) exclusive(i int) {
	for j := range m.rows {
		if j != i && m.rows[j].on && m.same(i, j) {
			m.rows[j].on = false
		}
	}
}

// SetSize sets the screen size.
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h; m.follow() }

// Title is the source editor's name.
func (m Model) Title() string { return m.title }

// Plan returns the plan with Apply set to what the user selected. Notes,
// tool suggestions and rows that are not imported are never applied.
func (m Model) Plan() migrate.Plan {
	p := m.plan
	p.Rows = append([]migrate.ReviewRow(nil), m.plan.Rows...)
	for i := range p.Rows {
		p.Rows[i].Apply = false
	}
	for _, r := range m.rows {
		if r.state == stateToggle && r.on {
			p.Rows[r.plan].Apply = true
		}
	}
	return p
}

// Counts is what the selected rows will change.
type Counts struct {
	Settings, Keys int
	Snippets       int
	Theme          string   // the new theme id, "" when unchanged
	Tools          []string // suggested tools (never installed)
}

// Total is the number of changes.
func (c Counts) Total() int {
	n := c.Settings + c.Keys + c.Snippets
	if c.Theme != "" {
		n++
	}
	return n
}

// Summary is a short "2 settings · 3 keys · theme" line.
func (c Counts) Summary() string {
	var parts []string
	if c.Settings > 0 {
		parts = append(parts, plural(c.Settings, "setting"))
	}
	if c.Keys > 0 {
		parts = append(parts, plural(c.Keys, "key"))
	}
	if c.Theme != "" {
		parts = append(parts, "theme")
	}
	if c.Snippets > 0 {
		parts = append(parts, plural(c.Snippets, "snippet"))
	}
	if len(parts) == 0 {
		return "nothing selected"
	}
	return strings.Join(parts, " · ")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// Counts counts the selected rows.
func (m Model) Counts() Counts {
	var c Counts
	seen := map[string]bool{}
	for _, r := range m.rows {
		pr := m.plan.Rows[r.plan]
		for _, t := range pr.Tools {
			if !seen[t] {
				seen[t] = true
				c.Tools = append(c.Tools, t)
			}
		}
		if r.state != stateToggle || !r.on {
			continue
		}
		switch pr.Kind {
		case migrate.RowSetting:
			c.Settings++
		case migrate.RowKey:
			c.Keys++
		case migrate.RowTheme:
			c.Theme = pr.Target
		case migrate.RowSnippet:
			c.Snippets++
		}
	}
	sort.Strings(c.Tools)
	return c
}

// selectable is how many rows can be toggled; selected how many are on.
func (m Model) tally(rows []int) (selected, selectable, skipped int) {
	for _, i := range rows {
		switch m.rows[i].state {
		case stateToggle:
			selectable++
			if m.rows[i].on {
				selected++
			}
		case stateSkip:
			skipped++
		}
	}
	return
}

func (m Model) allRows() []int {
	out := make([]int, len(m.rows))
	for i := range out {
		out[i] = i
	}
	return out
}

// Toggle flips the row or section under the cursor.
func (m *Model) Toggle() {
	if m.cursor < 0 || m.cursor >= len(m.lines) {
		return
	}
	ln := m.lines[m.cursor]
	if ln.row < 0 {
		m.toggleSection(ln.sec)
		return
	}
	m.toggleRow(ln.row)
}

func (m *Model) toggleRow(i int) {
	if m.rows[i].state != stateToggle {
		return
	}
	m.rows[i].on = !m.rows[i].on
	if m.rows[i].on {
		m.exclusive(i)
	}
}

// toggleSection turns every row of a section on, or off when all are on.
func (m *Model) toggleSection(s int) {
	sel, n, _ := m.tally(m.sections[s].rows)
	m.setRows(m.sections[s].rows, sel < n)
}

func (m *Model) setRows(rows []int, on bool) {
	for _, i := range rows {
		if m.rows[i].state != stateToggle {
			continue
		}
		if !on {
			m.rows[i].on = false
			continue
		}
		// The first row for a key or setting wins.
		if !m.taken(i) {
			m.rows[i].on = true
		}
	}
}

// SelectAll turns on every row that can change something.
func (m *Model) SelectAll() { m.setRows(m.allRows(), true) }

// SelectNone turns every row off.
func (m *Model) SelectNone() { m.setRows(m.allRows(), false) }

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update handles keys.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	g := m.geom()
	switch k.String() {
	case "esc", "q":
		return m, func() tea.Msg { return CloseMsg{} }
	case "enter":
		return m, func() tea.Msg { return ApplyMsg{} }
	case " ", "x":
		m.Toggle()
	case "a":
		m.SelectAll()
	case "n":
		m.SelectNone()
	case "down", "j", "ctrl+n", "tab":
		m.move(1)
	case "up", "k", "ctrl+p", "shift+tab":
		m.move(-1)
	case "pgdown", "ctrl+d":
		m.move(max(1, g.bodyH-1))
	case "pgup", "ctrl+u":
		m.move(-max(1, g.bodyH-1))
	case "home", "g":
		m.move(-len(m.lines))
	case "end", "G":
		m.move(len(m.lines))
	case "]":
		m.jumpSection(1)
	case "[":
		m.jumpSection(-1)
	}
	return m, nil
}

func (m *Model) move(d int) {
	m.cursor = min(max(m.cursor+d, 0), max(len(m.lines)-1, 0))
	m.follow()
}

// jumpSection moves the cursor to the next / previous section header.
func (m *Model) jumpSection(dir int) {
	for i := m.cursor + dir; i >= 0 && i < len(m.lines); i += dir {
		if m.lines[i].row < 0 {
			m.cursor = i
			m.follow()
			return
		}
	}
}

// follow scrolls so the cursor line is shown.
func (m *Model) follow() {
	g := m.geom()
	if g.bodyH <= 0 {
		return
	}
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	}
	if m.cursor >= m.scroll+g.bodyH {
		m.scroll = m.cursor - g.bodyH + 1
	}
	m.clampScroll(g)
}

func (m *Model) clampScroll(g geom) {
	m.scroll = min(m.scroll, max(len(m.lines)-g.bodyH, 0))
	m.scroll = max(m.scroll, 0)
}

// Scroll moves the view (the mouse wheel); the cursor stays on screen.
func (m *Model) Scroll(d int) {
	g := m.geom()
	m.scroll += d
	m.clampScroll(g)
	if m.cursor < m.scroll {
		m.cursor = m.scroll
	}
	if g.bodyH > 0 && m.cursor >= m.scroll+g.bodyH {
		m.cursor = m.scroll + g.bodyH - 1
	}
	m.cursor = min(max(m.cursor, 0), max(len(m.lines)-1, 0))
}

// HandleMouse handles a mouse event at screen cell (x, y).
func (m Model) HandleMouse(x, y int, action tea.MouseAction, button tea.MouseButton) (Model, tea.Cmd) {
	l := m.layout(false)
	if x < l.x || x >= l.x+l.w || y < l.y || y >= l.y+l.h {
		return m, nil // outside: a stray click must not close the review
	}
	switch button {
	case tea.MouseButtonWheelUp:
		m.Scroll(-3)
		return m, nil
	case tea.MouseButtonWheelDown:
		m.Scroll(3)
		return m, nil
	}
	if button != tea.MouseButtonLeft || action != tea.MouseActionPress {
		return m, nil
	}
	return m, m.click(l.hitAt(x-l.x, y-l.y))
}

// ButtonAt returns the id of the control at screen cell (x, y), "" when
// none. For tests and the app's mouse probe.
func (m Model) ButtonAt(x, y int) string {
	l := m.layout(false)
	return l.hitAt(x-l.x, y-l.y)
}

func (m *Model) click(id string) tea.Cmd {
	switch id {
	case "":
		return nil
	case "all":
		m.SelectAll()
	case "none":
		m.SelectNone()
	case "apply":
		return func() tea.Msg { return ApplyMsg{} }
	case "cancel":
		return func() tea.Msg { return CloseMsg{} }
	default:
		var n int
		if _, err := fmt.Sscanf(id, "line:%d", &n); err == nil && n >= 0 && n < len(m.lines) {
			m.cursor = n
			m.Toggle()
			m.follow()
		}
	}
	return nil
}
