package app

import (
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// PERF-5 perf guards (TERMOCODE_PERF=1, see package perfbudget): the
// Containers view's stats tick and context switch, against the fake
// engine only. .github/workflows/perf.yml runs them.

func medianMs(ds []time.Duration) float64 {
	s := slices.Clone(ds)
	slices.Sort(s)
	return float64(s[len(s)/2].Microseconds()/10) / 100
}

// One live-stats answer for 34 running containers (of 100) taken in,
// and the frame after it: the median of 30.
func TestPerfContainersStatsTick100(t *testing.T) {
	perfbudget.Require(t)
	st := perfHost()
	m := svcTestModel(st, 120, 50)
	rows := perfStats(st)
	var took []time.Duration
	for i := 0; i < 30; i++ {
		st.stats.polling = true
		t0 := time.Now()
		_ = m.onStatsResult(statsResultMsg{gen: st.stats.gen, rows: rows})
		_ = m.renderServicesSidebar(40, 50)
		took = append(took, time.Since(t0))
	}
	perfbudget.Check(t, "containers_stats_tick_100", medianMs(took))
}

// Switching the engine context with every section open: from the
// switch to every section listed again from the new context (fake
// engine, one process per call). The median of 5 switches.
func TestPerfContainersContextSwitch(t *testing.T) {
	perfbudget.Require(t)
	m, _ := ctxSession(t)
	st := m.services
	var took []time.Duration
	for i, name := range []string{"prod", "default", "prod", "default", "prod"} {
		t0 := time.Now()
		cmd := m.switchContext(name)
		if cmd == nil {
			t.Fatalf("switch %d to %s did nothing", i, name)
		}
		pumpUntilLoaded(t, m, cmd, func() bool {
			return st.containerDetected && st.hostLoaded["containers"] && st.hostLoaded["images"] &&
				st.hostLoaded["networks"] && st.hostLoaded["volumes"] && len(st.containerRows) > 0
		})
		took = append(took, time.Since(t0))
		st.stopHostWatch()
		if st.activeContext() != name {
			t.Fatalf("switch %d: active context %q, want %s", i, st.activeContext(), name)
		}
	}
	perfbudget.Check(t, "containers_context_switch", medianMs(took))
}

// pumpUntilLoaded runs cmd and the commands its answers give until done
// holds (no quiet wait after it: the guard times the loading).
func pumpUntilLoaded(t *testing.T, m *Model, cmd tea.Cmd, done func() bool) {
	t.Helper()
	results := make(chan tea.Msg, 256)
	start := func(c tea.Cmd) {
		if c != nil {
			go func() { results <- c() }()
		}
	}
	start(cmd)
	deadline := time.After(30 * time.Second)
	for !done() {
		select {
		case got := <-results:
			if b, ok := got.(tea.BatchMsg); ok {
				for _, c := range b {
					start(c)
				}
				continue
			}
			next, _ := m.handleServicesMsg(got)
			start(next)
		case <-deadline:
			t.Fatal("the sections did not load")
		}
	}
}
