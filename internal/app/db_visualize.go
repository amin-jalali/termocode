package app

import (
	"errors"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Visualize (DBX-35) on a result page: M colours the numeric cells of the
// grid by their value (a heatmap, from cool to hot), B draws a bar or a
// line chart of a numeric column in a read-only buffer, and v shows a
// geometry value as WKT and GeoJSON text (db_value.go).

const pickerKindDBChart pickerKindEnum = 5341

// dbHeatLevels are the highlight groups of the heatmap, lowest first.
var dbHeatLevels = [5]string{"TermocodeDbHeat0", "TermocodeDbHeat1", "TermocodeDbHeat2", "TermocodeDbHeat3", "TermocodeDbHeat4"}

// dbChartMaxRows caps the rows a chart draws (the loaded rows).
const dbChartMaxRows = 500

// dbHeatGrid is, per result column, the level of each row of the page
// (nil: not a numeric column).
func dbHeatGrid(page dbclient.Result) [][]int {
	out := make([][]int, len(page.Columns))
	for _, c := range dbclient.NumericColumns(page) {
		out[c] = dbclient.HeatLevels(page, c, len(dbHeatLevels))
	}
	return out
}

// databaseToggleHeat is M: the heatmap on or off for every result.
func (m *Model) databaseToggleHeat() tea.Cmd {
	s := m.dbConsole()
	s.heat = !s.heat
	state := "off"
	if s.heat {
		state = "on"
	}
	return tea.Batch(m.databaseShowPage(), m.pushToastDetail(toast.Info, "Heatmap "+state, "M turns it "+map[bool]string{true: "off", false: "on"}[s.heat]+"."))
}

// dbChartLabelCol is the first column that is not numeric (-1: none).
func dbChartLabelCol(r dbclient.Result, numeric []int) int {
	isNum := map[int]bool{}
	for _, c := range numeric {
		isNum[c] = true
	}
	for i := range r.Columns {
		if !isNum[i] {
			return i
		}
	}
	return -1
}

// dbChartItems are the charts a result can draw: a bar and a line chart per
// numeric column.
func dbChartItems(r dbclient.Result) []picker.Item {
	numeric := dbclient.NumericColumns(r)
	label := dbChartLabelCol(r, numeric)
	var items []picker.Item
	for _, c := range numeric {
		name := sanitizeDBText(r.Columns[c].Name)
		bar := "Bar chart: " + name
		if label >= 0 {
			bar += " by " + sanitizeDBText(r.Columns[label].Name)
		}
		items = append(items,
			picker.Item{ID: "bar:" + strconv.Itoa(c), Title: bar, Hint: "one bar per row"},
			picker.Item{ID: "line:" + strconv.Itoa(c), Title: "Line chart: " + name, Hint: "the rows in order, braille dots"})
	}
	return items
}

// dbChartText draws the chart id ("bar:<col>" or "line:<col>") of r in
// width cells.
func dbChartText(r dbclient.Result, id string, width int) ([]string, error) {
	kind, num, _ := strings.Cut(id, ":")
	col, err := strconv.Atoi(num)
	numeric := dbclient.NumericColumns(r)
	ok := false
	for _, c := range numeric {
		ok = ok || c == col
	}
	if err != nil || !ok {
		return nil, errors.New("that column has no numbers to draw")
	}
	if len(r.Rows) > dbChartMaxRows {
		r.Rows = r.Rows[:dbChartMaxRows]
	}
	name := sanitizeDBText(r.Columns[col].Name)
	head := []string{"-- " + name + " · " + dbPlural(len(r.Rows), "row"), ""}
	switch kind {
	case "bar":
		label := dbChartLabelCol(r, numeric)
		if label < 0 { // no text column: the row number is the label
			r = dbChartNumbered(r)
			label, col = 0, col+1
		}
		lines := dbclient.BarChart(r, label, col, max(width-30, 10))
		for i := range lines {
			lines[i] = sanitizeOutputLine(lines[i])
		}
		return append(head, lines...), nil
	case "line":
		vals := dbclient.ColumnValues(r, col)
		if len(vals) == 0 {
			return nil, errors.New("that column has no numbers to draw")
		}
		lo, hi := vals[0], vals[0]
		for _, v := range vals {
			lo, hi = min(lo, v), max(hi, v)
		}
		head = append(head, "max "+dbNum(hi))
		head = append(head, dbclient.BrailleLine(vals, max(width-4, 10), 12)...)
		return append(head, "min "+dbNum(lo)), nil
	}
	return nil, errors.New("unknown chart")
}

// dbChartNumbered puts the row number in front of every row.
func dbChartNumbered(r dbclient.Result) dbclient.Result {
	out := dbclient.Result{Columns: append([]dbclient.Column{{Name: "#"}}, r.Columns...)}
	for i, row := range r.Rows {
		out.Rows = append(out.Rows, append([]dbclient.Cell{{Text: strconv.Itoa(i + 1)}}, row...))
	}
	return out
}

// databaseChartStart is B: the chart picker for the result on the page.
func (m *Model) databaseChartStart() tea.Cmd {
	s := m.dbConsole()
	if s.shown == nil || s.shown.result.Err != nil || len(s.shown.result.Columns) == 0 {
		return m.pushToastDetail(toast.Info, "Nothing to draw", "Run a query that returns rows first.")
	}
	items := dbChartItems(s.displayedResult())
	if len(items) == 0 {
		return m.pushToastDetail(toast.Info, "No numeric column", "A chart needs a column with only numbers.")
	}
	m.picker = picker.NewItems("Chart of the result…", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBChart
	m.pickerOpen = true
	return nil
}

// databaseChartPicked draws the chart in a read-only buffer (q goes back).
func (m *Model) databaseChartPicked(id string) tea.Cmd {
	s := m.dbConsole()
	if s.shown == nil {
		return nil
	}
	lines, err := dbChartText(s.displayedResult(), id, m.databasePageWidth())
	if err != nil {
		return m.pushToastDetail(toast.Info, "Cannot draw this chart", err.Error())
	}
	if m.nvim == nil {
		return nil
	}
	m.ensureDBLua()
	m.ensureEditorWindowCurrent()
	_ = m.nvim.ExecLuaArgs(`_G._termocode_db_ddl(...)`, "SQL Chart: "+dbPageName(s.shown), lines)
	m.focus = FocusEditor
	return m.fetchStateCmd()
}
