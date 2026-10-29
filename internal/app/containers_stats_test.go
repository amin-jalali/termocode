package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// statsFake is hostFake with a stats answer for c1 and c2.
func statsFake(t *testing.T) *servicestest.Fake {
	t.Helper()
	f := hostFake(t)
	f.Set(t, "stats.json", `{"Container":"c1","ID":"c1","Name":"termocode-postgres-1","CPUPerc":"12.50%","MemUsage":"210MiB / 2GiB","MemPerc":"10.2%","NetIO":"1.2kB / 3.4kB","BlockIO":"0B / 8.19kB","PIDs":"12"}
{"Container":"c2","ID":"c2","Name":"termocode-redis-1","CPUPerc":"0.30%","MemUsage":"8MiB / 2GiB","MemPerc":"0.4%","NetIO":"--","BlockIO":"--","PIDs":"4"}`)
	return f
}

func statsModel(t *testing.T) (*Model, *servicesState) {
	t.Helper()
	st := svcHostState()
	st.collapsed[hostSection("containers")] = false
	m := svcTestModel(st, 150, 42)
	return &m, st
}

// statsRunFetch runs a fetch command and applies its result. It returns
// the command the result asks for (the next tick; never run in a test).
func statsRunFetch(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("no stats command")
	}
	res, ok := cmd().(statsResultMsg)
	if !ok {
		t.Fatalf("not a stats result")
	}
	return m.onStatsResult(res)
}

func TestStatsPollStartsWithOneCallForRunningContainers(t *testing.T) {
	f := statsFake(t)
	m, st := statsModel(t)
	cmd := m.statsSync()
	if next := statsRunFetch(t, m, cmd); next == nil {
		t.Error("no next tick scheduled")
	}
	if n := f.Count("stats"); n != 1 {
		t.Fatalf("stats calls = %d, want 1", n)
	}
	call := ""
	for _, c := range f.Calls() {
		if strings.HasPrefix(c, "stats") {
			call = c
		}
	}
	if !strings.Contains(call, "--no-stream") || !strings.Contains(call, "{{json .}}") {
		t.Errorf("call = %q", call)
	}
	for _, id := range []string{"c1", "c2", "c4", "c5"} {
		if !strings.Contains(call, id) {
			t.Errorf("running %s missing in %q", id, call)
		}
	}
	for _, id := range []string{"c3", "c6"} { // exited and paused: no stats
		if strings.Contains(call, id) {
			t.Errorf("not running %s in %q", id, call)
		}
	}
	if s := st.stats.series["c1"]; s == nil || len(s.Samples) != 1 || s.Samples[0].CPU != 12.5 {
		t.Errorf("c1 series = %+v", s)
	}
	if again := m.statsSync(); again != nil {
		t.Error("a second sync started a second chain")
	}
}

func TestStatsNotPolledWhenNotWanted(t *testing.T) {
	f := statsFake(t)
	for name, tweak := range map[string]func(m *Model, st *servicesState){
		"hidden":         func(m *Model, st *servicesState) { st.visible = false },
		"window blurred": func(m *Model, st *servicesState) { m.termBlurred = true },
		"section folded": func(m *Model, st *servicesState) { st.collapsed[hostSection("containers")] = true },
		"engine down":    func(m *Model, st *servicesState) { st.containerEngine.Reachable = false },
		"none running": func(m *Model, st *servicesState) {
			for i := range st.containerRows {
				st.containerRows[i].State = "exited"
			}
		},
	} {
		m, st := statsModel(t)
		tweak(m, st)
		if cmd := m.statsSync(); cmd != nil {
			t.Errorf("%s: a stats call was started", name)
		}
	}
	if n := f.Count("stats"); n != 0 {
		t.Errorf("stats calls = %d, want 0", n)
	}
}

func TestStatsStopWhenViewHides(t *testing.T) {
	f := statsFake(t)
	m, st := statsModel(t)
	statsRunFetch(t, m, m.statsSync())
	gen := st.stats.gen
	st.visible = false
	if cmd := m.statsSync(); cmd != nil {
		t.Error("sync restarted a hidden view")
	}
	if st.stats.polling {
		t.Error("still polling after the view hid")
	}
	if cmd := m.onStatsTick(statsTickMsg{gen: gen}); cmd != nil {
		t.Error("a late tick fetched")
	}
	if n := f.Count("stats"); n != 1 {
		t.Errorf("stats calls = %d, want 1", n)
	}
	// Shown again: a fresh chain starts.
	st.visible = true
	if cmd := m.statsSync(); cmd == nil {
		t.Error("no restart when shown again")
	}
}

func TestStatsTickFetchesAgainAndStopsWhenBlurred(t *testing.T) {
	f := statsFake(t)
	m, st := statsModel(t)
	statsRunFetch(t, m, m.statsSync())
	statsRunFetch(t, m, m.onStatsTick(statsTickMsg{gen: st.stats.gen}))
	if n := f.Count("stats"); n != 2 {
		t.Fatalf("stats calls = %d, want 2", n)
	}
	if got := len(st.stats.series["c1"].Samples); got != 2 {
		t.Errorf("samples = %d, want 2", got)
	}
	m.termBlurred = true
	if cmd := m.onStatsTick(statsTickMsg{gen: st.stats.gen}); cmd != nil {
		t.Error("tick fetched in a blurred window")
	}
	if st.stats.polling {
		t.Error("polling in a blurred window")
	}
}

func TestStatsUsesSessionContext(t *testing.T) {
	f := statsFake(t)
	m, st := statsModel(t)
	st.containerEngine.Context = "prod"
	statsRunFetch(t, m, m.statsSync())
	found := false
	for _, c := range f.CtxCalls() {
		if strings.HasPrefix(c, "prod|stats") {
			found = true
		}
	}
	if !found {
		t.Errorf("stats did not run in context prod: %q", f.CtxCalls())
	}
}

func TestStatsStoppedContainerDropsItsSeries(t *testing.T) {
	statsFake(t)
	m, st := statsModel(t)
	statsRunFetch(t, m, m.statsSync())
	if st.stats.series["c2"] == nil {
		t.Fatal("c2 has no series")
	}
	st.containerRows[1].State = "exited" // c2 stopped
	statsRunFetch(t, m, m.onStatsTick(statsTickMsg{gen: st.stats.gen}))
	if st.stats.series["c2"] != nil {
		t.Error("stopped container kept its series")
	}
}

func TestStatsHumanFormats(t *testing.T) {
	for in, want := range map[float64]string{0: "0 B", 512: "512 B", 2048: "2.0 KB", 210 * 1048576: "210 MB", 8.2 * 1048576: "8.2 MB", 7.7 * (1 << 30): "7.7 GB"} {
		if got := statsHumanBytes(in); got != want {
			t.Errorf("statsHumanBytes(%v) = %q, want %q", in, got, want)
		}
	}
	if statsPercent(12.4) != "12%" || statsPercent(0.31) != "0.3%" || statsPercent(100) != "100%" {
		t.Errorf("percent: %s %s %s", statsPercent(12.4), statsPercent(0.31), statsPercent(100))
	}
}

// statsSeeded is the host state with one sample for c1 (postgres, running).
func statsSeeded() *servicesState {
	st := svcHostState()
	st.collapsed[hostSection("containers")] = false
	st.stats.series = map[string]*containers.Series{"c1": {}}
	st.stats.series["c1"].Push(containers.Sample{CPU: 12.4, HasCPU: true, MemUsed: 210 * 1048576, MemLimit: 2 << 30, HasMem: true})
	return st
}

func TestStatsRowValuesByWidth(t *testing.T) {
	st := statsSeeded()
	line := func(w int) string { return lineWith(svcRender(st, w, 40), "postgres-1") }
	if l := line(70); !strings.Contains(l, "CPU 12% · MEM 210 MB") {
		t.Errorf("wide row = %q", l)
	}
	if l := line(50); !strings.Contains(l, "12% · 210 MB") || strings.Contains(l, "CPU") {
		t.Errorf("short row = %q", l)
	}
	if l := line(29); strings.Contains(l, "MB") || strings.Contains(l, "%") {
		t.Errorf("narrow row = %q", l)
	}
	// Containers that are stopped, or have no sample yet, show nothing.
	for _, name := range []string{"api-1", "redis-1", "builder"} {
		if l := lineWith(svcRender(st, 70, 40), name); l == "" || strings.Contains(l, "MB") || strings.Contains(l, "CPU") {
			t.Errorf("%s row = %q", name, l)
		}
	}
	st.containerRows[0].State = "exited" // a stale sample of a stopped container
	if l := line(70); strings.Contains(l, "MB") {
		t.Errorf("stopped row shows stats: %q", l)
	}
}

func TestStatsSparkline(t *testing.T) {
	cases := []struct {
		name  string
		vals  []float64
		max   float64
		width int
		want  string
	}{
		{"scale", []float64{0, 12.5, 25, 50, 75, 100}, 100, 6, "▁▂▃▅▇█"},
		{"left blank when short", []float64{100, 100}, 100, 5, "   ██"},
		{"newest kept", []float64{0, 0, 100, 100}, 100, 2, "██"},
		{"over max is full", []float64{250}, 100, 1, "█"},
		{"zero max is flat", []float64{5, 9}, 0, 2, "▁▁"},
		{"negative is flat", []float64{-4}, 100, 1, "▁"},
		{"no width", []float64{1}, 1, 0, ""},
		{"empty", nil, 100, 3, "   "},
	}
	for _, c := range cases {
		if got := statsSparkline(c.vals, c.max, c.width); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// statsDetailState is statsSeeded with a short history for c1 and the
// cursor on the postgres container.
func statsDetailState(t *testing.T) (*Model, *servicesState) {
	t.Helper()
	st := statsSeeded()
	for _, cpu := range []float64{5, 20, 40, 80} {
		st.stats.series["c1"].Push(containers.Sample{CPU: cpu, HasCPU: true, MemUsed: 210 * 1048576, MemLimit: 2 << 30, HasMem: true,
			NetRx: 1200, NetTx: 3400, HasNet: true, BlkRead: 0, BlkWrite: 8190, HasBlk: true})
	}
	m := svcTestModel(st, 150, 42)
	for i, r := range st.rows() {
		if r.kind == svcRowHostItem && r.hostID == "c1" {
			st.cursor = i
		}
	}
	m.statsRetarget()
	return &m, st
}

func TestStatsDetailShowsCharts(t *testing.T) {
	m, st := statsDetailState(t)
	lines := strings.Split(ansi.Strip(m.renderServicesSidebarRaw(40, 40, true, svcTestNow, -1, -1)), "\n")
	cpu, mem, net, blk := lineWith(lines, "CPU "), lineWith(lines, "MEM "), lineWith(lines, "NET "), lineWith(lines, "BLK ")
	if !strings.Contains(cpu, "▇") && !strings.Contains(cpu, "█") || !strings.Contains(cpu, "80%") {
		t.Errorf("cpu line = %q", cpu)
	}
	if !strings.Contains(mem, "210 MB/2.0 GB") || !strings.Contains(mem, "▁▁▁") {
		t.Errorf("mem line = %q", mem)
	}
	if !strings.Contains(net, "in 1.2 KB") || !strings.Contains(net, "out 3.3 KB") {
		t.Errorf("net line = %q", net)
	}
	if !strings.Contains(blk, "w 8.0 KB") {
		t.Errorf("blk line = %q", blk)
	}
	// The chart lines sit right under the selected row.
	rows := st.rows()
	if rows[st.cursor].hostID != "c1" || rows[st.cursor+1].kind != svcRowStats || rows[st.cursor+4].kind != svcRowStats || rows[st.cursor+5].kind == svcRowStats {
		t.Errorf("chart rows not under the cursor row")
	}
	// Other rows have none; they cannot be selected.
	n := 0
	for _, r := range rows {
		if r.kind == svcRowStats {
			n++
			if r.selectable() {
				t.Error("chart row is selectable")
			}
		}
	}
	if n != 4 {
		t.Errorf("chart rows = %d", n)
	}
}

func TestStatsDetailFollowsCursorAndKeepsRow(t *testing.T) {
	m, st := statsDetailState(t)
	st.stats.series["c2"] = &containers.Series{}
	st.stats.series["c2"].Push(containers.Sample{CPU: 1, HasCPU: true, MemUsed: 1 << 20, HasMem: true})
	m.servicesMove(1) // skips the chart rows: next is the next container
	m.statsRetarget()
	row, _ := m.servicesCurrentRow()
	if row.hostID == "c1" || row.kind != svcRowHostItem {
		t.Fatalf("cursor row = %+v", row)
	}
	if st.stats.detailID != row.hostID {
		t.Errorf("detail = %q, cursor on %q", st.stats.detailID, row.hostID)
	}
	for _, r := range st.rows() {
		if r.kind == svcRowStats && r.hostID != st.stats.detailID {
			t.Errorf("chart rows of %s remain", r.hostID)
		}
	}
	// Moving off every container drops the chart rows.
	st.cursor = 0
	m.statsRetarget()
	for _, r := range st.rows() {
		if r.kind == svcRowStats {
			t.Fatal("chart rows stay with the cursor on a header")
		}
	}
}

func TestStatsDetailNeedsRunningAndSample(t *testing.T) {
	m, st := statsDetailState(t)
	st.containerRows[0].State = "exited"
	m.statsRetarget()
	for _, r := range st.rows() {
		if r.kind == svcRowStats {
			t.Fatal("chart rows of a stopped container")
		}
	}
}

func TestStatsDetailNarrowWidthKeepsLinesInside(t *testing.T) {
	m, _ := statsDetailState(t)
	for _, w := range []int{20, 29, 80} {
		for _, l := range strings.Split(ansi.Strip(m.renderServicesSidebarRaw(w, 40, true, svcTestNow, -1, -1)), "\n") {
			if runewidth.StringWidth(l) > w {
				t.Errorf("w=%d line too wide: %q", w, l)
			}
		}
	}
}
