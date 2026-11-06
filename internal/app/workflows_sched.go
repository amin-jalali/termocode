package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/schedule"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// ── The scheduler in the app (WFX-15, plan §4.2) ─────────────────────────
//
// One schedule.Scheduler per process, made the first time a workflow is
// on (so a workspace without schedules has no loop and no wakeups). A
// job is a workflow that is trusted, on, has a schedule and a good file,
// while workflows_enabled is true. A due slot waits for a free place of
// workflows_parallel_max (across workflows), runs its precheck through
// internal/spawn, then asks the UI goroutine to start the run (no card:
// the user turned the schedule on; every write still asks). The state
// file outside the repo gets last_run, next_run and the outcome.

// wfxSchedHost is the app's scheduler.
type wfxSchedHost struct {
	s    *schedule.Scheduler
	ch   chan tea.Msg  // due slots and events for the UI goroutine
	sem  chan struct{} // workflows_parallel_max
	root string
	ws   string

	mu     sync.Mutex
	hashes map[string]string // job id → the file hash it was added for
}

// Messages of the scheduler.
type (
	wfxDueMsg struct {
		ctx   context.Context
		due   schedule.Due
		reply chan wfxDueReply
	}
	wfxDueReply struct {
		h   *run.Handle
		err error
	}
	wfxSchedEventMsg struct{ ev schedule.Event }
	// wfxBootMsg starts the workflows of the workspace at start (the
	// scheduler, resume, pruning).
	wfxBootMsg struct{}
)

// wfxBootCmd is the Init command of the workflows.
func wfxBootCmd() tea.Cmd { return func() tea.Msg { return wfxBootMsg{} } }

// wfxSchedBoot makes the scheduler use clock c (nil: the real clock)
// and adds the jobs.
func (m *Model) wfxSchedBoot(c schedule.Clock) tea.Cmd {
	st := m.wfxS()
	st.schedClock = c
	if st.list == nil {
		ws, _ := m.rootDir()
		m.wfxLoad(ws)
	}
	return m.wfxSchedSync()
}

// wfxSchedStop ends the scheduler (tests; the process end does it too).
func (m *Model) wfxSchedStop() {
	if m.wfx != nil && m.wfx.sched != nil {
		m.wfx.sched.s.Stop()
	}
}

// wfxSchedWanted are the workflows that should be scheduled now.
func (m *Model) wfxSchedWanted() map[string]wire.Entry {
	want := map[string]wire.Entry{}
	if !workflowsEnabled() {
		return want
	}
	for _, e := range m.wfxS().list {
		if !e.Broken() && e.Trusted && e.Enabled && e.File.Schedule != nil {
			want[e.Name] = e
		}
	}
	return want
}

// wfxSchedSync adds and removes jobs so they match the list. It makes the
// scheduler the first time a job is wanted and returns its listen cmd.
func (m *Model) wfxSchedSync() tea.Cmd {
	st := m.wfxS()
	want := m.wfxSchedWanted()
	if len(want) == 0 && st.sched == nil {
		return nil
	}
	var cmd tea.Cmd
	if st.sched == nil && st.notOwner {
		return nil // another process runs this workspace's schedules
	}
	if st.sched == nil {
		st.sched = m.newWfxSchedHost(st.schedClock)
		st.sched.s.Start()
		cmd = wfxListen(st.sched.ch)
	}
	h := st.sched
	h.mu.Lock()
	var drop []string
	for id, hash := range h.hashes {
		if e, ok := want[id]; !ok || e.Hash != hash {
			drop = append(drop, id)
			delete(h.hashes, id)
		}
	}
	h.mu.Unlock()
	for _, id := range drop {
		h.s.Remove(id)
	}
	for name, e := range want {
		h.mu.Lock()
		_, had := h.hashes[name]
		h.mu.Unlock()
		if had {
			continue
		}
		job, err := wfxJob(h, e)
		if err != nil {
			recordError("workflow " + name + ": schedule: " + err.Error())
			continue
		}
		if err := h.s.Add(job); err != nil {
			recordError("workflow " + name + ": " + err.Error())
			continue
		}
		h.mu.Lock()
		h.hashes[name] = e.Hash
		h.mu.Unlock()
	}
	return cmd
}

func (m *Model) newWfxSchedHost(c schedule.Clock) *wfxSchedHost {
	st := m.wfxS()
	n, _ := wfxRunSettings()
	if n < 1 {
		n = workflow.DefaultParallelMax
	}
	h := &wfxSchedHost{ch: make(chan tea.Msg, 32), sem: make(chan struct{}, n), root: st.root, ws: st.ws, hashes: map[string]string{}}
	h.s = schedule.New(schedule.Options{Clock: c, Grace: wfxGrace(), Fire: h.fire, OnEvent: h.record})
	return h
}

// wfxGrace is workflows_missed_grace_min as the scheduler's Grace (0:
// never run a missed slot).
func wfxGrace() time.Duration {
	v, _ := workflowSettingValue("setting-workflows-missed-grace")
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	if n == 0 {
		return -1
	}
	return time.Duration(n) * time.Minute
}

// wfxJob makes the scheduler job of e.
func wfxJob(h *wfxSchedHost, e wire.Entry) (schedule.Job, error) {
	sc := e.File.Schedule
	sp := schedule.Spec{Preset: sc.Preset, Time: sc.Time, Cron: sc.Cron, TZ: sc.TZ}
	if sp.Cron != "" {
		sp.Preset, sp.Time = "", ""
	}
	compiled, err := schedule.Compile(sp)
	if err != nil {
		return schedule.Job{}, err
	}
	j := schedule.Job{ID: e.Name, Schedule: compiled}
	if s, err := ledger.LoadState(h.root, e.Name, e.Hash); err == nil && s.LastRunMs > 0 {
		j.LastRun = time.UnixMilli(s.LastRunMs)
	}
	if argv := append([]string(nil), sc.Precheck...); len(argv) > 0 {
		ws := h.ws
		j.Precheck = func(ctx context.Context) error {
			out, code, err := spawnOutput(ctx, argv, ws)
			if err != nil {
				return err
			}
			if code != 0 {
				return fmt.Errorf("precheck %s exited %d: %s", argv[0], code, lastLine(out))
			}
			return nil
		}
	}
	return j, nil
}

// fire runs one due slot: a place of workflows_parallel_max, then the
// run on the UI goroutine; it returns when the run ends.
func (h *wfxSchedHost) fire(ctx context.Context, d schedule.Due) error {
	select {
	case h.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-h.sem }()
	reply := make(chan wfxDueReply, 1)
	select {
	case h.ch <- wfxDueMsg{ctx: ctx, due: d, reply: reply}:
	case <-ctx.Done():
		return ctx.Err()
	}
	var r wfxDueReply
	select {
	case r = <-reply:
	case <-ctx.Done():
		return ctx.Err()
	}
	if r.err != nil {
		return r.err
	}
	select {
	case <-r.h.Done():
	case <-ctx.Done(): // turned off or the file changed: the run goes on, by hand
		return ctx.Err()
	}
	if rf := r.h.Snapshot(); rf.State == run.RunFail {
		return errors.New(rf.Error)
	}
	return nil
}

// record writes the slot's outcome to the state file and tells the UI.
func (h *wfxSchedHost) record(ev schedule.Event) {
	h.mu.Lock()
	hash := h.hashes[ev.ID]
	h.mu.Unlock()
	if s, err := ledger.LoadState(h.root, ev.ID, hash); err == nil {
		s.LastRunMs, s.LastOutcome = ev.Slot.UnixMilli(), string(ev.Outcome)
		if next, ok := h.s.NextRun(ev.ID); ok {
			s.NextRunMs = next.UnixMilli()
		}
		if err := ledger.SaveState(h.root, ev.ID, s); err != nil {
			recordError("workflow " + ev.ID + ": state: " + err.Error())
		}
	}
	select {
	case h.ch <- wfxSchedEventMsg{ev: ev}:
	default: // the state file is the truth; a dropped event only delays a redraw
	}
}

// handleWfxSchedMsg handles the scheduler's messages. ok=false: not ours.
func (m *Model) handleWfxSchedMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case wfxBootMsg:
		return m.wfxBoot(), true
	case wfxDueMsg:
		st := m.wfxS()
		listen := wfxListen(st.sched.ch)
		if msg.ctx.Err() != nil {
			msg.reply <- wfxDueReply{err: msg.ctx.Err()}
			return listen, true
		}
		m.wfxLoad("")
		h, cmd, err := m.wfxStartScheduled(msg.due.ID)
		msg.reply <- wfxDueReply{h: h, err: err}
		return tea.Batch(cmd, listen), true
	case wfxSchedEventMsg:
		st := m.wfxS()
		if m.wfxVisible() {
			m.wfxLoad("")
		}
		return wfxListen(st.sched.ch), true
	}
	return nil, false
}

// wfxStartScheduled starts the scheduled run of workflow name, checking
// again that it may run.
func (m *Model) wfxStartScheduled(name string) (*run.Handle, tea.Cmd, error) {
	e, ok := wire.Find(m.wfxS().list, name)
	switch {
	case !ok:
		return nil, nil, errors.New("the workflow file is gone")
	case e.Broken():
		return nil, nil, errors.New("the workflow file has problems")
	case !e.Trusted || !e.Enabled:
		return nil, nil, errors.New("the workflow is not trusted and on")
	case !workflowsEnabled():
		return nil, nil, errors.New("workflows are turned off")
	case m.wfxRunning(name) != nil:
		return nil, nil, errors.New(string(schedule.SkippedRunning))
	}
	_, h, cmd, err := m.wfxStartRun(e, false, false)
	if err != nil {
		return nil, nil, err
	}
	if l := m.wfxS().live[h.Snapshot().ID]; l != nil {
		l.scheduled = true
	}
	return h, cmd, nil
}

// wfxBoot starts the workflows of the workspace at start.
func (m *Model) wfxBoot() tea.Cmd {
	ws, _ := m.rootDir()
	if ws == "" {
		return nil
	}
	return m.wfxBootIn(ws, nil)
}

// wfxBootIn claims the workspace's workflows (one process owns them),
// resumes interrupted runs and starts the scheduler. Another owner: this
// process runs workflows by hand only.
func (m *Model) wfxBootIn(ws string, c schedule.Clock) tea.Cmd {
	m.wfxLoad(ws)
	m.wfxSyncActivity()
	st := m.wfxS()
	if st.root == "" || len(st.list) == 0 {
		return nil
	}
	if st.owner == nil {
		o, err := ledger.ClaimOwner(st.root)
		if err != nil {
			st.notOwner = true
			if !errors.Is(err, ledger.ErrOwned) {
				recordError("workflows: " + err.Error())
			}
			return nil
		}
		st.owner = o
	}
	m.wfxPrune()
	cmds := []tea.Cmd{m.wfxResumeOnStart(), m.wfxSchedBoot(c)}
	return tea.Batch(cmds...)
}

// wfxResumeOnStart handles the run files that say running: their process
// ended. Each is marked interrupted; with resume_on_start, the same file
// version and workflows on, it goes on from the step that was running.
func (m *Model) wfxResumeOnStart() tea.Cmd {
	st := m.wfxS()
	if st.root == "" {
		return nil
	}
	store := wire.RunFiles{Root: st.root}
	all, _ := store.List("", 0)
	var cmds []tea.Cmd
	for _, rf := range all {
		if rf.State != run.RunRunning || st.live[rf.ID] != nil {
			continue
		}
		rec := run.Recover(rf)
		if b, err := json.Marshal(rec); err == nil {
			if err := store.WriteRunFile(rec.ID, b); err != nil {
				recordError("workflow run " + rec.ID + ": " + err.Error())
			}
		}
		e, ok := wire.Find(st.list, rf.Workflow)
		if !ok || e.Broken() || !e.Trusted || rf.Dry || !e.File.ResumeOnStart || e.Hash != rf.Hash || !workflowsEnabled() || m.wfxRunning(e.Name) != nil {
			m.wfxPutRun(rec)
			continue
		}
		_, _, cmd, err := m.wfxStartRunFrom(e, false, false, rec)
		if err != nil {
			recordError("workflow " + e.Name + ": resume: " + err.Error())
			m.wfxPutRun(rec)
			continue
		}
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// wfxNextRun is the next slot of a scheduled workflow.
func (m *Model) wfxNextRun(name string) (time.Time, bool) {
	if m.wfx == nil || m.wfx.sched == nil {
		return time.Time{}, false
	}
	return m.wfx.sched.s.NextRun(name)
}

// wfxPrune deletes the runs older than workflows_history_days (at start).
func (m *Model) wfxPrune() {
	st := m.wfxS()
	v, _ := workflowSettingValue("setting-workflows-history-days")
	days, err := strconv.Atoi(v)
	if err != nil || days < 1 || st.root == "" {
		return
	}
	n, err := wire.RunFiles{Root: st.root}.Prune(clock.Now().AddDate(0, 0, -days))
	if err != nil {
		recordError("workflows: history: " + err.Error())
	}
	if n > 0 {
		m.wfxLoad("")
	}
}

// wfxNotifyDone tells the user that a run ended: on the OS (while the
// terminal is in the background) and to the browsers. A scheduled run
// notifies unless the file says otherwise; a run by hand only when the
// file asks. The text is a short summary, never step output.
func (m *Model) wfxNotifyDone(l *wfxLive, rf *run.RunFile) tea.Cmd {
	if rf.Dry {
		return nil
	}
	var notify []string
	if e, ok := wire.Find(m.wfxS().list, l.name); ok && e.File != nil {
		notify = e.File.Notify
	}
	osOn, web := l.scheduled && len(notify) == 0, l.scheduled && len(notify) == 0
	for _, n := range notify {
		osOn = osOn || n == "os"
		web = web || n == "web"
	}
	if web && m.web != nil {
		kind := webproto.AttentionRunDone
		if rf.State != run.RunOK {
			kind = webproto.AttentionRunFailed
		}
		m.web.attention(kind, rf.ID)
	}
	if osOn {
		return m.notifyOS(wfxSummary(l.name, rf))
	}
	return nil
}

// wfxSummary is one line about a run: "weekday-bugs: done · 3 items (1
// failed) · wrote 2".
func wfxSummary(name string, rf *run.RunFile) string {
	word := map[string]string{run.RunOK: "done", run.RunFail: "failed", run.RunCancel: "stopped", run.RunPause: "paused"}[rf.State]
	if word == "" {
		word = rf.State
	}
	items, failed, wrote := 0, 0, 0
	for _, st := range rf.Steps {
		if st == nil || st.Each == nil {
			continue
		}
		for _, it := range st.Each.Items {
			items++
			if it.State == run.StateFailed {
				failed++
			}
		}
	}
	for _, w := range rf.Writes {
		if w.State == "done" {
			wrote++
		}
	}
	s := name + ": " + word
	if items > 0 {
		s += fmt.Sprintf(" · %d items", items)
		if failed > 0 {
			s += fmt.Sprintf(" (%d failed)", failed)
		}
	}
	return s + fmt.Sprintf(" · wrote %d", wrote)
}
