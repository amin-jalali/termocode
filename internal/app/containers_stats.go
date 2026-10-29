package app

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
)

// ── Containers view: live CPU and memory (G-DST) ─────────────────────────
//
// One `stats --no-stream` call for every running container, every 2 s,
// only while the Containers view is on screen, the window is in the
// foreground and a running container is listed. Hiding the view, a
// blurred window or the last container stopping ends the chain (and
// cancels a call in flight): no timer, no process.

const statsInterval = 2 * time.Second

type statsTickMsg struct{ gen uint64 }

type statsResultMsg struct {
	gen  uint64
	rows []containers.Stat
	err  error
}

// containerStats is the poller's state (UI goroutine only).
type containerStats struct {
	series   map[string]*containers.Series // by container ID
	gen      uint64
	polling  bool // a fetch or a tick of the current gen is outstanding
	cancel   context.CancelFunc
	lastFail string
}

// runningContainerIDs lists the containers that are running now.
func (st *servicesState) runningContainerIDs() []string {
	var ids []string
	for _, c := range st.containerRows {
		if strings.EqualFold(c.State, "running") {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// statsWanted: should the poller run right now?
func (m *Model) statsWanted() bool {
	st := m.services
	if st == nil || !st.visible || m.termBlurred {
		return false
	}
	if !st.containerEngine.Reachable || st.containerDetecting || !st.hostSectionVisible("containers") {
		return false
	}
	return len(st.runningContainerIDs()) > 0
}

// statsSync starts or ends the poller to match statsWanted. Called after
// every update; cheap when nothing changed.
func (m *Model) statsSync() tea.Cmd {
	st := m.services
	if st == nil {
		return nil
	}
	cs := &st.stats
	want := m.statsWanted()
	switch {
	case want && !cs.polling:
		cs.polling = true
		cs.gen++
		return m.statsFetch()
	case !want && cs.polling:
		cs.stop()
	}
	return nil
}

// stop ends the chain: a late result or tick of this gen is dropped.
func (cs *containerStats) stop() {
	cs.polling = false
	cs.gen++
	if cs.cancel != nil {
		cs.cancel()
		cs.cancel = nil
	}
}

// statsFetch runs one stats call for the running containers.
func (m *Model) statsFetch() tea.Cmd {
	st := m.services
	cs := &st.stats
	ids := st.runningContainerIDs()
	if len(ids) == 0 {
		return nil
	}
	if cs.cancel != nil {
		cs.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cs.cancel = cancel
	gen, e := cs.gen, st.containerEngine
	return func() tea.Msg {
		rows, err := e.Stats(ctx, ids...)
		return statsResultMsg{gen: gen, rows: rows, err: err}
	}
}

func statsTick(gen uint64, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return statsTickMsg{gen: gen} })
}

func (m *Model) onStatsTick(msg statsTickMsg) tea.Cmd {
	st := m.services
	if st == nil || msg.gen != st.stats.gen || !st.stats.polling {
		return nil
	}
	if !m.statsWanted() {
		st.stats.stop()
		return nil
	}
	return m.statsFetch()
}

func (m *Model) onStatsResult(msg statsResultMsg) tea.Cmd {
	st := m.services
	if st == nil || msg.gen != st.stats.gen || !st.stats.polling {
		return nil
	}
	cs := &st.stats
	if cs.cancel != nil {
		cs.cancel()
		cs.cancel = nil
	}
	if cs.series == nil {
		cs.series = map[string]*containers.Series{}
	}
	live := map[string]bool{}
	for _, c := range st.containerRows {
		if !strings.EqualFold(c.State, "running") {
			continue
		}
		live[c.ID] = true
		for _, row := range msg.rows {
			if row.Matches(c) {
				if cs.series[c.ID] == nil {
					cs.series[c.ID] = &containers.Series{}
				}
				cs.series[c.ID].Push(containers.SampleOf(row))
				break
			}
		}
	}
	for id := range cs.series { // a stopped or removed container keeps nothing
		if !live[id] {
			delete(cs.series, id)
		}
	}
	st.touch()
	return statsTick(cs.gen, statsInterval)
}
