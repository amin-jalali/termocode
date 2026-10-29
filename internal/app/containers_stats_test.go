package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
