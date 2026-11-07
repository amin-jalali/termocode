package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func dbVizResult() dbclient.Result {
	return dbclient.Result{
		Columns: []dbclient.Column{{Name: "city"}, {Name: "sales"}},
		Rows: [][]dbclient.Cell{
			{{Text: "Oslo"}, {Text: "10", Kind: "number"}},
			{{Text: "Rome"}, {Text: "55", Kind: "number"}},
			{{Text: "Lima"}, {Text: "100", Kind: "number"}},
		},
	}
}

// M colours the numeric cells of the grid by their value; off by default.
func TestDBHeatmapColoursNumericCells(t *testing.T) {
	heat := func(p dbPage) map[string]int {
		out := map[string]int{}
		for _, h := range p.hls {
			if g, _ := h["g"].(string); strings.HasPrefix(g, "TermocodeDbHeat") {
				out[g]++
			}
		}
		return out
	}
	withConfigDir(t)
	m := dbGridModel(dbVizResult())
	if got := heat(m.databasePage()); len(got) != 0 {
		t.Fatalf("heatmap on by default: %v", got)
	}
	m.dbConsole().heat = true
	got := heat(m.databasePage())
	if got["TermocodeDbHeat0"] != 1 || got["TermocodeDbHeat4"] != 1 || len(got) != 3 {
		t.Fatalf("heat spans: %v (want lowest, middle and highest level once)", got)
	}
	for i := range dbHeatLevels {
		if !strings.Contains(themeLuaBody, "TermocodeDbHeat"+string(rune('0'+i))) {
			t.Fatalf("no highlight group for level %d", i)
		}
	}
}

// V lists a bar and a line chart per numeric column; the chart text
// draws one bar per row.
func TestDBChartItemsAndText(t *testing.T) {
	r := dbVizResult()
	items := dbChartItems(r)
	if len(items) != 2 || items[0].ID != "bar:1" || items[1].ID != "line:1" {
		t.Fatalf("items: %+v", items)
	}
	if !strings.Contains(items[0].Title, "sales by city") {
		t.Fatalf("bar title: %q", items[0].Title)
	}
	lines, err := dbChartText(r, "bar:1", 40)
	if err != nil || len(lines) < 4 || !strings.Contains(strings.Join(lines, "\n"), "Lima") || !strings.Contains(strings.Join(lines, "\n"), "█") {
		t.Fatalf("bar chart: %v\n%s", err, strings.Join(lines, "\n"))
	}
	lines, err = dbChartText(r, "line:1", 40)
	if err != nil || len(lines) < 2 {
		t.Fatalf("line chart: %v %v", err, lines)
	}
	if _, err := dbChartText(r, "bar:0", 40); err == nil {
		t.Fatal("a text column is no chart value")
	}
	if items := dbChartItems(dbclient.Result{Columns: []dbclient.Column{{Name: "a"}}, Rows: [][]dbclient.Cell{{{Text: "x"}}}}); len(items) != 0 {
		t.Fatalf("no numbers, no charts: %+v", items)
	}
}

// v on a geometry value shows it as WKT and GeoJSON.
func TestDBCellViewShowsGeometryAsText(t *testing.T) {
	// POINT(1 2), little-endian WKB
	wkb := "0101000000000000000000F03F0000000000000040"
	got := dbCellView(dbclient.Cell{Text: wkb})
	if !strings.Contains(got, "POINT") || !strings.Contains(got, `"type":"Point"`) {
		t.Fatalf("geometry view:\n%s", got)
	}
	if got := dbCellView(dbclient.Cell{Text: "plain"}); got != "plain" {
		t.Fatalf("plain text changed: %q", got)
	}
}
