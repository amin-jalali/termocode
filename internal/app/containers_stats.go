package app

import (
	"context"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Containers view: live CPU and memory (G-DST) ─────────────────────────
//
// One `stats --no-stream` call for every running container, every 2 s,
// only while the Containers view is on screen, the window is in the
// foreground and a running container is listed. Hiding the view, a
// blurred window or the last container stopping ends the chain (and
// cancels a call in flight): no timer, no process.

// statsCPUFloor is the smallest CPU scale of a graph, in percent.
const statsCPUFloor = 20

const (
	statsInterval = 2 * time.Second
	statsBackoff  = 10 * time.Second // after a failed call
)

type statsTickMsg struct{ gen uint64 }

type statsResultMsg struct {
	gen  uint64
	rows []containers.Stat
	err  error
}

// containerStats is the poller's state (UI goroutine only).
type containerStats struct {
	series   map[string]*containers.Series // by container ID
	detailID string                        // the selected running container whose chart rows are shown
	gen      uint64
	polling  bool // a fetch or a tick of the current gen is outstanding
	cancel   context.CancelFunc
	lastFail string        // why the last call failed ("" = it worked)
	delay    time.Duration // the wait before the next call
	off      bool          // a demo: never call the engine
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
	if st == nil || !st.visible || m.termBlurred || st.stats.off {
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
	m.statsRetarget()
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
	if msg.err != nil {
		// Engine not reachable, or `stats` not supported: say so once,
		// quietly, keep the old graphs and try again later.
		cs.lastFail, cs.delay = statsFailText(msg.err), statsBackoff
		st.touch()
		return statsTick(cs.gen, cs.delay)
	}
	cs.lastFail, cs.delay = "", statsInterval
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
	return statsTick(cs.gen, cs.delay)
}

// ── Formatting ───────────────────────────────────────────────────────────

// statsHumanBytes is a size for people: "512 B", "48 KB", "210 MB",
// "7.7 GB" (1024 steps, one decimal under 10).
func statsHumanBytes(v float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i > 0 && v < 10 {
		return fmt.Sprintf("%.1f %s", v, units[i])
	}
	return fmt.Sprintf("%.0f %s", v, units[i])
}

// statsPercent is "12%" at 10 and over, "0.3%" under.
func statsPercent(v float64) string {
	if v >= 10 {
		return fmt.Sprintf("%.0f%%", v)
	}
	return fmt.Sprintf("%.1f%%", v)
}

// statsRowText is the inline values of a container row: "CPU 12% · MEM
// 210 MB" (full) or "12% · 210 MB" (short). "" = nothing to show yet.
func statsRowText(s containers.Sample, full bool) string {
	var parts []string
	if s.HasCPU {
		p := statsPercent(s.CPU)
		if full {
			p = "CPU " + p
		}
		parts = append(parts, p)
	}
	if s.HasMem {
		p := statsHumanBytes(s.MemUsed)
		if full {
			p = "MEM " + p
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, " · ")
}

// statsOf is the newest sample of a container (ok false: none yet).
func (st *servicesState) statsOf(id string) (containers.Sample, bool) {
	return st.stats.series[id].Last()
}

const (
	statsFullMinWidth  = 62 // "CPU 12% · MEM 210 MB" in the row
	statsShortMinWidth = 44 // "12% · 210 MB"
)

// statsRowCol is the stats column of a running container's row (nil =
// none: no sample yet, or the sidebar is narrow).
func (st *servicesState) statsRowCol(c containers.Container, w int) *hostCol {
	if w < statsShortMinWidth || !strings.EqualFold(c.State, "running") {
		return nil
	}
	s, ok := st.statsOf(c.ID)
	if !ok {
		return nil
	}
	full := w >= statsFullMinWidth
	text := statsRowText(s, full)
	if text == "" {
		return nil
	}
	width := 12
	if full {
		width = 22
	}
	return &hostCol{text: text, width: width, right: true, col: theme.TextSecondary}
}

// ── Sparkline ────────────────────────────────────────────────────────────

var (
	statsBlocks      = []rune("▁▂▃▄▅▆▇█")
	statsBlocksASCII = []rune("_.:-=+*#") // the ascii icon mode
)

// statsSparkline draws the newest `width` values as one block char each,
// scaled so that `max` is a full block. Fewer values than `width` leave
// blank cells on the left (the line fills from the right, like a scope).
// A value above max is a full block; a negative one counts as 0.
func statsSparkline(vals []float64, max float64, width int) string {
	if width <= 0 {
		return ""
	}
	if len(vals) > width {
		vals = vals[len(vals)-width:]
	}
	blocks := statsBlocks
	if theme.CurrentIconMode == theme.IconModeASCII {
		blocks = statsBlocksASCII
	}
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", width-len(vals)))
	for _, v := range vals {
		level := 0
		if max > 0 && v > 0 {
			level = int(v / max * float64(len(blocks)))
			if level >= len(blocks) {
				level = len(blocks) - 1
			}
		}
		b.WriteRune(blocks[level])
	}
	return b.String()
}

// ── Detail: chart rows under the selected container ──────────────────────

// statsDetailRows are the four chart lines that follow container c's row
// while it is the selected one (and has a sample).
func (st *servicesState) statsDetailRows(c containers.Container, depth int) []svcRow {
	if st.stats.detailID == "" || st.stats.detailID != c.ID || !strings.EqualFold(c.State, "running") {
		return nil
	}
	if _, ok := st.statsOf(c.ID); !ok {
		return nil
	}
	out := make([]svcRow, 4)
	for i := range out {
		out[i] = svcRow{kind: svcRowStats, section: hostSection("containers"), hostID: c.ID, idx: i, depth: depth}
	}
	return out
}

// statsRetarget makes the chart rows follow the cursor: the row under
// the cursor, when it is a running container with a sample, shows them.
// The cursor keeps the same row when the chart rows move the list.
func (m *Model) statsRetarget() {
	st := m.services
	if st == nil {
		return
	}
	cs := &st.stats
	if cs.detailID == "" && len(cs.series) == 0 {
		return
	}
	rows := st.rows()
	want := ""
	var cur svcRow
	if st.cursor >= 0 && st.cursor < len(rows) {
		cur = rows[st.cursor]
		if cur.kind == svcRowHostItem && cur.host == hostContainer {
			if _, ok := st.statsOf(cur.hostID); ok {
				want = cur.hostID
			}
		}
	}
	if want == cs.detailID {
		return
	}
	cs.detailID = want
	if cur.kind == 0 && cur.section == "" {
		return
	}
	for i, r := range st.rows() {
		if r.kind == cur.kind && r.section == cur.section && r.host == cur.host && r.hostID == cur.hostID && r.name == cur.name && r.tag == cur.tag {
			st.cursor = i
			break
		}
	}
	st.touch()
}

// statsSeriesValues extracts one metric of a series.
func statsSeriesValues(s *containers.Series, f func(containers.Sample) float64) []float64 {
	if s == nil {
		return nil
	}
	out := make([]float64, len(s.Samples))
	for i, v := range s.Samples {
		out[i] = f(v)
	}
	return out
}

// renderStatsRow draws one chart line: label, sparkline, newest value.
func (st *servicesState) renderStatsRow(r svcRow, w int) string {
	bg := theme.BgSidebar
	row := lipgloss.NewStyle().Background(theme.LG(bg))
	fg := func(c theme.Color256) lipgloss.Style { return row.Foreground(theme.LG(c)) }
	series := st.stats.series[r.hostID]
	last, _ := series.Last()
	lead := 1 + 2*r.depth
	var label, value string
	var vals []float64
	var maxV float64
	spark := false
	switch r.idx {
	case 0:
		label, spark = "CPU", true
		vals = statsSeriesValues(series, func(s containers.Sample) float64 { return s.CPU })
		maxV = statsCPUFloor // a quiet container stays low; a busy one (or several cores) fills the line
		for _, v := range vals {
			maxV = max(maxV, v)
		}
		if last.HasCPU {
			value = statsPercent(last.CPU)
		} else {
			value = "--"
		}
	case 1:
		label, spark = "MEM", true
		vals = statsSeriesValues(series, func(s containers.Sample) float64 { return s.MemUsed })
		maxV = last.MemLimit
		if maxV <= 0 {
			for _, v := range vals {
				maxV = max(maxV, v)
			}
		}
		if last.HasMem {
			value = statsHumanBytes(last.MemUsed)
			if last.MemLimit > 0 {
				value += "/" + statsHumanBytes(last.MemLimit)
			}
		} else {
			value = "--"
		}
	case 2:
		label = "NET"
		if last.HasNet {
			value = "in " + statsHumanBytes(last.NetRx) + " out " + statsHumanBytes(last.NetTx)
		} else {
			value = "--"
		}
	default:
		label = "BLK"
		if last.HasBlk {
			value = "r " + statsHumanBytes(last.BlkRead) + " w " + statsHumanBytes(last.BlkWrite)
		} else {
			value = "--"
		}
	}
	var b strings.Builder
	b.WriteString(row.Render(strings.Repeat(" ", lead)))
	b.WriteString(fg(theme.TextMuted).Render(label + " "))
	used := lead + len(label) + 1
	avail := w - used - 1
	if spark {
		// The graph needs a few cells; drop the "/limit" before the graph.
		if avail-runewidth.StringWidth(value) < 6 {
			value, _, _ = strings.Cut(value, "/")
		}
		sw := min(avail-runewidth.StringWidth(value)-1, containers.StatWindow)
		if sw >= 4 {
			b.WriteString(fg(theme.GitAdded).Render(statsSparkline(vals, maxV, sw)))
			b.WriteString(row.Render(" "))
			used += sw + 1
		}
	}
	value = runewidth.Truncate(value, max(w-used-1, 1), "…")
	b.WriteString(fg(theme.TextSecondary).Render(value))
	used += runewidth.StringWidth(value)
	return clampSidebarRow(b.String(), used, w, row)
}

// statsFailText is the one-line reason shown under the engine line.
func statsFailText(err error) string {
	l := errLine(err.Error())
	if l == "" {
		l = "no answer"
	}
	return "stats unavailable: " + l
}

// statsDemo fills the demo's running containers with a minute of canned
// samples and puts the cursor on the first, so its charts show.
func (m *Model) statsDemo() {
	st := m.services
	st.stats.series = map[string]*containers.Series{}
	// id → CPU base, memory base (MB), memory limit (GB)
	for _, d := range []struct {
		id        string
		cpu, mem  float64
		limitGB   float64
		wave, amp int
	}{
		{"9f3c2e1a", 18, 210, 2, 9, 14},
		{"a1b2c3d4", 3, 24, 1, 5, 2},
		{"f3e4d5c6", 41, 380, 2, 7, 25},
		{"e4d5c6b7", 9, 150, 2, 11, 6},
		{"aabbccdd", 1, 12, 1, 3, 1},
	} {
		se := &containers.Series{}
		for i := 0; i < containers.StatWindow; i++ {
			w := float64((i*d.wave)%(2*d.amp+1) - d.amp) // a zig-zag, no randomness
			se.Push(containers.Sample{
				CPU: max(d.cpu+w, 0.1), HasCPU: true,
				MemUsed: (d.mem + float64(i)/3) * 1048576, MemLimit: d.limitGB * (1 << 30), HasMem: true,
				NetRx: float64(1200 + 90*i), NetTx: float64(3400 + 40*i), HasNet: true,
				BlkRead: 0, BlkWrite: float64(8190 + 512*i), HasBlk: true,
			})
		}
		st.stats.series[d.id] = se
	}
	for i, r := range st.rows() {
		if r.kind == svcRowHostItem && r.host == hostContainer && r.hostID == "9f3c2e1a" {
			st.cursor = i
			break
		}
	}
	m.statsRetarget()
}
