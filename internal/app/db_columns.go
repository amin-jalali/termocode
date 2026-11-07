package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
	tea "github.com/charmbracelet/bubbletea"
)

// DBX-8: the column layout of the result grid. Columns can be hidden,
// moved, frozen on the left and made narrower or wider. A table's layout
// is saved per connection and table (db/columns.json in the config dir);
// a console result keeps its layout for the session, keyed by its column
// names. The layout names columns, so it outlives a rerun and a column
// the table gains or drops.

const pickerKindDBColumns pickerKindEnum = 1280

const (
	dbColMinWidth  = 3
	dbColMaxWidth  = 200
	dbColWidthStep = 4
)

type dbColLayout struct {
	Order  []string       `json:"order,omitempty"`
	Hidden []string       `json:"hidden,omitempty"`
	Frozen int            `json:"frozen,omitempty"`
	Width  map[string]int `json:"width,omitempty"`
	off    int            // columns scrolled past the frozen ones (not saved)
}

func (l *dbColLayout) empty() bool {
	return len(l.Order) == 0 && len(l.Hidden) == 0 && l.Frozen == 0 && len(l.Width) == 0
}

type dbColStore struct {
	loaded  bool
	saved   map[string]*dbColLayout // tables, on disk
	session map[string]*dbColLayout // console results
}

func dbColumnsPath() string { return dbConfigPath("columns.json") }

func (st *dbColStore) load() {
	if st.loaded {
		return
	}
	st.loaded = true
	st.saved, st.session = map[string]*dbColLayout{}, map[string]*dbColLayout{}
	if p := dbColumnsPath(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &st.saved)
		}
	}
	for k, l := range st.saved {
		if l == nil {
			delete(st.saved, k)
		}
	}
}

func (st *dbColStore) save() error {
	p := dbColumnsPath()
	if p == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st.saved, "", "  ")
	if err != nil {
		return err
	}
	return importAtomicWrite(p, b, 0o600)
}

// dbColKey names the layout of r: a table, or a console result by its
// column names. table is true when the layout is saved.
func dbColKey(r *dbRun) (key string, table bool) {
	if tv := r.tv; tv != nil {
		return strings.Join([]string{"table", tv.conn, tv.schema, tv.name}, "\x1f"), true
	}
	names := make([]string, len(r.result.Columns))
	for i, c := range r.result.Columns {
		names[i] = c.Name
	}
	return "result\x1f" + r.conn + "\x1f" + strings.Join(names, "\x1f"), false
}

// colLayout is the layout of the result on the page (nil: none yet);
// create makes one.
func (s *dbConsoleState) colLayout(create bool) *dbColLayout {
	if s.shown == nil || s.shown.redis != nil {
		return nil
	}
	if s.colStore == nil {
		s.colStore = &dbColStore{}
	}
	st := s.colStore
	st.load()
	key, table := dbColKey(s.shown)
	m := st.session
	if table {
		m = st.saved
	}
	l := m[key]
	if l == nil && create {
		l = &dbColLayout{}
		m[key] = l
	}
	return l
}

// visibleCols is the result columns the layout shows, in display order
// (every column when there is no layout).
func (s *dbConsoleState) visibleCols(cols []dbclient.Column) []int {
	l := s.colLayout(false)
	out := make([]int, 0, len(cols))
	if l == nil || l.empty() {
		for i := range cols {
			out = append(out, i)
		}
		return out
	}
	used := make([]bool, len(cols))
	hidden := func(name string) bool { return slices.Contains(l.Hidden, name) }
	for _, name := range l.Order {
		for i, c := range cols {
			if !used[i] && c.Name == name {
				used[i] = true
				if !hidden(name) {
					out = append(out, i)
				}
				break
			}
		}
	}
	for i, c := range cols {
		if !used[i] && !hidden(c.Name) {
			out = append(out, i)
		}
	}
	if len(out) == 0 && len(cols) > 0 {
		out = append(out, 0) // never an empty grid
	}
	return out
}

// drawnCols is the columns the grid draws: the frozen ones, then the
// rest from the scroll offset on.
func (s *dbConsoleState) drawnCols(cols []dbclient.Column) []int {
	vis := s.visibleCols(cols)
	l := s.colLayout(false)
	if l == nil {
		return vis
	}
	frozen := min(max(l.Frozen, 0), len(vis))
	rest := len(vis) - frozen
	off := min(max(l.off, 0), max(rest-1, 0))
	if off == 0 {
		return vis
	}
	return append(vis[:frozen:frozen], vis[frozen+off:]...)
}

// colWidth is the width the layout sets for a column (0: fit the cells).
func (s *dbConsoleState) colWidth(name string) int {
	if l := s.colLayout(false); l != nil {
		return l.Width[name]
	}
	return 0
}

// gridColumn is the result column of grid display column i (-1: none).
func (s *dbConsoleState) gridColumn(i int) int {
	if s.shown == nil || i < 0 {
		return -1
	}
	d := s.drawnCols(s.shown.result.Columns)
	if i >= len(d) {
		return -1
	}
	return d[i]
}

// visPos is the place of display column i in the visible list.
func (s *dbConsoleState) visPos(i int) int {
	l := s.colLayout(false)
	if l == nil || i < l.Frozen {
		return i
	}
	vis := s.visibleCols(s.shown.result.Columns)
	frozen := min(max(l.Frozen, 0), len(vis))
	return i + min(max(l.off, 0), max(len(vis)-frozen-1, 0))
}

// databaseColumnsKey handles the column keys of the grid; col is the
// display column under the cursor.
func (m *Model) databaseColumnsKey(key string, col int) (tea.Cmd, bool) {
	switch key {
	case "H", "O", "<", ">", "F", "(", ")", "{", "}", "g=":
	default:
		return nil, false
	}
	s := m.dbConsole()
	r := s.shown
	if r == nil || r.redis != nil || r.plan != nil || r.result.Err != nil || len(r.result.Columns) == 0 || s.trans || s.view != dbViewGrid {
		return nil, true
	}
	cols := r.result.Columns
	if key == "O" {
		return m.databaseColumnsPickerOpen(), true
	}
	if key == "g=" {
		k, table := dbColKey(r)
		if l := s.colLayout(false); l == nil || l.empty() && l.off == 0 {
			return nil, true
		}
		if table {
			delete(s.colStore.saved, k)
		} else {
			delete(s.colStore.session, k)
		}
		return m.databaseColumnsChanged(table), true
	}
	vis := s.visibleCols(cols)
	if col < 0 || col >= len(s.drawnCols(cols)) {
		return nil, true
	}
	pos := s.visPos(col)
	if pos >= len(vis) {
		return nil, true
	}
	name := cols[vis[pos]].Name
	l := s.colLayout(true)
	_, table := dbColKey(r)
	names := func(idx []int) []string {
		out := make([]string, 0, len(cols))
		for _, i := range idx {
			out = append(out, cols[i].Name)
		}
		return out
	}
	switch key {
	case "H":
		if len(vis) <= 1 {
			return m.pushToastDetail(toast.Info, "The last column stays", "O shows the hidden columns again."), true
		}
		l.Order = append(names(vis), l.Hidden...)
		l.Hidden = append(l.Hidden, name)
		l.Frozen = min(l.Frozen, len(vis)-1)
	case "<", ">":
		to := pos - 1
		if key == ">" {
			to = pos + 1
		}
		if to < 0 || to >= len(vis) {
			return nil, true
		}
		vis = slices.Clone(vis)
		vis[pos], vis[to] = vis[to], vis[pos]
		l.Order = append(names(vis), l.Hidden...)
	case "F":
		if l.Frozen == pos+1 {
			l.Frozen = 0
		} else {
			l.Frozen = pos + 1
		}
		l.off = 0
	case "{":
		if l.off <= 0 {
			return nil, true
		}
		l.off--
		return m.databaseShowPage(), true
	case "}":
		frozen := min(max(l.Frozen, 0), len(vis))
		if l.off+1 >= len(vis)-frozen {
			return nil, true
		}
		l.off++
		return m.databaseShowPage(), true
	case "(", ")":
		step := dbColWidthStep
		if key == "(" {
			step = -step
		}
		m.databaseColumnGrow(l, name, vis[pos], step)
	}
	return m.databaseColumnsChanged(table), true
}

// databaseColumnGrow changes the width of result column i (named name) by
// delta cells, from the width the page gives it now; the width stays in
// dbColMinWidth … dbColMaxWidth.
func (m *Model) databaseColumnGrow(l *dbColLayout, name string, i, delta int) {
	w := l.Width[name]
	if w == 0 {
		w = m.databaseNaturalWidth(i)
	}
	if l.Width == nil {
		l.Width = map[string]int{}
	}
	l.Width[name] = dbClampColWidth(w + delta)
}

func dbClampColWidth(w int) int { return min(max(w, dbColMinWidth), dbColMaxWidth) }

func init() {
	registerNotifyHandler("termocode_db_resize", func(m *Model, args []any) tea.Cmd {
		return m.databaseColumnResize(notifyArgInt(args, 0), notifyArgInt(args, 1))
	})
}

// databaseColumnResize is a mouse drag of the right edge of display
// column col by dx cells (DBX-8).
func (m *Model) databaseColumnResize(col, dx int) tea.Cmd {
	s := m.dbConsole()
	r := s.shown
	if dx == 0 || s.running != nil || r == nil || r.redis != nil || r.plan != nil || r.result.Err != nil || len(r.result.Columns) == 0 || s.trans || s.view != dbViewGrid {
		return nil
	}
	cols := r.result.Columns
	vis := s.visibleCols(cols)
	if col < 0 || col >= len(s.drawnCols(cols)) {
		return nil
	}
	pos := s.visPos(col)
	if pos >= len(vis) {
		return nil
	}
	l := s.colLayout(true)
	_, table := dbColKey(r)
	m.databaseColumnGrow(l, cols[vis[pos]].Name, vis[pos], dx)
	s.keepCursor = true
	return m.databaseColumnsChanged(table)
}

// databaseNaturalWidth is the width the grid gives result column i on
// this page.
func (m *Model) databaseNaturalWidth(i int) int {
	p := m.databasePage()
	s := m.dbConsole()
	for j, c := range s.drawnCols(s.shown.result.Columns) {
		if c == i && j < len(p.widths) {
			return p.widths[j]
		}
	}
	return dbMaxCellWidth
}

func (m *Model) databaseColumnsChanged(table bool) tea.Cmd {
	s := m.dbConsole()
	if table {
		if err := s.colStore.save(); err != nil {
			return tea.Batch(m.databaseShowPage(), m.pushToastDetail(toast.Errr, "Could not save the column layout", sanitizeDBText(err.Error())))
		}
	}
	return m.databaseShowPage()
}

func (m *Model) databaseColumnsPickerOpen() tea.Cmd {
	s := m.dbConsole()
	cols := s.shown.result.Columns
	l := s.colLayout(false)
	vis := s.visibleCols(cols)
	var items []picker.Item
	for i, c := range cols {
		hint := "shown"
		if !slices.Contains(vis, i) {
			hint = "hidden"
		} else if l != nil && slices.Index(vis, i) < l.Frozen {
			hint = "frozen"
		}
		items = append(items, picker.Item{ID: fmt.Sprint(i), Title: dbOneLine(c.Name), Hint: hint})
	}
	m.picker = picker.NewItems("Columns (select: show / hide)", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBColumns
	m.pickerOpen = true
	return nil
}

// databaseColumnsPicker toggles the column picked in the Columns picker.
func (m *Model) databaseColumnsPicker(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBColumns {
		return nil, false
	}
	s := m.dbConsole()
	r := s.shown
	var i int
	if _, err := fmt.Sscan(id, &i); err != nil || r == nil || i < 0 || i >= len(r.result.Columns) {
		return nil, true
	}
	cols := r.result.Columns
	name := cols[i].Name
	vis := s.visibleCols(cols)
	l := s.colLayout(true)
	_, table := dbColKey(r)
	if k := slices.Index(l.Hidden, name); k >= 0 {
		l.Hidden = slices.Delete(slices.Clone(l.Hidden), k, k+1)
	} else {
		if len(vis) <= 1 {
			return m.pushToastDetail(toast.Info, "The last column stays", "Show another column first."), true
		}
		l.Hidden = append(l.Hidden, name)
		l.Frozen = min(l.Frozen, len(vis)-1)
	}
	return m.databaseColumnsChanged(table), true
}
