package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/theme"
)

// The AGENTS panel (U20 AP-9): a view of the chat's agent runs that
// shares the right-side slot with the chat (Alt+Shift+A switches the
// slot between the two; Alt+A always shows the chat). It is drawn from
// the run's assistant message (its step cards, ai.StoredStep), so a live
// run, a finished one and one from before a restart look the same; for
// a run that termocode's restart interrupted it replays the run's last
// snapshot (ai_runs.go) instead of the cancelled states.
//
//	 run r1 · plan (2/2)                       wave 3/4 · done 2/5
//	 ● 2 running · following
//	 ──────────────────────────────────────────────────────────────
//	 ┌ ▾ Left ───────────────────┐  ┌ ▾ Right ──────────────────┐
//	 │ ● running · quick         │  │ ● running · quick         │
//	 │ ▶ read_file nav.go        │  │ ✎ now · …the scoped wiring│
//	 │ 21 s · 1 turn · 1 tool    │  │ 21 s · 1 turn             │
//	 └───────────────────────────┘  └───────────────────────────┘
//	               └─────────────┬────────────┘
//	                             ▼
//	 Dependencies / Node details · Left / key hints
//
// Nothing ticks for the panel: a running node's time moves with the
// chat's 1 s activity tick, which runs only while a run goes and the
// slot is shown. The drawn rows are cached; an idle frame reuses them.

// aiAgentsState is the AGENTS view's state (in aiState).
type aiAgentsState struct {
	on        bool   // the chat slot shows AGENTS instead of the chat
	usage     bool   // ... as the usage tables (AI: Usage, ai_usage_view.go)
	runID     string // the run shown ("" = the running or the newest one)
	sel       string // the node the user selected (manual)
	manual    bool   // the user moved the selection: follow mode is off
	noDetails bool   // "d" hid the node details
	scroll    int    // body rows scrolled from the top
	free      bool   // the user scrolled: the selection may leave the view
	// runOf is the run whose node start times and turn counts start and
	// turns hold (the last run started in this session).
	runOf string
	start map[string]time.Time
	turns map[string]int
	// act is each running node's activity (the chat's aiActivity, with
	// its stall thresholds), by node id; a finished node has none.
	act map[string]*aiActivity
	// snaps are the loaded snapshots of interrupted runs by run id (nil:
	// none on disk); snapGen changes when one loads.
	snaps   map[string]*aiRunSnapshot
	snapGen int
	cache   aiAgentsCache
}

// aiAgentsKey is everything the drawn rows depend on.
type aiAgentsKey struct {
	runGen, msgs, snapGen int
	conv, runID, sel      string
	manual, noDetails     bool
	usage                 bool
	usageGen              int
	width                 int
	secs                  int64 // the clock second while a node runs (0 when none)
	rk                    theme.RenderKey
}

// aiAgentsCache keeps the drawn AGENTS rows between frames.
type aiAgentsCache struct {
	ok             bool
	key            aiAgentsKey
	head           []string // run line, status line, divider
	body           []string
	foot           string
	hits           []agentsHit
	selTop, selBot int // body rows of the selected card (-1: none)
}

// agentsHit is a card's place in the body rows.
type agentsHit struct {
	y0, y1, x0, x1 int // rows and columns, half-open
	id             string
}

// agentsBuilds counts rebuilds of the drawn rows (the idle perf guard).
var agentsBuilds atomic.Int64

// Card sizes: narrower than agentsCardMin per column means one column.
const (
	agentsCardMin = 22
	agentsCardMax = 36
	agentsCardGap = 2
	agentsHeadH   = 3 // run line, status line, divider
)

// agentsRun is one run as the panel draws it.
type agentsRun struct {
	idx, nth, of int // message index; the run's place among the chat's runs
	msg          *ai.StoredMessage
	live         bool // running or paused (its goroutines are going)
	snap         *aiRunSnapshot
	nodes        []agentsNode // in wave order
	layers       [][]int      // indexes into nodes
	edges        []agentdag.Edge
	byID         map[string]int
}

// agentsNode is one node of a drawn run.
type agentsNode struct {
	id, title string
	state     agentdag.State
	st        *ai.StoredStep // nil: a node only the snapshot knows
}

// ── Data ───────────────────────────────────────────────────────────────

// agentsRunIdxs are the message indexes of the chat's runs, oldest first.
func (s *aiState) agentsRunIdxs() []int {
	var out []int
	for i, msg := range s.conv.Messages {
		if msg.Run != nil && msg.Role == ai.StoredAssistant {
			out = append(out, i)
		}
	}
	return out
}

func aiRunLive(r *ai.StoredRun) bool {
	return r != nil && (r.State == ai.RunStateRunning || r.State == ai.RunStatePaused)
}

// agentsShown is the run the panel shows: the chosen one, else the
// running one, else the newest. ok=false when the chat has no run.
func (s *aiState) agentsShown() (idx, nth, of int, ok bool) {
	runs := s.agentsRunIdxs()
	if len(runs) == 0 {
		return 0, 0, 0, false
	}
	pick := len(runs) - 1
	found := false
	if s.agents.runID != "" {
		for k, i := range runs {
			if s.conv.Messages[i].Run.ID == s.agents.runID {
				pick, found = k, true
			}
		}
	}
	if !found {
		for k := len(runs) - 1; k >= 0; k-- {
			if aiRunLive(s.conv.Messages[runs[k]].Run) {
				pick = k
				break
			}
		}
	}
	return runs[pick], pick + 1, len(runs), true
}

// agentsData builds the shown run's graph.
func (s *aiState) agentsData() (agentsRun, bool) {
	idx, nth, of, ok := s.agentsShown()
	if !ok {
		return agentsRun{}, false
	}
	return s.agentsDataAt(idx, nth, of), true
}

// agentsDataAt builds the graph of the run of message idx.
func (s *aiState) agentsDataAt(idx, nth, of int) agentsRun {
	msg := &s.conv.Messages[idx]
	d := agentsRun{idx: idx, nth: nth, of: of, msg: msg, live: aiRunLive(msg.Run), byID: map[string]int{}}
	if msg.Run.State == ai.RunStateInterrupted {
		d.snap = s.agents.snaps[msg.Run.ID]
	}
	var nodes []agentsNode
	known := map[string]bool{}
	for k := range msg.Steps {
		st := &msg.Steps[k]
		title := st.Title
		if title == "" {
			title = st.ID
		}
		nodes = append(nodes, agentsNode{id: st.ID, title: agentdag.Clean(title), state: agentdag.State(st.State), st: st})
		known[st.ID] = true
	}
	graphRun := false
	for _, st := range msg.Steps {
		graphRun = graphRun || len(st.Deps) > 0 || st.Planned
		for _, dep := range st.Deps {
			d.edges = append(d.edges, agentdag.Edge{From: dep, To: st.ID})
		}
	}
	if d.snap != nil { // the replayed states and edges
		pos := map[string]int{}
		for i, n := range nodes {
			pos[n.id] = i
		}
		for _, sn := range d.snap.Run.Nodes {
			if i, ok := pos[sn.ID]; ok {
				nodes[i].state = sn.State
				continue
			}
			nodes = append(nodes, agentsNode{id: sn.ID, title: agentdag.Clean(sn.Title), state: sn.State})
			if nodes[len(nodes)-1].title == "" {
				nodes[len(nodes)-1].title = sn.ID
			}
			known[sn.ID] = true
		}
		if len(d.snap.Run.Edges) > 0 {
			d.edges, graphRun = d.snap.Run.Edges, true
		}
	}
	if !graphRun { // a plain pipeline: one path in step order
		ids := make([]string, len(nodes))
		for i, n := range nodes {
			ids[i] = n.id
		}
		d.edges = agentdag.Chain(ids...)
	}
	var edges []agentdag.Edge
	for _, e := range d.edges {
		if known[e.From] && known[e.To] {
			edges = append(edges, e)
		}
	}
	d.edges = edges
	// Waves: Kahn layers of the graph (each node its own wave when the
	// saved graph does not check out).
	g := agentdag.Run{ID: msg.Run.ID, Edges: d.edges}
	for _, n := range nodes {
		g.Nodes = append(g.Nodes, agentdag.Node{ID: n.id})
	}
	layers, err := agentdag.Layers(g)
	if err != nil {
		layers = nil
		for _, n := range nodes {
			layers = append(layers, []string{n.id})
		}
	}
	pos := map[string]int{}
	for i, n := range nodes {
		pos[n.id] = i
	}
	for _, l := range layers {
		var row []int
		for _, id := range l {
			d.byID[id] = len(d.nodes)
			row = append(row, len(d.nodes))
			d.nodes = append(d.nodes, nodes[pos[id]])
		}
		d.layers = append(d.layers, row)
	}
	return d
}

// progress is the wave the run is in (the first one not done), the
// number of waves, and how many nodes are done and running.
func (d agentsRun) progress() (wave, waves, done, running int) {
	waves = len(d.layers)
	wave = waves
	for li := len(d.layers) - 1; li >= 0; li-- {
		for _, i := range d.layers[li] {
			if !d.nodes[i].state.Done() {
				wave = li + 1
			}
		}
	}
	for _, n := range d.nodes {
		switch {
		case n.state.Done():
			done++
		case n.state == agentdag.StateRunning && d.live:
			running++
		}
	}
	return wave, waves, done, running
}

// aiRunWaveText is the chat run row of a running planned run:
// "wave 2/4 · done 1/5 · 2 running" ("" for a plain pipeline).
func (s *aiState) aiRunWaveText(i int) string {
	msg := s.conv.Messages[i]
	graph := false
	for _, st := range msg.Steps {
		graph = graph || len(st.Deps) > 0 || st.Planned
	}
	if !graph {
		return ""
	}
	d := s.agentsDataAt(i, 0, 0)
	wave, waves, done, running := d.progress()
	out := "wave " + strconv.Itoa(wave) + "/" + strconv.Itoa(waves) + " · done " + strconv.Itoa(done) + "/" + strconv.Itoa(len(d.nodes))
	if running > 0 {
		out += " · " + strconv.Itoa(running) + " running"
	}
	return out
}

// agentsSelected is the selected node's index (-1 when none): in follow
// mode the first running node, else the user's choice; with neither, the
// first failed node, the last node of a finished run or the first one
// not done.
func (s *aiState) agentsSelected(d agentsRun) int {
	if len(d.nodes) == 0 {
		return -1
	}
	if s.agents.manual {
		if i, ok := d.byID[s.agents.sel]; ok {
			return i
		}
	} else if d.live {
		for i, n := range d.nodes {
			if n.state == agentdag.StateRunning {
				return i
			}
		}
	}
	for i, n := range d.nodes {
		if n.state == agentdag.StateFailed {
			return i
		}
	}
	for i, n := range d.nodes {
		if !n.state.Done() {
			return i
		}
	}
	return len(d.nodes) - 1
}

// agentsExpanded reports whether a card is drawn expanded: the user's
// fold choice (shared with the chat's step card), else expanded until
// the node is done.
func (s *aiState) agentsExpanded(runID string, n agentsNode) bool {
	if v, ok := s.conv.Folded[aiCardID(runID, n.id)]; ok {
		return !v
	}
	return !n.state.Done()
}

// agentsTurns is a node's model requests: from its usage, else counted
// live (0 when unknown).
func (s *aiState) agentsTurns(d agentsRun, n agentsNode) int {
	if n.st != nil && n.st.Usage != nil && n.st.Usage.Requests > 0 {
		return n.st.Usage.Requests
	}
	if d.msg.Run.ID == s.agents.runOf {
		return s.agents.turns[n.id]
	}
	return 0
}

// agentsNodeStart / agentsNodeTurn record a live node's start and its
// model requests (onAIRunEvent).
func (s *aiState) agentsNodeStart(runID, node string, now time.Time) {
	if s.agents.runOf != runID {
		s.agents.runOf, s.agents.start, s.agents.turns = runID, map[string]time.Time{}, map[string]int{}
		s.agents.act = map[string]*aiActivity{}
	}
	s.agents.start[node] = now
	s.agents.turns[node] = 0
}

// agentsNodeEvent moves a node's activity with an agent event (start
// begins it); agentsNodeEnd drops it when the node ends.
func (s *aiState) agentsNodeEvent(runID, node string, ev ai.Event, now time.Time, start bool) {
	if s.agents.runOf != runID || s.agents.act == nil {
		return
	}
	a := s.agents.act[node]
	if a == nil || start {
		a = &aiActivity{}
		a.begin(now)
		s.agents.act[node] = a
	}
	if !start {
		a.onEvent(ev, now)
	}
}

func (s *aiState) agentsNodeEnd(runID, node string) {
	if s.agents.runOf == runID {
		delete(s.agents.act, node)
	}
}

// agentsNodeView is a running node's activity at now (ok=false: none).
func (s *aiState) agentsNodeView(runID, node string, now time.Time) (aiActivityView, bool) {
	if s.agents.runOf != runID || !s.streaming {
		return aiActivityView{}, false
	}
	a := s.agents.act[node]
	if a == nil {
		return aiActivityView{}, false
	}
	v := a.view(now)
	return v, v.phase != aiPhaseNone
}

// agentsStalled is the silent seconds of a stalled running node (ok
// false: not stalled).
func (s *aiState) agentsStalled(runID string, st agentdag.State, node string, now time.Time) (int, bool) {
	if st != agentdag.StateRunning {
		return 0, false
	}
	v, ok := s.agentsNodeView(runID, node, now)
	return v.secs, ok && v.stalled
}

// agentsStallSig names the stalled nodes (the transcript cache key: the
// step cards warn too). "" when none.
func (s *aiState) agentsStallSig(now time.Time) string {
	if !s.streaming || len(s.agents.act) == 0 {
		return ""
	}
	var ids []string
	for id, a := range s.agents.act {
		if a.view(now).stalled {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

// agSecs is "35s" or "1m 5s".
func agSecs(n int) string {
	if n < 60 {
		return strconv.Itoa(max(n, 0)) + "s"
	}
	return strconv.Itoa(n/60) + "m " + strconv.Itoa(n%60) + "s"
}

func (s *aiState) agentsNodeTurn(runID, node string) {
	if s.agents.runOf == runID && s.agents.turns != nil {
		s.agents.turns[node]++
	}
}

// ── Look ───────────────────────────────────────────────────────────────

type agentsGlyphSet struct {
	h, v, tl, tr, bl, br, teeDown, teeUp, teeRight, teeLeft, cross, arrow string
	to, from, tool, now, fail                                             string
}

func agentsGlyphs() agentsGlyphSet {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return agentsGlyphSet{"-", "|", "+", "+", "+", "+", "+", "+", "+", "+", "+", "v", "->", "<-", ">", "~", "x"}
	}
	return agentsGlyphSet{"─", "│", "┌", "┐", "└", "┘", "┬", "┴", "├", "┤", "┼", "▼", "→", "←", "▶", "✎", "✘"}
}

// agentsLook is a node state's glyph, word and colour. A "running" node
// of a run that no longer goes (a replayed snapshot) "was running".
func agentsLook(st agentdag.State, live bool) (glyph, label string, tok theme.Color256) {
	ascii := theme.CurrentIconMode == theme.IconModeASCII
	pick := func(u, a string) string {
		if ascii {
			return a
		}
		return u
	}
	switch st {
	case agentdag.StateRunning, agentdag.StateScheduled:
		if !live {
			return pick("◌", "~"), "was running", theme.DiagWarning
		}
		return pick("●", "*"), "running", theme.AIAccent
	case agentdag.StateCompleted:
		return pick("✓", "+"), "done", theme.AccentGreen
	case agentdag.StateSkipped:
		return pick("↷", ">"), "skipped", theme.TextDim
	case agentdag.StateFailed:
		return pick("✘", "x"), "failed", theme.DiagError
	case agentdag.StateBlocked:
		return pick("⊘", "!"), "blocked", theme.DiagWarning
	case agentdag.StatePaused:
		return pick("‖", "="), "paused", theme.DiagWarning
	case agentdag.StateCancelled:
		return pick("◌", "-"), "cancelled", theme.TextDim
	}
	return pick("○", "o"), "pending", theme.TextDim
}

func agSeg(tok theme.Color256, s string) string { return theme.FgBg(tok, theme.BgPanel).Render(s) }

// agFit cuts s to w cells (with …) and pads it to exactly w.
func agFit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) > w {
		s = runewidth.Truncate(s, w, "…")
	}
	if pad := w - runewidth.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// agRow pads a styled row to width with the panel background.
func agRow(styled string, width int) string {
	if pad := width - textWidth(styled); pad > 0 {
		styled += theme.Bg(theme.BgPanel).Render(strings.Repeat(" ", pad))
	}
	return styled
}

// agText is one plain row: indent + text in tok, cut and padded.
func agText(tok theme.Color256, text string, width int) string {
	return agSeg(tok, agFit(" "+text, width))
}

// ── Drawing ────────────────────────────────────────────────────────────

// agentsActivity is a card's second row: what the node does now, waits
// for, or what it ran on.
func (s *aiState) agentsActivity(d agentsRun, n agentsNode, room int, g agentsGlyphSet) string {
	st := n.st
	switch n.state {
	case agentdag.StateRunning, agentdag.StateScheduled:
		if !d.live {
			return "stopped by a restart"
		}
		if st == nil {
			return "running"
		}
		if k := len(st.Tools); k > 0 && !strings.Contains(st.Tools[k-1], " → ") {
			return g.tool + " " + aiOneLine(aiCleanText(strings.TrimPrefix(st.Tools[k-1], "⚙ ")))
		}
		if t := aiOneLine(aiCleanText(st.Text)); t != "" {
			head := g.now + " now · …"
			r := []rune(t)
			if keep := room - runewidth.StringWidth(head); keep > 0 && len(r) > keep {
				t = string(r[len(r)-keep:])
			}
			return head + strings.TrimLeft(t, " ")
		}
		return "waiting for the model"
	case agentdag.StatePending, agentdag.StateBlocked, "":
		var deps []string
		for _, e := range d.edges {
			if e.To == n.id {
				deps = append(deps, d.nodes[d.byID[e.From]].title)
			}
		}
		if len(deps) == 0 {
			return "ready to start"
		}
		return g.from + " " + strings.Join(deps, ", ")
	case agentdag.StateFailed:
		if st != nil && st.Error != "" {
			return g.fail + " " + aiOneLine(aiCleanText(st.Error))
		}
	}
	return agentsModelLine(st, room)
}

// agentsModelLine is "claude-sonnet-4-5 · 4.1k tok · $0.013": the model,
// the tokens and the cost of a finished node, in at most room cells. The
// least important part goes first: the version of the model name, then
// the tokens, then the cost. A price that is not known leaves the cost
// out. "" when the node has no model.
func agentsModelLine(st *ai.StoredStep, room int) string {
	if st == nil || st.Model == "" {
		return ""
	}
	var tok, cost string
	if st.Usage != nil {
		tok = aiTokText(aiUsageTokens(*st.Usage))
	}
	if st.Cost != nil {
		cost = aiDollars(*st.Cost)
	}
	short := aiShortModel(st.Model)
	for _, parts := range [][]string{
		{st.Model, tok, cost}, {short, tok, cost}, {short, cost}, {short}, {cost},
	} {
		if line := strings.Join(nonEmpty(parts...), " · "); runewidth.StringWidth(line) <= room {
			return line
		}
	}
	return short
}

// agentsStats is "21 s · 1 turn · 3 tools" (a pending node: its model).
func (s *aiState) agentsStats(d agentsRun, n agentsNode, now time.Time) string {
	dur, turns, tools := s.agentsStatParts(d, n, now)
	return strings.Join(nonEmpty(dur, turns, tools), " · ")
}

// agentsStatParts are the three parts of agentsStats: the time, the
// turns and the tools ("" for a part that is not known). A pending node
// has its model in the first.
func (s *aiState) agentsStatParts(d agentsRun, n agentsNode, now time.Time) (dur, turns, tools string) {
	st := n.st
	if n.state == agentdag.StatePending || n.state == "" {
		if st != nil {
			return st.Model, "", ""
		}
		return "", "", ""
	}
	var parts []string
	switch {
	case n.state == agentdag.StateRunning && d.live && d.msg.Run.ID == s.agents.runOf && !s.agents.start[n.id].IsZero():
		parts = append(parts, aiDurText(now.Sub(s.agents.start[n.id]).Milliseconds()))
	case st != nil && (st.ElapsedMs > 0 || n.state.Done() || n.state == agentdag.StateFailed):
		parts = append(parts, aiDurText(st.ElapsedMs))
	}
	dur = strings.Join(parts, "")
	if t := s.agentsTurns(d, n); t > 0 {
		turns = strconv.Itoa(t) + " turn" + plural(t)
	}
	if st != nil && len(st.Tools) > 0 {
		tools = strconv.Itoa(len(st.Tools)) + " tool" + plural(len(st.Tools))
	}
	return dur, turns, tools
}

// aiShortModelFit is model, or its short name when room is too small.
func aiShortModelFit(model string, room int) string {
	if runewidth.StringWidth(model) <= room {
		return model
	}
	return aiShortModel(model)
}

// agFitParts joins parts with " · " in at most room cells, leaving out
// the parts listed in drop, in that order, until it fits.
func agFitParts(parts []string, drop []int, room int) string {
	parts = append([]string(nil), parts...)
	for k := 0; ; k++ {
		line := strings.Join(nonEmpty(parts...), " · ")
		if runewidth.StringWidth(line) <= room || k >= len(drop) {
			return line
		}
		parts[drop[k]] = ""
	}
}

// agentsCard draws one node card, exactly w cells wide.
func (s *aiState) agentsCard(d agentsRun, n agentsNode, w int, selected bool, now time.Time, g agentsGlyphSet) []string {
	inner := w - 2
	border := theme.TextDim
	titleTok := theme.TextPrimary
	if selected {
		border, titleTok = theme.AIAccent, theme.AIAccent
	}
	stallSecs, stalled := 0, false
	if d.live {
		stallSecs, stalled = s.agentsStalled(d.msg.Run.ID, n.state, n.id, now)
	}
	if stalled {
		border = theme.DiagWarning // colour by state: no token for too long
	}
	expanded := s.agentsExpanded(d.msg.Run.ID, n)
	head := runewidth.Truncate(aiFoldGlyph(expanded)+" "+n.title, max(inner-3, 1), "…")
	fill := max(inner-2-runewidth.StringWidth(head), 0)
	top := agSeg(border, g.tl+" ") + theme.FgBg(titleTok, theme.BgPanel).Bold(true).Render(head) +
		agSeg(border, " "+strings.Repeat(g.h, fill)+g.tr)
	row := func(tok theme.Color256, text string) string {
		return agSeg(border, g.v) + agSeg(tok, " "+agFit(text, inner-2)+" ") + agSeg(border, g.v)
	}
	glyph, label, tok := agentsLook(n.state, d.live)
	stallRow := theme.GlyphAIStall.String() + " possibly stalled · " + agSecs(stallSecs)
	if stalled {
		tok = theme.DiagWarning
	}
	first := glyph + " " + label
	if n.st != nil && n.st.Category != "" {
		first += " · " + n.st.Category
	}
	out := []string{top}
	if expanded {
		stats := s.agentsStats(d, n, now)
		if n.st != nil && n.st.Model != "" && !n.state.Done() && n.state != agentdag.StatePending && n.state != "" {
			// A node at work shows its model next to the live time; on a
			// narrow card the tools go first, then the turns, then the model.
			dur, turns, tools := s.agentsStatParts(d, n, now)
			stats = agFitParts([]string{aiShortModelFit(n.st.Model, inner-2-len(dur)-3), dur, turns, tools}, []int{3, 2, 0}, inner-2)
		}
		act := row(theme.TextSecondary, s.agentsActivity(d, n, inner-2, g))
		if stalled {
			act = row(theme.DiagWarning, stallRow)
		}
		out = append(out, row(tok, first), act, row(theme.TextDim, stats))
	} else {
		if stats := s.agentsStats(d, n, now); stats != "" && n.state.Done() {
			first = glyph + " " + label + " · " + stats
		}
		out = append(out, row(tok, first))
		if line := agentsModelLine(n.st, inner-2); line != "" && n.state.Done() {
			out = append(out, row(theme.TextDim, line)) // model · tokens · cost
		}
		if stalled {
			out = append(out, row(theme.DiagWarning, stallRow))
		}
	}
	return append(out, agSeg(border, g.bl+strings.Repeat(g.h, inner)+g.br))
}

// agentsConnector joins the cards of one wave row (centres up) to the
// next wave's first row (centres down): │ down from each, one ─ line
// with ┴ ┬ joints, ▼ into each. The dependency list says which node
// feeds which.
func agentsConnector(up, down []int, width int, g agentsGlyphSet) []string {
	line := func(set map[int]string) string {
		var b strings.Builder
		for x := 0; x < width; x++ {
			if c, ok := set[x]; ok {
				b.WriteString(c)
			} else {
				b.WriteByte(' ')
			}
		}
		return agSeg(theme.TextDim, b.String())
	}
	at := func(xs []int, c string) map[int]string {
		m := map[int]string{}
		for _, x := range xs {
			if x < width {
				m[x] = c
			}
		}
		return m
	}
	if len(up) == 1 && len(down) == 1 && up[0] == down[0] {
		return []string{line(at(up, g.v)), line(at(down, g.arrow))}
	}
	lo, hi := width, -1
	inUp, inDown := map[int]bool{}, map[int]bool{}
	for _, x := range up {
		lo, hi = min(lo, x), max(hi, x)
		inUp[x] = true
	}
	for _, x := range down {
		lo, hi = min(lo, x), max(hi, x)
		inDown[x] = true
	}
	mid := map[int]string{}
	for x := lo; x <= hi && x < width; x++ {
		c := g.h
		switch {
		case inUp[x] && inDown[x] && x == lo && x != hi:
			c = g.teeRight
		case inUp[x] && inDown[x] && x == hi && x != lo:
			c = g.teeLeft
		case inUp[x] && inDown[x]:
			c = g.cross
		case inUp[x] && x == lo:
			c = g.bl
		case inUp[x] && x == hi:
			c = g.br
		case inUp[x]:
			c = g.teeUp
		case inDown[x] && x == lo:
			c = g.tl
		case inDown[x] && x == hi:
			c = g.tr
		case inDown[x]:
			c = g.teeDown
		}
		mid[x] = c
	}
	return []string{line(at(up, g.v)), line(mid), line(at(down, g.arrow))}
}

// agentsBuild draws the head, body and foot rows of the shown run.
func (m Model) agentsBuild(width int, now time.Time) aiAgentsCache {
	if m.ai.agents.usage {
		return m.usageBuild(width)
	}
	s := m.ai
	g := agentsGlyphs()
	out := aiAgentsCache{ok: true, selTop: -1, selBot: -1}
	d, ok := s.agentsData()
	ascii := theme.CurrentIconMode == theme.IconModeASCII
	runs := "←→"
	if ascii {
		runs = "<- ->"
	}
	out.foot = agText(theme.TextDim, "Tab next · Space fold · d details · u usage · "+runs+" runs · Esc chat", width)
	divider := agSeg(theme.BorderSubtle, strings.Repeat(g.h, width))
	if !ok {
		out.head = []string{agText(theme.TextSecondary, "No agent runs in this chat yet.", width), agText(theme.TextDim, "", width), divider}
		for _, l := range wrapPlain("Pick a profile with more than one step (Alt+Shift+P) and send a message: its steps show here as cards, wave by wave.", max(width-2, 8)) {
			out.body = append(out.body, agText(theme.TextDim, l, width))
		}
		return out
	}

	// Head: the run and its progress.
	wave, waves, done, running := d.progress()
	left := "run " + d.msg.Run.ID
	if d.msg.Run.Profile != "" {
		left += " · " + d.msg.Run.Profile
	}
	if d.of > 1 {
		left += " (" + strconv.Itoa(d.nth) + "/" + strconv.Itoa(d.of) + ")"
	}
	right := "wave " + strconv.Itoa(wave) + "/" + strconv.Itoa(waves) + " · done " + strconv.Itoa(done) + "/" + strconv.Itoa(len(d.nodes))
	lw := max(width-2-runewidth.StringWidth(right)-2, 4)
	runLine := theme.FgBg(theme.TextPrimary, theme.BgPanel).Bold(true).Render(agFit(" "+left, lw+1)) +
		agSeg(theme.TextSecondary, agFit("  "+right, width-lw-1))
	status, statusTok := "", theme.TextDim
	switch rs := d.msg.Run.State; {
	case rs == ai.RunStatePaused:
		glyph, _, _ := agentsLook(agentdag.StatePaused, true)
		status, statusTok = glyph+" paused · budget "+aiBudgetText(d.msg.Run.Budget)+" reached", theme.DiagWarning
	case d.live:
		glyph, _, tok := agentsLook(agentdag.StateRunning, true)
		status, statusTok = glyph+" "+strconv.Itoa(running)+" running", tok
		if !s.agents.manual {
			status += " · following"
		}
	case rs == ai.RunStateInterrupted && d.snap != nil:
		status, statusTok = g.fail+" run interrupted · last snapshot "+
			time.UnixMilli(d.snap.UpdatedAtMs).In(now.Location()).Format("15:04"), theme.DiagWarning
	case rs == ai.RunStateInterrupted:
		status, statusTok = g.fail+" run interrupted (termocode restarted)", theme.DiagWarning
	case rs == ai.RunStateDone:
		glyph, _, tok := agentsLook(agentdag.StateCompleted, false)
		status, statusTok = glyph+" "+aiRunSummaryText(d.msg.Run, d.msg.Steps), tok
	default:
		status, statusTok = g.fail+" "+aiRunSummaryText(d.msg.Run, d.msg.Steps), theme.DiagError
	}
	out.head = []string{agRow(runLine, width), agText(statusTok, status, width), divider}

	// Body: the cards, wave by wave.
	sel := s.agentsSelected(d)
	inner := width - 2
	cols := max(1, min(3, (inner+agentsCardGap)/(agentsCardMin+agentsCardGap)))
	cardW := max(min((inner-(cols-1)*agentsCardGap)/cols, agentsCardMax), 8)
	if cols == 1 {
		cardW = max(min(inner, agentsCardMax+4), 8)
	}
	centre := func(col int) int { return 1 + col*(cardW+agentsCardGap) + cardW/2 }
	gapStr := theme.Bg(theme.BgPanel).Render(strings.Repeat(" ", agentsCardGap))
	margin := theme.Bg(theme.BgPanel).Render(" ")
	blankCard := theme.Bg(theme.BgPanel).Render(strings.Repeat(" ", cardW))
	var prevCentres []int
	for _, layer := range d.layers {
		for r0 := 0; r0 < len(layer); r0 += cols {
			row := layer[r0:min(r0+cols, len(layer))]
			var centres []int
			for c := range row {
				centres = append(centres, centre(c))
			}
			switch {
			case prevCentres != nil && r0 == 0:
				out.body = append(out.body, agentsConnector(prevCentres, centres, width, g)...)
			case len(out.body) > 0:
				out.body = append(out.body, agRow("", width))
			}
			cards := make([][]string, len(row))
			h := 0
			for c, ni := range row {
				cards[c] = s.agentsCard(d, d.nodes[ni], cardW, ni == sel, now, g)
				h = max(h, len(cards[c]))
			}
			y0 := len(out.body)
			for r := 0; r < h; r++ {
				line := margin
				for c := range row {
					if c > 0 {
						line += gapStr
					}
					if r < len(cards[c]) {
						line += cards[c][r]
					} else {
						line += blankCard
					}
				}
				out.body = append(out.body, agRow(line, width))
			}
			for c, ni := range row {
				x0 := 1 + c*(cardW+agentsCardGap)
				out.hits = append(out.hits, agentsHit{y0: y0, y1: y0 + len(cards[c]), x0: x0, x1: x0 + cardW, id: d.nodes[ni].id})
				if ni == sel {
					out.selTop, out.selBot = y0, y0+len(cards[c])-1
				}
			}
			prevCentres = centres
		}
	}

	// The dependency list.
	out.body = append(out.body, agRow("", width), theme.FgBg(theme.TextSecondary, theme.BgPanel).Bold(true).Render(agFit(" Dependencies", width)))
	if len(d.edges) == 0 {
		out.body = append(out.body, agText(theme.TextDim, "  none", width))
	}
	fromW := 0
	for _, e := range d.edges {
		fromW = max(fromW, runewidth.StringWidth(d.nodes[d.byID[e.From]].title))
	}
	for _, e := range d.edges {
		from := d.nodes[d.byID[e.From]].title
		text := "  " + from + strings.Repeat(" ", fromW-runewidth.StringWidth(from)) + " " + g.to + " " + d.nodes[d.byID[e.To]].title
		out.body = append(out.body, agText(theme.TextSecondary, text, width))
	}

	// The selected node's details.
	if sel >= 0 && !s.agents.noDetails {
		n := d.nodes[sel]
		out.body = append(out.body, agRow("", width),
			theme.FgBg(theme.TextSecondary, theme.BgPanel).Bold(true).Render(agFit(" Node details · "+n.title, width)))
		for _, l := range s.agentsDetails(d, n, now, max(width-3, 8), g) {
			out.body = append(out.body, agText(theme.TextSecondary, "  "+l, width))
		}
	}
	return out
}

// agentsDetails are the details rows of node n (each ≤ w cells).
func (s *aiState) agentsDetails(d agentsRun, n agentsNode, now time.Time, w int, g agentsGlyphSet) []string {
	st := n.st
	var out []string
	var facts []string
	if st != nil {
		facts = append(facts, st.Category, st.Model)
		if st.Usage != nil {
			facts = append(facts, aiTokText(aiUsageTokens(*st.Usage)))
		}
		if st.Cost != nil {
			facts = append(facts, aiDollars(*st.Cost))
		}
		if st.ContextPct != nil {
			facts = append(facts, "context "+strconv.FormatFloat(*st.ContextPct, 'f', 0, 64)+" %")
		}
	}
	if f := strings.Join(nonEmpty(facts...), " · "); f != "" {
		out = append(out, f)
	}
	glyph, label, _ := agentsLook(n.state, d.live)
	state := strings.Join(nonEmpty(glyph+" "+label, s.agentsStats(d, n, now)), " · ")
	if st != nil && st.Files > 0 {
		state += " · " + strconv.Itoa(st.Files) + " file" + plural(st.Files)
	}
	out = append(out, state)
	if task := s.agentsTask(d, n.id); task != "" {
		out = append(out, "task · "+task)
	}
	if st == nil {
		return out
	}
	for _, t := range st.Notes {
		out = append(out, aiOneLine(aiCleanText(t)))
	}
	tools := st.Tools
	if len(tools) > 3 {
		out = append(out, "… "+strconv.Itoa(len(tools)-3)+" earlier tool"+plural(len(tools)-3))
		tools = tools[len(tools)-3:]
	}
	for _, t := range tools {
		out = append(out, aiOneLine(aiCleanText(t)))
	}
	switch {
	case n.state == agentdag.StateFailed && st.Error != "":
		for _, l := range wrapPlain(g.fail+" "+aiCleanText(st.Error), w) {
			out = append(out, l)
		}
	case n.state == agentdag.StateRunning && d.live:
		if a := s.agentsActivity(d, n, w, g); strings.HasPrefix(a, g.now) {
			out = append(out, a)
		}
	case n.state.Done():
		lines := wrapPlain(aiOneLine(aiCleanText(st.Text)), w)
		if len(lines) > 4 {
			lines = append(lines[:4], "…")
		}
		out = append(out, lines...)
	}
	return out
}

// agentsTask is a planned node's task while its run goes (the live
// handle holds it; chats and snapshots keep no prompts).
func (s *aiState) agentsTask(d agentsRun, id string) string {
	if !d.live || s.run == nil || s.runMsg != d.idx {
		return ""
	}
	for _, n := range s.run.Snapshot().Nodes {
		if n.ID == id && n.Prompt != "" {
			return aiOneLine(aiCleanText(n.Prompt))
		}
	}
	return ""
}

// agentsCached returns the drawn rows, rebuilt only when their key
// changed.
func (m Model) agentsCached(width int) *aiAgentsCache {
	s := m.ai
	now := clock.Now()
	k := aiAgentsKey{runGen: s.runGen, msgs: len(s.conv.Messages), snapGen: s.agents.snapGen, conv: s.conv.ID,
		runID: s.agents.runID, sel: s.agents.sel, manual: s.agents.manual, noDetails: s.agents.noDetails,
		usage: s.agents.usage, usageGen: s.usageGen,
		width: width, rk: theme.CurrentRenderKey()}
	if s.streaming && s.run != nil {
		k.secs = now.Unix() // running times move once a second
	}
	c := &s.agents.cache
	if c.ok && c.key == k {
		return c
	}
	agentsBuilds.Add(1)
	*c = m.agentsBuild(width, now)
	c.key = k
	return c
}

// agentsBodyH is the scrolled body's height in a slot of height rows.
func agentsBodyH(height int) int {
	return max(height-aiPanelTopRow-agentsHeadH-1, 1)
}

// renderAgentsBody draws the AGENTS rows below the slot's header and
// divider, each passed through edge.
func (m Model) renderAgentsBody(width, height int, edge func(string) string) []string {
	s := m.ai
	c := m.agentsCached(width)
	rows := make([]string, 0, height-aiPanelTopRow)
	avail := height - aiPanelTopRow
	for _, r := range c.head {
		if len(rows) < avail {
			rows = append(rows, edge(r))
		}
	}
	bodyH := agentsBodyH(height)
	top := s.agents.scroll
	if !s.agents.free && c.selTop >= 0 {
		if c.selBot >= top+bodyH {
			top = c.selBot - bodyH + 1
		}
		if c.selTop < top {
			top = c.selTop
		}
	}
	top = max(min(top, len(c.body)-bodyH), 0)
	s.agents.scroll = top
	blank := edge(agRow("", width))
	for i := top; i < top+bodyH && len(rows) < avail-1; i++ {
		if i < len(c.body) {
			rows = append(rows, edge(c.body[i]))
		} else {
			rows = append(rows, blank)
		}
	}
	for len(rows) < avail-1 {
		rows = append(rows, blank)
	}
	if len(rows) < avail {
		rows = append(rows, edge(c.foot))
	}
	return rows
}

// ── Snapshots ──────────────────────────────────────────────────────────

// aiRunSnapshotPath is the file of run id's snapshot.
func aiRunSnapshotPath(id string) (string, error) {
	dir, err := aiRunsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return r
		}
		return '_'
	}, id)+".json"), nil
}

// aiLoadRunSnapshot reads run id's snapshot (nil when there is none or
// it does not parse).
func aiLoadRunSnapshot(id string) *aiRunSnapshot {
	path, err := aiRunSnapshotPath(id)
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var snap aiRunSnapshot
	if json.Unmarshal(b, &snap) != nil || snap.Run.ID != id {
		return nil
	}
	return &snap
}

// aiAgentsSnapMsg carries a loaded snapshot to Update.
type aiAgentsSnapMsg struct {
	id   string
	snap *aiRunSnapshot
}

// agentsSnapCmd loads the shown run's snapshot off the Update goroutine
// when the run was interrupted and its snapshot is not loaded yet.
func (m *Model) agentsSnapCmd() tea.Cmd {
	s := m.aiS()
	idx, _, _, ok := s.agentsShown()
	if !ok {
		return nil
	}
	r := s.conv.Messages[idx].Run
	if r.State != ai.RunStateInterrupted {
		return nil
	}
	if _, loaded := s.agents.snaps[r.ID]; loaded {
		return nil
	}
	id := r.ID
	return func() tea.Msg { return aiAgentsSnapMsg{id: id, snap: aiLoadRunSnapshot(id)} }
}

func (m *Model) onAIAgentsSnap(msg aiAgentsSnapMsg) tea.Cmd {
	s := m.aiS()
	if s.agents.snaps == nil {
		s.agents.snaps = map[string]*aiRunSnapshot{}
	}
	s.agents.snaps[msg.id] = msg.snap
	s.agents.snapGen++
	return nil
}

// ── Switching and keys ─────────────────────────────────────────────────

// toggleAgentsView is Alt+Shift+A / "AI: Agents Panel": it opens the
// slot on the AGENTS view, or switches the open slot between the chat
// and AGENTS.
func (m *Model) toggleAgentsView() tea.Cmd {
	s := m.aiS()
	if m.actionsPanelVisible() && s.agents.on {
		s.agents.on = false
		s.focused = true
		return nil
	}
	return m.showAgents("")
}

// showAgents shows the AGENTS view on run id ("" = the running or the
// newest run), in follow mode.
func (m *Model) showAgents(id string) tea.Cmd {
	s := m.aiS()
	m.openAIPanel(true)
	s.agents.on, s.agents.usage = true, false
	s.agents.runID = id
	s.agents.manual, s.agents.free, s.agents.sel = false, false, ""
	return m.agentsSnapCmd()
}

// agentsMove moves the selection dir nodes in wave order (wrapping);
// the user chose: follow mode stops.
func (m *Model) agentsMove(dir int) {
	s := m.aiS()
	d, ok := s.agentsData()
	if !ok || len(d.nodes) == 0 {
		return
	}
	cur := s.agentsSelected(d)
	next := (cur + dir + len(d.nodes)) % len(d.nodes)
	s.agents.sel, s.agents.manual, s.agents.free = d.nodes[next].id, true, false
}

// agentsSwitchRun shows the previous / next run of the chat.
func (m *Model) agentsSwitchRun(dir int) tea.Cmd {
	s := m.aiS()
	runs := s.agentsRunIdxs()
	_, nth, _, ok := s.agentsShown()
	if !ok {
		return nil
	}
	k := max(min(nth-1+dir, len(runs)-1), 0)
	s.agents.runID = s.conv.Messages[runs[k]].Run.ID
	s.agents.sel, s.agents.manual, s.agents.free, s.agents.scroll = "", false, false, 0
	return m.agentsSnapCmd()
}

// agentsFold folds or unfolds the selected card (the same choice as its
// step card in the chat).
func (m *Model) agentsFold() {
	s := m.aiS()
	d, ok := s.agentsData()
	if !ok {
		return
	}
	i := s.agentsSelected(d)
	if i < 0 {
		return
	}
	if s.conv.Folded == nil {
		s.conv.Folded = map[string]bool{}
	}
	s.conv.Folded[aiCardID(d.msg.Run.ID, d.nodes[i].id)] = s.agentsExpanded(d.msg.Run.ID, d.nodes[i])
	s.runGen++
	if !s.streaming {
		_ = s.store.Save(&s.conv)
	}
}

// agentsScroll scrolls the body by delta rows (the selection may leave
// the view until it moves again).
func (m *Model) agentsScroll(delta int) {
	s := m.aiS()
	s.agents.scroll = max(s.agents.scroll+delta, 0)
	s.agents.free = true
}

// routeAgentsKey handles the keys of the focused AGENTS view.
func (m *Model) routeAgentsKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	s := m.aiS()
	page := max(agentsBodyH(m.h)-2, 1)
	switch msg.String() {
	case "tab", "down", "j":
		m.agentsMove(1)
	case "shift+tab", "up", "k":
		m.agentsMove(-1)
	case "enter", " ":
		m.agentsFold()
	case "d":
		s.agents.noDetails = !s.agents.noDetails
	case "u":
		s.agents.usage = !s.agents.usage // AGENTS <-> AI: Usage
		s.agents.scroll, s.agents.free = 0, true
	case "f":
		s.agents.manual, s.agents.free, s.agents.sel = false, false, ""
	case "left", "h":
		return m.agentsSwitchRun(-1), true
	case "right", "l":
		return m.agentsSwitchRun(1), true
	case "pgup":
		m.agentsScroll(-page)
	case "pgdown":
		m.agentsScroll(page)
	case "home":
		s.agents.scroll, s.agents.free = 0, true
	case "esc", "q":
		s.agents.on = false
	case "ctrl+c":
		return m.aiCancelAll(), true
	default:
		if msg.Alt {
			return nil, false
		}
		// Letters do nothing here (there is no input box), but they do
		// not reach the editor either.
		return nil, msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace
	}
	return nil, true
}

// agentsTargetAt is the hit id of panel-local body row y and column x
// of the AGENTS view: "ai:agents:<node>" on a card, else "ai:agents".
func (m Model) agentsTargetAt(x, y int) string {
	s := m.ai
	if s == nil {
		return "ai:agents"
	}
	row := y - aiPanelTopRow - agentsHeadH
	if row < 0 || row >= agentsBodyH(m.h) {
		return "ai:agents"
	}
	c := &s.agents.cache
	if !c.ok {
		return "ai:agents"
	}
	i := s.agents.scroll + row
	for _, h := range c.hits {
		if i >= h.y0 && i < h.y1 && x >= h.x0 && x < h.x1 {
			return "ai:agents:" + h.id
		}
	}
	return "ai:agents"
}
