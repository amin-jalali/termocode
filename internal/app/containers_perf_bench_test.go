package app

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
)

// PERF-5: a scaled-up host (100 containers in 20 compose projects) with
// live stats for its running containers. Benchmarks for the Containers
// view's hot paths: building the rows, one frame after a key, one stats
// tick and the per-update stats check.

// perfHost is bigHost(100) with a full stats window for each running
// container and the cursor on a running container (its chart rows show).
func perfHost() *servicesState {
	st := bigHost(100)
	st.stats.series = map[string]*containers.Series{}
	for _, c := range st.containerRows {
		if c.State != "running" {
			continue
		}
		se := &containers.Series{}
		for i := 0; i < containers.StatWindow; i++ {
			se.Push(containers.Sample{CPU: float64(i % 30), HasCPU: true, MemUsed: float64(i) * 1e6, MemLimit: 1e9, HasMem: true, HasNet: true, HasBlk: true})
		}
		st.stats.series[c.ID] = se
	}
	st.touch()
	return st
}

// perfStats is one `stats --no-stream` answer for every running container.
func perfStats(st *servicesState) []containers.Stat {
	var out []containers.Stat
	for i, c := range st.containerRows {
		if c.State == "running" {
			out = append(out, containers.Stat{ID: c.ID, Name: c.Names[0], CPUPerc: fmt.Sprintf("%d.5%%", i%40),
				MemUsage: "48.2MiB / 1GiB", MemPerc: "4.7%", NetIO: "1.2kB / 3.4kB", BlockIO: "0B / 8.19kB", PIDs: "3"})
		}
	}
	return out
}

func BenchmarkContainersRows100(b *testing.B) {
	st := perfHost()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = st.rows()
	}
}

// One Down key in the focused sidebar and the frame after it.
func BenchmarkContainersKeyFrame100(b *testing.B) {
	st := perfHost()
	m := svcTestModel(st, 120, 50)
	m.focus = FocusExplorer
	n := len(st.rows())
	down := tea.KeyMsg{Type: tea.KeyDown}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if st.cursor >= n-2 {
			st.cursor = 0
		}
		m, _ = m.handleServicesSidebarKey(down)
		m.statsSync()
		_ = m.renderServicesSidebar(40, 50)
	}
}

// One stats answer taken in (2 s tick) and the frame after it.
func BenchmarkContainersStatsTick100(b *testing.B) {
	st := perfHost()
	m := svcTestModel(st, 120, 50)
	rows := perfStats(st)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st.stats.polling = true
		_ = m.onStatsResult(statsResultMsg{gen: st.stats.gen, rows: rows})
		_ = m.renderServicesSidebar(40, 50)
	}
}

// The stats check that runs after every update of the app (any key in
// any view), here with the Containers view hidden after it was used.
func BenchmarkContainersStatsSyncHidden100(b *testing.B) {
	st := perfHost()
	st.visible = false
	m := svcTestModel(st, 120, 50)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = m.statsSync()
	}
}

// The same check while the view is on screen and nothing changed.
func BenchmarkContainersStatsSyncVisible100(b *testing.B) {
	st := perfHost()
	m := svcTestModel(st, 120, 50)
	st.stats.polling = true
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = m.statsSync()
	}
}
