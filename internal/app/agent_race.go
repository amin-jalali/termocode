package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentjail"
	"github.com/amin-jalali/termocode/internal/agentrace"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/profileform"
	"github.com/amin-jalali/termocode/internal/raceview"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Agent: Race… (AGX-4, plan 2.5) ───────────────────────────────────────
//
// One task fanned out to 2–8 lanes of outside agents (an agent may run in several, CMP-18). Each lane gets its own agent
// worktree from HEAD (hidden in the git dir, like G-WT) and a terminal
// tab in it that runs the agent. The task is typed into a lane only once
// the agent's input box is on screen (its screen rules say Done), never
// into a question, and only once. The race is one record in
// <git-common-dir>/termocode/races.json; lanes' states come from their
// tabs. The compare page (agent_race_compare.go) merges the winner.
//
// A lane runs the agent's bare program: no termocode MCP server, whose
// tools would work in the user's folder instead of the lane's worktree.

const (
	agentRacePaletteID        = "agent-race"
	agentRaceComparePaletteID = "agent-race-compare"
)

// raceState is the race flow's state; nil until first used.
type raceState struct {
	formOn bool
	form   raceview.Form
	opts   []raceview.Option
	pageOn bool
	page   raceview.Compare

	races []agentrace.Race // the records of this repository
	dir   string           // where they live ("" not read yet)
	cur   string           // the race on show

	bufs   map[string]int    // lane id → its tab's buffer
	worked map[string]bool   // lane id → the agent worked after the task was typed
	verify map[int][2]string // verify job buffer → race id, lane id
	target [2]string         // the race and lane a card is about
	bcast  raceBroadcast     // the message or command a card is about (agent_race_broadcast.go)
	seq    int               // save generation
}

func (m *Model) raceS() *raceState {
	if m.race == nil {
		m.race = &raceState{bufs: map[string]int{}, worked: map[string]bool{}, verify: map[int][2]string{}}
	}
	return m.race
}

func (m Model) raceFormOpen() bool { return m.race != nil && m.race.formOn }
func (m Model) racePageOpen() bool { return m.race != nil && m.race.pageOn }
func (m Model) raceOpen() bool     { return m.raceFormOpen() || m.racePageOpen() }

// find is race id (nil: none).
func (s *raceState) find(id string) *agentrace.Race {
	for i := range s.races {
		if s.races[i].ID == id {
			return &s.races[i]
		}
	}
	return nil
}

// current is the race on show, else the newest.
func (s *raceState) current() *agentrace.Race {
	if r := s.find(s.cur); r != nil {
		return r
	}
	if len(s.races) == 0 {
		return nil
	}
	return &s.races[len(s.races)-1]
}

func (s *raceState) bufOf(lane string) int { return s.bufs[lane] }

// raceLaneBuf: buffer buf is a race lane's tab.
func (m Model) raceLaneBuf(buf int) bool {
	if m.race == nil {
		return false
	}
	_, l := m.race.laneOfBuf(buf)
	return l != nil
}

// laneOfBuf is the lane whose tab is buffer buf.
func (s *raceState) laneOfBuf(buf int) (*agentrace.Race, *agentrace.Lane) {
	for id, b := range s.bufs {
		if b != buf {
			continue
		}
		for i := range s.races {
			if l := s.races[i].Lane(id); l != nil {
				return &s.races[i], l
			}
		}
	}
	return nil, nil
}

// raceActive: the agent of l may still work (it has a live tab).
func raceActive(l *agentrace.Lane) bool {
	return l.State == agentrace.Starting || l.State == agentrace.Working || l.State == agentrace.Waiting
}

// Seams for tests: worktree, tab and keys.
var (
	raceCreateWorktree = git.CreateAgentWorktreeFromHead
	raceLaunchLane     = func(m *Model, l agentrace.Lane, a agents.Agent) (int, error) { return m.raceLaunchTab(l, a) }
	raceSendKeys       = func(m *Model, buf int, text string) { m.raceChansend(buf, text) }
	// raceSubmitDelay is the pause between the task's text and its Enter
	// (an agent may read a fast "text + Enter" as a paste).
	raceSubmitDelay = 200 * time.Millisecond
)

// raceOptions are the lanes the card offers: the installed agents; one
// without screen rules is listed but off (the task cannot be typed
// safely, its input box is never seen).
func raceOptions(cli func(id string) string) []raceview.Option {
	var out []raceview.Option
	for _, a := range agents.Registry {
		if _, err := agentLookPath(a.Bin); err != nil {
			continue
		}
		o := raceview.Option{ID: a.ID, Title: a.Title, Hint: a.Bin}
		if s := cli(a.ID); s != "" {
			o.Hint += " · " + s
		}
		if !agents.HasScreenRules(a.ID) {
			o.Disabled, o.Hint = true, "its screen cannot be read, so the task cannot be typed safely"
		}
		out = append(out, o)
	}
	return out
}

// openRaceForm opens Agent: Race….
func (m *Model) openRaceForm(task, verify string) tea.Cmd {
	if !git.IsRepo(aiWorkspaceRoot()) {
		return m.aiInfo("A race needs a git repository: each lane works in its own git worktree")
	}
	s := m.raceS()
	signed := func(string) string { return "" }
	if m.ai != nil {
		cli := m.ai.cliStates
		signed = func(id string) string {
			if agentSignInState(cli, id) == profileform.CLIReady {
				return "signed in"
			}
			return ""
		}
	}
	s.opts = raceOptions(signed)
	n := 0
	for _, o := range s.opts {
		if !o.Disabled {
			n++
		}
	}
	if n < agentrace.MinLanes {
		return m.aiInfo("A race needs two installed agents whose screen termocode can read (Agent: Start… installs them)")
	}
	s.form = raceview.NewForm(s.opts, task, verify)
	s.form.SetSize(m.w, m.h)
	s.formOn = true
	return nil
}

// raceStartedMsg: the lanes' worktrees are made (or failed).
type raceStartedMsg struct {
	race  agentrace.Race
	races []agentrace.Race // the records read from dir (when the UI had none)
	dir   string
	err   error
}

// raceStartCmd makes the race and its worktrees off the UI goroutine.
// taken is the highest race number the UI knows.
func raceStartCmd(root string, have bool, taken int, task, verify string, specs []agentrace.LaneSpec, now int64) tea.Cmd {
	return raceStartCmdOf(agentrace.New, root, have, taken, task, verify, specs, now)
}

// raceNewFunc makes the record: agentrace.New (a race) or
// agentrace.NewSplit (CMP-4).
type raceNewFunc func(n int, prompt, verify string, lanes []agentrace.LaneSpec, nowMs int64) (agentrace.Race, error)

// raceStartCmdOf is raceStartCmd with the record made by newRace.
func raceStartCmdOf(newRace raceNewFunc, root string, have bool, taken int, task, verify string, specs []agentrace.LaneSpec, now int64) tea.Cmd {
	return func() tea.Msg {
		dir, err := git.AgentStateDir(root)
		if err != nil {
			return raceStartedMsg{err: err}
		}
		var races []agentrace.Race
		if !have {
			if races, err = agentrace.Load(dir); err != nil {
				return raceStartedMsg{err: err}
			}
		}
		n := max(agentrace.NextNumber(races), taken+1)
		for raceNumberUsed(dir, n) { // left-over lanes of a lost record
			n++
		}
		r, err := newRace(n, task, verify, specs, now)
		if err != nil {
			return raceStartedMsg{err: err}
		}
		for i := range r.Lanes {
			l := &r.Lanes[i]
			w, err := raceCreateWorktree(root, l.ID)
			if err != nil {
				l.State, l.Note = agentrace.Failed, "no worktree: "+aiOneLine(err.Error())
				continue
			}
			l.Worktree, l.Branch = w.Dir(), w.Branch
		}
		return raceStartedMsg{race: r, races: races, dir: dir}
	}
}

// raceNumberUsed: a worktree of race n is still there.
func raceNumberUsed(dir string, n int) bool {
	es, _ := os.ReadDir(filepath.Join(dir, "worktrees"))
	pre := fmt.Sprintf("race-%d-", n)
	for _, e := range es {
		if strings.HasPrefix(e.Name(), pre) {
			return true
		}
	}
	return false
}

// startRace starts the race the card (or agent_race_start) asked for.
func (m *Model) startRace(task, verify string, lanes []string) tea.Cmd {
	var specs []agentrace.LaneSpec
	for _, id := range lanes {
		a, ok := agents.ByID(id)
		if !ok {
			return m.aiInfo("Unknown agent " + id)
		}
		specs = append(specs, agentrace.LaneSpec{Agent: a.ID, Title: a.Title})
	}
	s := m.raceS()
	return raceStartCmd(aiWorkspaceRoot(), s.dir != "", agentrace.NextNumber(s.races)-1, task, verify, specs, clock.Now().UnixMilli())
}

func (m *Model) onRaceStarted(msg raceStartedMsg) tea.Cmd {
	if msg.err != nil {
		return m.toastErr("Could not start the race", msg.err)
	}
	s := m.raceS()
	if s.dir == "" {
		s.races, s.dir = msg.races, msg.dir
	}
	s.races = append(s.races, msg.race)
	r := &s.races[len(s.races)-1]
	s.cur = r.ID
	started := 0
	for i := range r.Lanes {
		l := &r.Lanes[i]
		if l.State == agentrace.Failed {
			continue
		}
		a, _ := agents.ByID(l.Agent)
		buf, err := raceLaunchLane(m, *l, a)
		if err != nil {
			why := "no tab: "
			if errors.Is(err, agentjail.ErrNoTool) {
				why = "not started: "
			}
			l.State, l.Note = agentrace.Failed, why+aiOneLine(err.Error())
			continue
		}
		s.bufs[l.ID] = buf
		started++
	}
	m.openRacePage(r.ID)
	text := fmt.Sprintf("Race started: %d lane%s. Each agent gets the task when its input box shows", started, plural(started))
	if r.Split() {
		text = fmt.Sprintf("Split started: %d part%s. Each agent gets its part when its input box shows", started, plural(started))
	}
	if agentOSSandboxOn() {
		text += ". Each lane runs in its OS sandbox (agent_os_sandbox): only its worktree can be written"
	}
	return tea.Batch(m.raceSave(), m.aiInfo(text))
}

// raceLaunchTab opens lane l's tab in its worktree and runs the agent.
func (m *Model) raceLaunchTab(l agentrace.Lane, a agents.Agent) (int, error) {
	if m.nvim == nil {
		return 0, fmt.Errorf("the terminal is not ready")
	}
	if !m.termOpen && len(m.hiddenTerminalTabs) > 0 {
		m.toggleTerminalPanel()
	}
	if !m.termOpen {
		m.openTerminalPanel()
	}
	line, _, err := raceAgentLine(a, l.Worktree)
	if err != nil {
		return 0, err
	}
	n := len(m.terminalTabs)
	m.newTerminalTabIn(l.Worktree)
	t := m.activeTerminalTab()
	if t == nil || len(m.terminalTabs) == n {
		return 0, fmt.Errorf("could not open a terminal tab")
	}
	t.Agent, t.AgentState = a.ID, agents.StartState(a.ID)
	m.raceChansend(t.BufID, line+"\n")
	m.agentWatch(t.BufID, true)
	m.applyLayout()
	m.resizeTerminalSplit()
	return t.BufID, nil
}

// raceChansend types text into the terminal of buffer buf.
func (m *Model) raceChansend(buf int, text string) {
	if m.nvim == nil {
		return
	}
	_ = m.nvim.ExecLua(fmt.Sprintf(`
		local buf = %d
		local ok, chan = pcall(function() return vim.bo[buf].channel end)
		if ok and chan and chan > 0 then pcall(vim.fn.chansend, chan, %q) end
	`, buf, text))
}

// raceSubmitMsg is the Enter after the task's text.
type raceSubmitMsg struct {
	buf  int
	lane string
}

// raceOnAgentState follows a lane's agent screen (agent_tabs.go calls it
// when the state changed).
func (m *Model) raceOnAgentState(buf int, st agents.State) tea.Cmd {
	if m.race == nil {
		return nil
	}
	s := m.race
	r, l := s.laneOfBuf(buf)
	if l == nil || !raceActive(l) {
		return nil
	}
	var cmds []tea.Cmd
	switch {
	case !l.Typed && st == agents.Done: // the input box: type the task, once
		raceSendKeys(m, buf, r.LaneTask(*l)) // a split lane gets its own part (CMP-4)
		l.Typed, l.State = true, agentrace.Working
		s.worked[l.ID] = false
		if t, _ := m.termTabByBuf(buf); t != nil {
			t.AgentState = agents.Unknown // the old input box reads as a change
		}
		if raceSubmitDelay <= 0 {
			raceSendKeys(m, buf, "\r")
		} else {
			id := l.ID
			cmds = append(cmds, tea.Tick(raceSubmitDelay, func(time.Time) tea.Msg { return raceSubmitMsg{buf: buf, lane: id} }))
		}
	case st == agents.Waiting:
		l.State = agentrace.Waiting // the user answers in the tab
	case !l.Typed:
		return nil
	case st == agents.Working:
		s.worked[l.ID] = true
		l.State = agentrace.Working
	case st == agents.Done && s.worked[l.ID]:
		l.State = agentrace.Done
		cmds = append(cmds, m.raceLaneEnded(r, l))
	case st == agents.Done:
		l.State = agentrace.Working // the input box before the agent reacted
	}
	m.raceRefreshPage()
	cmds = append(cmds, m.raceSave())
	return tea.Batch(cmds...)
}

// raceOnAgentExit: the agent's command ended in its tab (shell
// integration). A lane that had the task is done, else stopped.
func (m *Model) raceOnAgentExit(buf int) tea.Cmd {
	if m.race == nil {
		return nil
	}
	r, l := m.race.laneOfBuf(buf)
	if l == nil || !raceActive(l) {
		return nil
	}
	l.State = agentrace.Stopped
	if l.Typed {
		l.State = agentrace.Done
	}
	delete(m.race.bufs, l.ID)
	m.raceRefreshPage()
	return tea.Batch(m.raceLaneEnded(r, l), m.raceSave())
}

// raceLaneEnded reads the lane's files and says when the race is over.
func (m *Model) raceLaneEnded(r *agentrace.Race, l *agentrace.Lane) tea.Cmd {
	cmds := []tea.Cmd{raceStatsCmd(aiWorkspaceRoot(), r.ID, []string{l.ID})}
	for _, o := range r.Lanes {
		if raceActive(&o) {
			return tea.Batch(cmds...)
		}
	}
	var c tea.Cmd
	if r.Split() {
		m.toast, c = m.toast.Push(toast.Info, "Split "+r.Name+" is done: F1 → Agent: Race Compare to merge each part")
		return tea.Batch(append(cmds, c, m.notifyOS("Agent split done"))...)
	}
	m.toast, c = m.toast.Push(toast.Info, "Race "+r.Name+" is done: F1 → Agent: Race Compare")
	return tea.Batch(append(cmds, c, m.notifyOS("Agent race done"))...)
}

// raceSyncTabs stops the lanes whose tab is gone.
func (m *Model) raceSyncTabs() {
	if m.race == nil {
		return
	}
	for id, buf := range m.race.bufs {
		if t, _ := m.termTabByBuf(buf); t != nil {
			continue
		}
		delete(m.race.bufs, id)
		for i := range m.race.races {
			if l := m.race.races[i].Lane(id); l != nil && raceActive(l) {
				l.State, l.Note = agentrace.Stopped, "tab closed"
			}
		}
	}
}

// raceStop stops every lane of r that still works: Ctrl+C once into a
// lane that has the task (the agent stops its turn; the tab and the work
// stay).
func (m *Model) raceStop(r *agentrace.Race) tea.Cmd {
	m.raceSyncTabs()
	n := 0
	var ended []string
	for i := range r.Lanes {
		l := &r.Lanes[i]
		if !raceActive(l) {
			continue
		}
		if buf, ok := m.race.bufs[l.ID]; ok && l.Typed {
			raceSendKeys(m, buf, "\x03")
		}
		l.State = agentrace.Stopped
		ended = append(ended, l.ID)
		n++
	}
	m.raceRefreshPage()
	if n == 0 {
		return m.aiInfo("No lane is working")
	}
	return tea.Batch(raceStatsCmd(aiWorkspaceRoot(), r.ID, ended), m.raceSave(), m.aiInfo(fmt.Sprintf("Stopped %d lane%s; the work stays in the worktrees", n, plural(n))))
}

// ── Saving ───────────────────────────────────────────────────────────────

var raceSaveMu sync.Mutex
var raceSavedSeq = map[string]int{}

type raceSavedMsg struct{ err error }

// raceSave writes the records off the UI goroutine; an older save that
// runs late never writes over a newer one.
func (m *Model) raceSave() tea.Cmd {
	s := m.raceS()
	if s.dir == "" {
		return nil
	}
	s.seq++
	seq, dir := s.seq, s.dir
	rs := make([]agentrace.Race, len(s.races))
	for i, r := range s.races {
		r.Lanes = append([]agentrace.Lane(nil), r.Lanes...)
		rs[i] = r
	}
	return func() tea.Msg {
		raceSaveMu.Lock()
		defer raceSaveMu.Unlock()
		if raceSavedSeq[dir] > seq {
			return nil
		}
		raceSavedSeq[dir] = seq
		return raceSavedMsg{err: agentrace.Save(dir, rs)}
	}
}

// ── The page ─────────────────────────────────────────────────────────────

// openRaceCompare is Agent: Race Compare: the race on show, else the
// newest (the records are read first when needed).
func (m *Model) openRaceCompare() tea.Cmd {
	s := m.raceS()
	if s.dir == "" {
		return raceLoadCmd(aiWorkspaceRoot())
	}
	r := s.current()
	if r == nil {
		return m.aiInfo("No race yet: F1 → Agent: Race… starts one")
	}
	m.raceSyncTabs()
	m.openRacePage(r.ID)
	return nil
}

// raceLoadedMsg carries the records read off the UI goroutine.
type raceLoadedMsg struct {
	races []agentrace.Race
	dir   string
	err   error
}

func raceLoadCmd(root string) tea.Cmd {
	return func() tea.Msg {
		dir, err := git.AgentStateDir(root)
		if err != nil {
			return raceLoadedMsg{err: err}
		}
		rs, err := agentrace.Load(dir)
		return raceLoadedMsg{races: rs, dir: dir, err: err}
	}
}

func (m *Model) onRaceLoaded(msg raceLoadedMsg) tea.Cmd {
	if msg.err != nil {
		return m.toastErr("Could not read the races", msg.err)
	}
	s := m.raceS()
	if s.dir == "" {
		s.races, s.dir = msg.races, msg.dir
		// Read from disk: no lane has a live tab in this session.
		for i := range s.races {
			for j := range s.races[i].Lanes {
				if l := &s.races[i].Lanes[j]; raceActive(l) {
					l.State = agentrace.Stopped
				}
			}
		}
	}
	return m.openRaceCompare()
}

func (m *Model) openRacePage(id string) {
	s := m.raceS()
	r := s.find(id)
	if r == nil {
		return
	}
	s.cur = id
	s.page = raceview.NewCompare(*r)
	s.page.SetSize(m.w, m.h)
	s.page.SetNow(clock.Now().UnixMilli())
	s.pageOn = true
}

// raceRefreshPage shows the newest state of the race on show.
func (m *Model) raceRefreshPage() {
	s := m.raceS()
	if !s.pageOn {
		return
	}
	if r := s.find(s.cur); r != nil {
		s.page.SetRace(*r)
	}
	s.page.SetNow(clock.Now().UnixMilli())
}

// handleRaceMsg routes the race messages (early in Update, so they arrive
// under any overlay).
func (m *Model) handleRaceMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case raceview.StartMsg:
		m.raceS().formOn = false
		return m.startRace(msg.Task, msg.Verify, msg.Lanes), true
	case raceview.CancelMsg:
		m.raceS().formOn = false
		return nil, true
	case raceview.ActionMsg:
		return m.raceAct(msg), true
	case raceStartedMsg:
		return m.onRaceStarted(msg), true
	case raceSubmitMsg:
		if _, l := m.raceS().laneOfBuf(msg.buf); l != nil && l.ID == msg.lane {
			raceSendKeys(m, msg.buf, "\r")
		}
		return nil, true
	case raceLoadedMsg:
		return m.onRaceLoaded(msg), true
	case raceSavedMsg:
		if msg.err != nil {
			return m.toastErr("Could not save the race", msg.err), true
		}
		return nil, true
	}
	return m.handleRaceCompareMsg(msg)
}

// routeToRace sends keys, clicks and resizes to the open card or page.
func (m Model) routeToRace(msg tea.Msg) (Model, tea.Cmd) {
	s := m.raceS()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		s.form.SetSize(m.w, m.h)
		s.page.SetSize(m.w, m.h)
		return m, nil
	case tea.MouseMsg:
		if m.mouseProbe != nil {
			m.probed("race")
			return m, nil
		}
		if s.formOn {
			var cmd tea.Cmd
			s.form, cmd = s.form.HandleMouse(msg.X, msg.Y, msg.Action, msg.Button)
			return m, cmd
		}
		return m, nil
	case tea.KeyMsg:
		if s.formOn {
			var cmd tea.Cmd
			s.form, cmd = s.form.Update(msg)
			return m, cmd
		}
		return m, s.page.Key(msg)
	}
	return m, nil
}

// raceLayer draws the card or the page over base (a confirm can sit on
// it).
func (m Model) raceLayer(base string) string {
	if !m.raceOpen() {
		return base
	}
	s := m.race
	o := m.overlayOpts()
	o.At = true
	if s.formOn {
		f := s.form
		f.SetSize(m.w, m.h)
		o.X, o.Y = f.Origin()
		return modalOverlay(base, f.Box(), m.w, m.h, &o)
	}
	p := s.page
	p.SetSize(m.w, m.h)
	o.X, o.Y = p.Origin()
	return modalOverlay(base, p.Box(), m.w, m.h, &o)
}

func init() {
	aiActionPolicies[agentRacePaletteID] = aiNever(aiNoPicker + " (agent_race_start)")
	aiActionPolicies[agentRaceComparePaletteID] = aiAuto() // a view: nothing changes
}
