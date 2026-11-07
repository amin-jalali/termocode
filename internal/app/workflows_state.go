package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/aistep"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/schedule"
	"github.com/amin-jalali/termocode/internal/workflow/source"
	"github.com/amin-jalali/termocode/internal/workflow/trigger"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// ── Workflows: the state, the ways in and the verbs (WFX-7) ─────────────
//
// The view lives in the sidebar (activity bar: the flow mark, shown once
// the workspace has a .termocode/workflows folder) and owns one page in
// the editor area: the overview, one workflow, or one run (a dry run is
// a run page with "would …" rows). Runs start in the UI process (plan
// §4.1) through internal/workflow/wire; their events come back over a
// channel per run. Every risky verb asks first, with the safe answer
// first and focused.

// Confirm kinds of the view (WFX range 3500–3539).
const (
	confirmKindWorkflowRun   confirmKindEnum = 3500
	confirmKindWorkflowDry   confirmKindEnum = 3501
	confirmKindWorkflowTrust confirmKindEnum = 3502
	confirmKindWorkflowOnOff confirmKindEnum = 3503
	confirmKindWorkflowStop  confirmKindEnum = 3504
	// workflows_trust.go
	confirmKindWorkflowUndo   confirmKindEnum = 3505
	confirmKindWorkflowBudget confirmKindEnum = 3506
	// the schedule went off while a run works: stop it too?
	confirmKindWorkflowOffStop confirmKindEnum = 3507
	// workflows_again.go: run a failed run again from its failed part
	confirmKindWorkflowRetry confirmKindEnum = 3508
)

// wfxNarrowW: a window narrower than this shows the view as one page.
const wfxNarrowW = 120

// wfxRunsKept is how many runs per workflow the view reads.
const wfxRunsKept = 20

// wfxLiveLines caps the live output kept per run.
const wfxLiveLines = 200

// wfxState is the view's state (Model.wfx; nil until it opens).
type wfxState struct {
	ws, private, root string
	list              []wire.Entry
	runs              map[string][]*run.RunFile // by workflow, newest first
	sel               string                    // selected workflow
	filter            string
	typing            bool
	scroll            int
	// page: the editor-area page. detail is "" (overview), a workflow
	// name, or "run:<id>".
	pageOn     bool
	detail     string
	pageScroll int
	// stepTab is the step tab of a run page (workflows_again.go): 0 =
	// all steps, i+1 = the file's step i.
	stepTab    int
	live       map[string]*wfxLive // by run id
	pending    string              // the workflow a card is about
	pendingDry bool
	// pendingRun, pendingStep, pendingItem: the run (and write) the
	// undo or budget card is about.
	pendingRun, pendingStep, pendingItem string
	problemSig                           string // the Problems rows last sent
	// sched is the scheduler (workflows_sched.go; nil until a workflow
	// is on); schedClock is its clock (nil: the real one).
	sched      *wfxSchedHost
	schedClock schedule.Clock
	// owner is this process's claim of the workspace's workflows;
	// notOwner: another process has it (no schedules, no resume here).
	owner    *ledger.Owner
	notOwner bool
	// trig polls and serves the event triggers (workflows_triggers.go;
	// nil while no workflow has one); trigQueue are the events whose
	// card waits (the first is on the card); trigRecent the last events.
	trig       *wfxTrigHost
	trigQueue  []trigger.Event
	trigRecent []wfxTrigSeen
}

// wfxLive is a run of this process.
type wfxLive struct {
	id, name string
	dry      bool
	h        *run.Handle
	ch       chan tea.Msg
	mu       sync.Mutex
	lines    []string // live output (AI tails, step lines)
	waiting  string   // what the run waits for ("" = nothing)
	// scheduled: the scheduler started it (no page is opened for it).
	scheduled bool
	// budget: the budget that paused it ("" = none: "day", "run" or
	// "item:<key>"); budgetOK: the budgets the user let it go on past
	// (each is not asked again).
	budget   string
	budgetOK map[string]bool
}

func (l *wfxLive) addLine(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, ln := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		l.lines = append(l.lines, ln)
	}
	if n := len(l.lines) - wfxLiveLines; n > 0 {
		l.lines = append(l.lines[:0:0], l.lines[n:]...)
	}
}

func (l *wfxLive) tail(n int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.lines) <= n {
		return append([]string(nil), l.lines...)
	}
	return append([]string(nil), l.lines[len(l.lines)-n:]...)
}

// Messages from a run's goroutines.
type (
	wfxEventMsg struct {
		id string
		ev run.Event
	}
	wfxLineMsg struct{ id, text string }
	wfxGateMsg struct {
		id    string
		ask   wire.GateAsk
		tool  bool // a source tool that is not read-only (no allow-list)
		plan  bool // a plan step that waits for approval (CMP-45)
		reply chan wire.GateDecision
	}
	wfxDoneMsg struct{ id string }
)

func (m *Model) wfxS() *wfxState {
	if m.wfx == nil {
		m.wfx = &wfxState{runs: map[string][]*run.RunFile{}, live: map[string]*wfxLive{}}
	}
	return m.wfx
}

// wfxPolicy is the admin policy (workflows-policy.json), read each time
// so an admin's change counts at once.
func wfxPolicy() workflow.Policy {
	return workflow.LoadPolicy(workflow.SystemPolicyPath, workflow.UserPolicyPath(filepath.Dir(filepath.Dir(wfxPrivateDir()))))
}

// wfxPrivateDir is the user's own workflow folder.
func wfxPrivateDir() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "termocode", "workflows")
}

// wfxHasProject reports whether the workspace has a workflow folder (the
// activity bar shows the icon then).
func wfxHasProject(ws string) bool {
	if ws == "" {
		return false
	}
	fi, err := os.Stat(filepath.Join(ws, filepath.FromSlash(wire.ProjectDir)))
	return err == nil && fi.IsDir()
}

// wfxLoad reads the workflow files and their recent runs of workspace ws.
func (m *Model) wfxLoad(ws string) {
	st := m.wfxS()
	if ws != "" {
		st.ws = ws
	}
	if st.root == "" && st.ws != "" {
		st.root, _ = ledger.Root(st.ws)
	}
	if st.private == "" {
		st.private = wfxPrivateDir()
	}
	st.list = wire.Load(st.ws, st.private, st.root)
	st.runs = map[string][]*run.RunFile{}
	if st.root != "" {
		all, _ := wire.RunFiles{Root: st.root}.List("", 0)
		for _, rf := range all {
			if len(st.runs[rf.Workflow]) < wfxRunsKept {
				st.runs[rf.Workflow] = append(st.runs[rf.Workflow], rf)
			}
		}
	}
	for _, l := range st.live {
		snap := l.h.Snapshot()
		m.wfxPutRun(snap)
	}
	if st.sel == "" || !m.wfxHas(st.sel) {
		st.sel = ""
		if keys := m.wfxKeys(); len(keys) > 0 {
			st.sel = keys[0]
		}
	}
}

// wfxPutRun puts rf first in its workflow's runs (replacing the same id).
func (m *Model) wfxPutRun(rf *run.RunFile) {
	st := m.wfxS()
	list := st.runs[rf.Workflow]
	out := []*run.RunFile{rf}
	for _, r := range list {
		if r.ID != rf.ID {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if len(out) > wfxRunsKept {
		out = out[:wfxRunsKept]
	}
	st.runs[rf.Workflow] = out
}

func (m *Model) wfxHas(name string) bool {
	_, ok := wire.Find(m.wfxS().list, name)
	return ok
}

// wfxKeys are the listed workflows (the filter applied).
func (m *Model) wfxKeys() []string {
	st := m.wfxS()
	q := strings.ToLower(st.filter)
	var out []string
	for _, e := range st.list {
		if q != "" && !strings.Contains(strings.ToLower(e.Name+" "+wfxTitle(e)), q) {
			continue
		}
		out = append(out, e.Name)
	}
	return out
}

func wfxTitle(e wire.Entry) string {
	if e.File != nil {
		return e.File.Title
	}
	return ""
}

// wfxRunning is the live run of workflow name (nil: none).
func (m *Model) wfxRunning(name string) *wfxLive {
	for _, l := range m.wfxS().live {
		if l.name == name {
			select {
			case <-l.h.Done():
			default:
				return l
			}
		}
	}
	return nil
}

// wfxRunByID finds a run file (live or stored).
func (m *Model) wfxRunByID(id string) (*run.RunFile, bool) {
	st := m.wfxS()
	if l, ok := st.live[id]; ok {
		return l.h.Snapshot(), true
	}
	for _, list := range st.runs {
		for _, rf := range list {
			if rf.ID == id {
				return rf, true
			}
		}
	}
	return nil, false
}

// ── Ways in ──────────────────────────────────────────────────────────────

// wfxVisible reports whether the view or its page is on screen.
func (m Model) wfxVisible() bool {
	return (m.showExp && !m.zenMode && m.activity.Active() == activity.ViewWorkflows) || m.wfxPageShown()
}

// revealWorkflows shows the view; a narrow window shows the page.
func (m *Model) revealWorkflows(focus bool) tea.Cmd {
	ws, _ := m.rootDir()
	m.wfxLoad(ws)
	m.wfxSyncActivity()
	m.activity.SetActive(activity.ViewWorkflows)
	cmd := m.wfxSendProblems()
	if m.w > 0 && m.w < wfxNarrowW {
		if m.showExp {
			m.showExp = false
			m.applyLayout()
		}
		return tea.Batch(cmd, m.wfxShowPage(m.wfxS().detail, true))
	}
	if !m.showExp {
		m.showExp = true
		m.applyLayout()
	}
	if focus {
		m.focus = FocusExplorer
	}
	return cmd
}

// wfxSyncActivity shows the activity bar icon when the workspace has
// workflows (or the view is open).
func (m *Model) wfxSyncActivity() {
	ws, _ := m.rootDir()
	activity.SetWorkflowsShown(wfxHasProject(ws) || (m.wfx != nil && len(m.wfx.list) > 0))
}

// ── Verbs ────────────────────────────────────────────────────────────────

// wfxTarget is the workflow a verb acts on: the page's, else the
// selected one.
func (m *Model) wfxTarget() (wire.Entry, bool) {
	st := m.wfxS()
	name := st.sel
	if m.wfxPageShown() && st.detail != "" {
		name = st.detail
		if strings.HasPrefix(name, "run:") {
			if rf, ok := m.wfxRunByID(strings.TrimPrefix(name, "run:")); ok {
				name = rf.Workflow
			}
		}
	}
	return wire.Find(st.list, name)
}

func (m *Model) wfxCard(kind confirmKindEnum, title, body string, safe confirm.Button, act confirm.Button) {
	m.confirm = confirm.Risky(title, body, safe, act)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, kind
}

// wfxAskRun asks before a run (dry or real); an untrusted repo file asks
// for trust first.
func (m *Model) wfxAskRun(dry bool) tea.Cmd {
	e, ok := m.wfxTarget()
	if !ok {
		return m.aiInfo("No workflow selected")
	}
	if e.Broken() {
		return m.aiInfo(e.Name + " has problems in its file; fix them first (Problems lists them)")
	}
	if !workflowsEnabled() {
		return m.aiInfo("Workflows are turned off (Settings → Workflows → workflows_enabled)")
	}
	if m.wfxRunning(e.Name) != nil {
		return m.aiInfo(e.Name + " is running already")
	}
	st := m.wfxS()
	st.pending, st.pendingDry = e.Name, dry
	if !e.Trusted {
		m.wfxCard(confirmKindWorkflowTrust, "Trust this workflow file?", wfxTrustBody(e),
			confirm.Button{ID: "cancel", Title: "Cancel"}, confirm.Button{ID: "trust", Title: "Trust this version"})
		return nil
	}
	return m.wfxAskRunCard(e, dry)
}

func (m *Model) wfxAskRunCard(e wire.Entry, dry bool) tea.Cmd {
	reads, writes := wfxCounts(e)
	if dry {
		body := fmt.Sprintf("%s · %s\nA dry run reads %d source(s) for real (read-only) and runs plan steps (they cost AI money).\n"+
			"Run steps are only shown; the %d write(s) are listed with their full text and never sent.", e.Name, wfxTitle(e), reads, writes)
		m.wfxCard(confirmKindWorkflowDry, "Dry run "+e.Name+"?", body,
			confirm.Button{ID: "cancel", Title: "Cancel"}, confirm.Button{ID: "dry", Title: "Dry run"})
		return nil
	}
	body := fmt.Sprintf("%s · %s\nIt reads %d source(s) and may write %d time(s) to outside systems.\n"+
		"Every write asks you on a card unless you allowed it for this file version.", e.Name, wfxTitle(e), reads, writes)
	m.wfxCard(confirmKindWorkflowRun, "Run "+e.Name+" now?", body,
		confirm.Button{ID: "cancel", Title: "Cancel"}, confirm.Button{ID: "run", Title: "Run"})
	return nil
}

// wfxTrustBody shows the file (first lines) and where it comes from.
func wfxTrustBody(e wire.Entry) string {
	lines := strings.Split(strings.TrimRight(string(e.Data), "\n"), "\n")
	more := ""
	if len(lines) > 10 {
		more = fmt.Sprintf("\n    … %d more lines (e opens the file)", len(lines)-10)
		lines = lines[:10]
	}
	return "This file comes from the repo: " + wfxRel(e) + "\n" +
		"It can read outside systems and ask to write to them. Trust is kept outside the repo, for these exact bytes; a change asks again.\n\n    " +
		strings.Join(lines, "\n    ") + more
}

func wfxRel(e wire.Entry) string {
	if e.Private {
		return "~/.config/termocode/workflows/" + filepath.Base(e.Path)
	}
	return wire.ProjectDir + "/" + filepath.Base(e.Path)
}

// wfxCounts counts the source and write steps of e.
func wfxCounts(e wire.Entry) (reads, writes int) {
	if e.File == nil {
		return 0, 0
	}
	walkSteps(e.File.Steps, func(typ string) {
		switch typ {
		case "source":
			reads++
		case "write":
			writes++
		}
	})
	return reads, writes
}

// answerWorkflowCard handles the view's cards; only the action id acts.
func (m *Model) answerWorkflowCard(id string) tea.Cmd {
	st := m.wfxS()
	kind := m.confirmKind
	if cmd, ok := m.answerWorkflowTrustCard(kind, id); ok {
		return cmd
	}
	if cmd, ok := m.answerWorkflowRetryCard(kind, id); ok {
		return cmd
	}
	name, dry := st.pending, st.pendingDry
	st.pending = ""
	e, ok := wire.Find(st.list, name)
	switch kind {
	case confirmKindWorkflowTrust:
		if id != "trust" || !ok {
			return nil
		}
		if err := wire.Trust(st.root, e); err != nil {
			return m.aiInfo("Could not save the trust: " + agentCtlShort(err.Error()))
		}
		m.wfxLoad("")
		e, _ = wire.Find(st.list, name)
		st.pending, st.pendingDry = name, dry
		return m.wfxAskRunCard(e, dry)
	case confirmKindWorkflowRun, confirmKindWorkflowDry:
		if (id != "run" && id != "dry") || !ok {
			return nil
		}
		return m.wfxStart(e, id == "dry")
	case confirmKindWorkflowOnOff:
		if id != "toggle" || !ok {
			return nil
		}
		was := e.Enabled
		cmd := m.wfxToggle(e)
		if was && m.wfxRunning(name) != nil {
			if now, ok := wire.Find(st.list, name); ok && !now.Enabled {
				st.pending = name
				m.wfxCard(confirmKindWorkflowOffStop, "Stop the run of "+name+" too?",
					name+" is off now: no new run starts on its schedule. A run of it still works.\n"+
						"Keep running lets it finish. Stop run ends it; finished items keep their results.",
					confirm.Button{ID: "keep", Title: "Keep running"}, confirm.Button{ID: "stop", Title: "Stop run", Style: confirm.StyleDestructive})
			}
		}
		return cmd
	case confirmKindWorkflowStop, confirmKindWorkflowOffStop:
		if id != "stop" {
			return nil
		}
		if l := m.wfxRunning(name); l != nil {
			l.h.Cancel()
			return m.aiInfo("Stopping " + name)
		}
	}
	return nil
}

// wfxAskToggle asks before a schedule goes on or off.
func (m *Model) wfxAskToggle() tea.Cmd {
	e, ok := m.wfxTarget()
	if !ok {
		return nil
	}
	if e.File == nil || e.File.Schedule == nil {
		return m.aiInfo(e.Name + " has no schedule; it runs by hand (r)")
	}
	m.wfxS().pending = e.Name
	if e.Enabled {
		m.wfxCard(confirmKindWorkflowOnOff, "Turn "+e.Name+" off?", "Its schedule stops; you can still run it by hand.",
			confirm.Button{ID: "cancel", Title: "Keep it on"}, confirm.Button{ID: "toggle", Title: "Turn off"})
		return nil
	}
	m.wfxCard(confirmKindWorkflowOnOff, "Turn "+e.Name+" on?",
		"It then runs by itself on its schedule, also while you are away. Writes still ask unless allowed.",
		confirm.Button{ID: "cancel", Title: "Cancel"}, confirm.Button{ID: "toggle", Title: "Turn on"})
	return nil
}

func (m *Model) wfxToggle(e wire.Entry) tea.Cmd {
	st := m.wfxS()
	if !e.Trusted {
		return m.aiInfo("Trust the file first (r asks)")
	}
	s, err := ledger.LoadState(st.root, e.Name, e.Hash)
	if err == nil {
		s.Enabled = !s.Enabled
		err = ledger.SaveState(st.root, e.Name, s)
	}
	if err != nil {
		return m.aiInfo("Could not save: " + agentCtlShort(err.Error()))
	}
	m.wfxLoad("")
	cmd := m.wfxSchedSync()
	if s.Enabled {
		word := e.Name + " is on"
		if next, ok := m.wfxNextRun(e.Name); ok {
			word += "; next run " + next.In(wfxLoc).Format("Mon Jan 2 15:04")
		}
		return tea.Batch(cmd, m.aiInfo(word))
	}
	return tea.Batch(cmd, m.aiInfo(e.Name+" is off"))
}

// wfxAskStop asks before the live run of the target stops.
func (m *Model) wfxAskStop() tea.Cmd {
	e, ok := m.wfxTarget()
	if !ok {
		return nil
	}
	if m.wfxRunning(e.Name) == nil {
		return m.aiInfo(e.Name + " is not running")
	}
	m.wfxS().pending = e.Name
	m.wfxCard(confirmKindWorkflowStop, "Stop "+e.Name+"?",
		"The step that works now ends without its answer; finished items keep their results.",
		confirm.Button{ID: "keep", Title: "Keep running"}, confirm.Button{ID: "stop", Title: "Stop", Style: confirm.StyleDestructive})
	return nil
}

// wfxTogglePause pauses the target's live run before its next step, or
// lets a paused one go on. No card: a pause only waits.
func (m *Model) wfxTogglePause() tea.Cmd {
	e, ok := m.wfxTarget()
	if !ok {
		return nil
	}
	l := m.wfxRunning(e.Name)
	if l == nil {
		return m.aiInfo(e.Name + " is not running")
	}
	if l.h.Held() {
		l.h.Continue()
		return m.aiInfo(e.Name + ": continues")
	}
	l.h.Pause()
	return m.aiInfo(e.Name + ": paused before its next step (p continues)")
}

// wfxEdit opens the workflow file in the editor.
func (m *Model) wfxEdit() tea.Cmd {
	e, ok := m.wfxTarget()
	if !ok {
		return nil
	}
	line := 1
	if len(e.Problems) > 0 && e.Problems[0].Line > 0 {
		line = e.Problems[0].Line
	}
	m.openFileAtLine(e.Path, line)
	return nil
}

// ── Running ──────────────────────────────────────────────────────────────

// wfxStart starts a run of e with every adapter of the app.
func (m *Model) wfxStart(e wire.Entry, dry bool) tea.Cmd {
	_, _, cmd, err := m.wfxStartID(e, dry)
	if err != nil {
		return m.aiInfo(e.Name + ": " + agentCtlShort(err.Error()))
	}
	return cmd
}

// wfxStartID is wfxStart that also gives the run id and handle (the AI's
// workflow tools wait on it).
func (m *Model) wfxStartID(e wire.Entry, dry bool) (string, *run.Handle, tea.Cmd, error) {
	return m.wfxStartRun(e, dry, true)
}

// wfxStartRun starts a run; show opens its page.
func (m *Model) wfxStartRun(e wire.Entry, dry, show bool) (string, *run.Handle, tea.Cmd, error) {
	return m.wfxStartRunFrom(e, dry, show, nil)
}

// wfxStartRunFrom is wfxStartRun that may continue a recovered run file.
func (m *Model) wfxStartRunFrom(e wire.Entry, dry, show bool, resume *run.RunFile) (string, *run.Handle, tea.Cmd, error) {
	return m.wfxStartRunWith(e, dry, show, resume, wfxRunOpts{})
}

// wfxRunOpts are the extras of a triggered run (CMP-38).
type wfxRunOpts struct {
	vars map[string]any // the run's vars over the file's (trigger_*)
	dir  string         // the run works here (a new agent worktree); "" is the workspace
}

// wfxStartRunWith is wfxStartRunFrom with vars and a working dir.
func (m *Model) wfxStartRunWith(e wire.Entry, dry, show bool, resume *run.RunFile, opts wfxRunOpts) (string, *run.Handle, tea.Cmd, error) {
	st := m.wfxS()
	ws := st.ws
	if opts.dir != "" {
		ws = opts.dir
	}
	if !dry && e.File != nil {
		if why := wfxPolicy().Blocked(e.File); why != "" {
			return "", nil, nil, errors.New("blocked: " + why)
		}
	}
	id := wire.NewRunID()
	if resume != nil {
		id = resume.ID
	}
	l := &wfxLive{id: id, name: e.Name, dry: dry, ch: make(chan tea.Msg, 64)}
	send := func(msg tea.Msg) {
		select {
		case l.ch <- msg:
		default: // the snapshot is the truth; a dropped event only delays a redraw
		}
	}
	block := func(ctx context.Context, msg tea.Msg) error {
		select {
		case l.ch <- msg:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	askAs := func(ctx context.Context, a wire.GateAsk, tool, plan bool) (wire.GateDecision, error) {
		reply := make(chan wire.GateDecision, 1)
		if err := block(ctx, wfxGateMsg{id: id, ask: a, tool: tool, plan: plan, reply: reply}); err != nil {
			return wire.GateDeny, err
		}
		select {
		case d := <-reply:
			return d, nil
		case <-ctx.Done():
			return wire.GateDeny, ctx.Err()
		}
	}
	ask := func(ctx context.Context, a wire.GateAsk, tool bool) (wire.GateDecision, error) {
		return askAs(ctx, a, tool, false)
	}
	mc := newWFMCP(m.aiMCP(), m.aiMCPTrustAnnotations())
	var tools source.ToolCaller
	var check run.ToolCheck
	if mc != nil {
		tools, check = mc, mc
	}
	src := &source.Sources{Workspace: ws, Tools: tools, GH: ghci.DefaultRunner(source.DefaultGitHubTimeout), Databases: wfDB{root: st.ws}}
	if s, err := m.secretStore(); err == nil && s != nil {
		src.Secrets = s
	}
	ms := wfxMessenger(id, block)
	ms.Tools = tools
	cfg := wire.Config{
		Workspace: ws, Root: st.root, Entry: e, RunID: id, Dry: dry, Resume: resume, Vars: opts.vars,
		Src: src, Tools: check, AI: m.wfxAIEnv(), Exec: wfxExec,
		Gate: func(ctx context.Context, a wire.GateAsk) (wire.GateDecision, error) { return ask(ctx, a, false) },
		ApprovePlan: func(ctx context.Context, c run.ApproveCall) (bool, error) {
			d, err := askAs(ctx, wfxPlanAsk(c), false, true)
			return d != wire.GateDeny, err
		},
		AskTool: func(ctx context.Context, c run.AskCall) (bool, error) {
			d, err := ask(ctx, wire.GateAsk{Step: c.Step.ID, Target: c.Server + " · " + c.Tool,
				Preview: "This source step calls a tool that is not marked read-only. It may change things there."}, true)
			return d != wire.GateDeny, err
		},
		Notify: func(ctx context.Context, msg wire.Message) error {
			if err := ms.Deliver(ctx, msg); err != nil {
				return err
			}
			via := strings.TrimSpace(msg.Via)
			switch {
			case via == "os" || via == "web":
				return nil // the line comes with the message
			case strings.HasPrefix(via, "${"):
				via = "webhook"
			}
			return block(ctx, wfxLineMsg{id: id, text: "message (" + via + "): " + wire.RedactOutside(msg.Text)})
		},
		OnEvent:      func(ev run.Event) { send(wfxEventMsg{id: id, ev: ev}) },
		OnAuditError: func(err error) { recordError("workflow " + e.Name + ": audit: " + err.Error()) },
		OnAgent: func(step, item string, ev ai.Event) {
			if ev.Kind == ai.EventToolStart && ev.Detail != "" {
				l.addLine(step + " " + item + ": " + lastLine(ev.Detail))
				send(wfxLineMsg{id: id})
			}
		},
		AllowFlagged: func(ctx context.Context, step, item string, chip aistep.Chip) (bool, error) {
			d, err := ask(ctx, wire.GateAsk{Step: step, Item: item, Target: "agent step " + step + " · item " + item,
				Preview: chip.Text + ": " + strings.Join(chip.Reasons, ", ") + ". The agent gets this item only if you allow it."}, true)
			return d != wire.GateDeny, err
		},
		OnAI: func(step, item string, ev agentrun.Event) {
			if ev.Tail != "" {
				l.addLine(step + " " + item + ": " + wfxLastLine(ev.Tail))
				send(wfxLineMsg{id: id})
			}
		},
	}
	gh := newWFXGitHub(ws)
	cfg.PR, cfg.GitHubComment, cfg.Find, cfg.Wait = gh.PR, gh.Comment, gh.Find, gh.Wait
	cfg.ParallelMax, cfg.AgentSandbox = wfxRunSettings()
	cfg.AgentOSSandbox = agentOSSandboxOn()
	if !dry && wfxHasAgentStep(e.File) {
		ae, why := agentEnvForRun(st.ws)
		if why != "" {
			return "", nil, nil, errors.New(why)
		}
		if ae != nil {
			cfg.AgentEnvFile = ae
			if src.Secrets != nil {
				cfg.AgentSecrets = src.Secrets
			}
			cfg.OnNetwork = func(step, item string, blocked []string) {
				l.addLine(step + " " + item + ": " + "network rule blocked " + strings.Join(blocked, ", "))
				send(wfxLineMsg{id: id})
			}
		}
	}
	h, err := wire.Start(context.Background(), cfg)
	if err != nil {
		return "", nil, nil, err
	}
	l.h = h
	st.live[id] = l
	crash.Go("workflow run watcher", func() {
		<-h.Done()
		l.ch <- wfxDoneMsg{id: id}
	})
	m.wfxPutRun(h.Snapshot())
	m.wfxCheckBudget(l)
	if !show {
		return id, h, wfxListen(l.ch), nil
	}
	return id, h, tea.Batch(m.wfxShowPage("run:"+id, true), wfxListen(l.ch)), nil
}

func wfxLastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// wfxListen waits for the next message of a run.
func wfxListen(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// handleWfxMsg handles the run messages. ok=false: not ours.
func (m *Model) handleWfxMsg(msg tea.Msg) (tea.Cmd, bool) {
	if cmd, ok := m.handleWfxSchedMsg(msg); ok {
		return cmd, true
	}
	switch msg := msg.(type) {
	case wfxEventMsg:
		l := m.wfxS().live[msg.id]
		if l == nil {
			return nil, true
		}
		if msg.ev.Kind == run.StepFailed || msg.ev.Kind == run.WriteDenied {
			l.addLine(msg.ev.Step + " " + msg.ev.Item + ": " + string(msg.ev.Kind) + " " + msg.ev.Err)
		}
		m.wfxPutRun(l.h.Snapshot())
		if msg.ev.Kind == run.StepDone {
			m.wfxCheckBudget(l)
		}
		return wfxListen(l.ch), true
	case wfxNoteMsg:
		return m.aiInfo(msg.text), true
	case wfxMessageMsg:
		l := m.wfxS().live[msg.id]
		if l == nil {
			return nil, true
		}
		l.addLine("message (" + msg.via + "): " + msg.text)
		return tea.Batch(m.wfxDeliverLocal(msg.via, msg.text, msg.id, true), wfxListen(l.ch)), true
	case wfxLineMsg:
		l := m.wfxS().live[msg.id]
		if l == nil {
			return nil, true
		}
		if msg.text != "" {
			l.addLine(msg.text)
		}
		return wfxListen(l.ch), true
	case wfxGateMsg:
		l := m.wfxS().live[msg.id]
		if l == nil {
			msg.reply <- wire.GateDeny
			return nil, true
		}
		st := m.wfxS()
		e, _ := wire.Find(st.list, l.name)
		a := WorkflowWriteAsk{Workflow: l.name, Step: msg.ask.Step, Item: msg.ask.Item, Target: msg.ask.Target,
			Preview: msg.ask.Preview, Undo: msg.ask.Undo, StateRoot: st.root, Plan: msg.plan}
		if !msg.tool && !msg.plan {
			a.FileHash = e.Hash
		}
		reply := msg.reply
		l.waiting = msg.ask.Step
		a.Answer = func(d WorkflowGateDecision) {
			l.waiting = ""
			switch d {
			case WorkflowGateAllowOnce:
				reply <- wire.GateAllowOnce
			case WorkflowGateAllowWorkflow:
				reply <- wire.GateAllowWorkflow
			default:
				reply <- wire.GateDeny
			}
		}
		return tea.Batch(m.AskWorkflowWrite(a), wfxListen(l.ch)), true
	case wfxDoneMsg:
		st := m.wfxS()
		l := st.live[msg.id]
		if l == nil {
			return nil, true
		}
		rf := l.h.Snapshot()
		delete(st.live, msg.id)
		m.wfxPutRun(rf)
		word := map[string]string{run.RunOK: "done", run.RunFail: "failed", run.RunCancel: "stopped", run.RunPause: "paused"}[rf.State]
		if word == "" {
			word = rf.State
		}
		kind := "run"
		if rf.Dry {
			kind = "dry run"
		}
		return tea.Batch(m.wfxNotifyDone(l, rf), m.aiInfo(l.name+": "+kind+" "+word)), true
	}
	return nil, false
}

// wfxExec runs one argv for shell and tests steps (internal/spawn).
func wfxExec(ctx context.Context, argv []string, dir string) (string, int, error) {
	return spawnOutput(ctx, argv, dir)
}

// workflowsEnabled is the workflows_enabled setting.
func workflowsEnabled() bool {
	v, _ := workflowSettingValue("setting-workflows-enabled")
	return v != "false"
}

// wfxLoc is the zone the view shows times in (tests pin UTC).
var wfxLoc = time.Local

// wfxAgo is a short time for the rows.
func wfxAgo(t, now time.Time) string {
	if t.IsZero() {
		return "—"
	}
	t, now = t.In(wfxLoc), now.In(wfxLoc)
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04")
	}
	return t.Format("Jan 2")
}
