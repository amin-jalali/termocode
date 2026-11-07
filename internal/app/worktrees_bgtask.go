package app

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/bgtask"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── Background agent tasks in a sandbox (CMP-1) ──────────────────────────
//
// b in WORKTREES (Worktree: Run Agent Task in Background…) asks for a
// task, then a card — Cancel first and focused — shows what starts: the
// agent in its headless mode, in a throwaway container on this host's
// engine, seeing only the worktree and the git common dir, with the API
// key variables passed by name. Start runs `run -d`: the task keeps
// working when termocode, the session or (with termocode on your own
// server) the laptop is gone. B (Worktree: Background Tasks…) lists every
// task container with its state; Enter on one offers Follow output (a
// job tab with `logs -f`), Stop (running) or Remove container (ended;
// the worktree and its changes stay). A remote engine context is refused:
// its bind mounts would name another machine's folders.

const (
	confirmKindWTBgTask    confirmKindEnum = 3881
	confirmKindWTBgTaskAct confirmKindEnum = 3882
	promptKindWTBgTask     promptKindEnum  = 3883
	pickerKindWTBgTasks    pickerKindEnum  = 3884

	wtBgTaskPaletteID  = "worktree-bg-task"
	wtBgTasksPaletteID = "worktree-bg-tasks"
	wtBgTaskPfx        = "bgtask:"
	wtBgTaskTimeout    = 2 * time.Minute // a first run may pull the image
	wtBgListTimeout    = 15 * time.Second
)

// wtBgPending is a task between the prompt and the card's answer.
type wtBgPending struct {
	title, path, common, agent string
	spec                       bgtask.Spec
	engine                     containers.Engine
}

// wtBgActPending is the open card of one listed task.
type wtBgActPending struct {
	task   bgtask.Task
	engine containers.Engine
}

// Tests swap these: the engine lookup and its calls.
var (
	wtBgLocate = func(contextName string) containers.Engine {
		return containers.Locate(containers.DetectOptions{Preferred: preferredContainerEngine(), Context: contextName,
			Exec: servicesExec, Dir: os.TempDir(), Android: runtime.GOOS == "android"})
	}
	wtBgRun = func(ctx context.Context, e containers.Engine, argv []string) (string, string, int, error) {
		x := e.X
		if x == nil {
			x = servicesExec
		}
		if env := e.ContextEnv(); env != "" {
			x = services.WithEnv(x, env)
		}
		return x.Capture(ctx, "", append([]string{e.Binary}, argv...)...)
	}
)

// wtBgContext is the engine context the task would run on ("" = the
// engine's default) and whether that is this host.
func (m *Model) wtBgContext() (string, bool) {
	name := os.Getenv("DOCKER_CONTEXT")
	session := ""
	if m.services != nil {
		session = m.services.sessionContext
		if c := m.services.activeContext(); c != "" {
			name = c
		}
	}
	return session, bgtask.Local(name, os.Getenv("DOCKER_HOST"))
}

// wtBgAgent is the agent for it: its own agent when that one has a
// headless mode, else the first installed one that has, else Claude Code.
func wtBgAgent(it wtItem) string {
	if bgtask.Supported(it.e.Meta.Agent) {
		return it.e.Meta.Agent
	}
	for _, id := range bgtask.SupportedIDs() {
		if a, ok := agents.ByID(id); ok && agentInstalled(a) {
			return id
		}
	}
	return "claude"
}

func wtBgAgentTitle(id string) string {
	if a, ok := agents.ByID(id); ok {
		return a.Title
	}
	return id
}

// worktreeBgTask is b: ask for the task of the cursor's worktree.
func (m *Model) worktreeBgTask() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	switch {
	case it.e.Gone:
		return m.pushToastDetail(toast.Info, "Its folder is gone", "F forgets it.")
	case it.e.Meta.State == worktrees.StateArchived:
		return m.pushToastDetail(toast.Info, "This worktree is archived", "x restores its folder first.")
	}
	if _, local := m.wtBgContext(); !local {
		return m.pushToastDetail(toast.Warn, "The container engine is on another machine",
			"Background tasks mount this host's folders. Switch to a local context, or run termocode on that server.")
	}
	ag := wtBgAgent(it)
	m.wt.bgAsk = &wtBgPending{title: wtTitle(it), path: it.e.Path, common: m.wt.common, agent: ag}
	m.prompt = prompt.New(" Background task ", "The task for "+wtBgAgentTitle(ag)+" in "+wtTitle(it)+":", "")
	m.prompt.SetNote("It runs on its own in a sandbox container; a card shows everything before it starts.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindWTBgTask
	return nil
}

// onWTBgTask builds the task and opens the card.
func (m *Model) onWTBgTask(value string) tea.Cmd {
	if m.wt == nil {
		return nil
	}
	p := m.wt.bgAsk
	if p == nil {
		return nil
	}
	task := strings.TrimSpace(value)
	if task == "" {
		m.wt.bgAsk = nil
		return nil
	}
	p.spec = bgtask.Spec{ID: bgtask.NewID(clock.Now()), Worktree: p.path, Common: p.common, Agent: p.agent, Task: task, User: wtBgUser()}
	argv, err := bgtask.RunArgv(p.spec)
	if err != nil {
		m.wt.bgAsk = nil
		return m.pushToastDetail(toast.Warn, "The task cannot start", err.Error())
	}
	session, _ := m.wtBgContext()
	p.engine = wtBgLocate(session)
	if p.engine.Binary == "" {
		m.wt.bgAsk = nil
		return m.pushToastDetail(toast.Warn, "No container engine", "Background tasks need docker or podman on this host.")
	}
	var b strings.Builder
	b.WriteString(wtBgAgentTitle(p.agent) + " works on its own in a throwaway " + p.engine.Kind + " container (" + bgtask.DefaultImage + ").\n\n")
	b.WriteString("It keeps running when termocode or this session ends. With termocode on your own server, the laptop can be off.\n\n")
	b.WriteString("It sees only:\n  " + wtShortPath(p.path) + " (its edits land here)")
	if p.common != "" && !strings.HasPrefix(p.common+"/", p.path+"/") {
		b.WriteString("\n  " + wtShortPath(p.common) + " (git)")
	}
	b.WriteString("\n\nThe agent's own approval prompts are off: nobody is there to answer them, the container is the sandbox. ")
	b.WriteString("Its network is on (the agent needs its API).\n\n")
	b.WriteString("Key variables passed by name (values never shown): " + strings.Join(bgtask.KeyEnv, ", ") + ".\n\n")
	b.WriteString("Runs: " + wtBgPreview(p.engine.Binary, argv))
	m.confirm = confirm.Risky("Start "+wtBgAgentTitle(p.agent)+" in the background?", b.String(),
		confirm.Button{ID: "cancel", Title: "Cancel"}, confirm.Button{ID: "start", Title: "Start in background", Style: confirm.StylePrimary}).WithWideText().WithScrollText() // the whole command stays readable
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWTBgTask
	return nil
}

// wtBgPreview is the command line for the card (the task shortened).
func wtBgPreview(bin string, argv []string) string {
	words := []string{bin}
	for i, a := range argv {
		if i == len(argv)-1 || strings.ContainsAny(a, "\n") {
			a = bgtask.Title(a)
			if r := []rune(a); len(r) > 60 {
				a = string(r[:59]) + "…"
			}
		}
		if q, ok := agents.ShellWord(a); ok {
			a = q
		}
		words = append(words, a)
	}
	return strings.Join(words, " ")
}

// wtBgUser is the host user the agent runs as ("" on Windows).
func wtBgUser() string {
	uid, gid := os.Getuid(), os.Getgid()
	if uid < 0 || gid < 0 {
		return ""
	}
	return strconv.Itoa(uid) + ":" + strconv.Itoa(gid)
}

// answerWTBgTask answers the start card ("" = Esc = Cancel).
func (m *Model) answerWTBgTask(id string) tea.Cmd {
	if m.wt == nil {
		return nil
	}
	p := m.wt.bgAsk
	m.wt.bgAsk = nil
	if p == nil || id != "start" {
		return nil
	}
	argv, err := bgtask.RunArgv(p.spec)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "The task cannot start", err.Error())
	}
	e, title, agent := p.engine, p.title, wtBgAgentTitle(p.agent)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), wtBgTaskTimeout)
		defer cancel()
		_, stderr, code, err := wtBgRun(ctx, e, argv)
		return applyMsg(func(m *Model) tea.Cmd {
			if err != nil || code != 0 {
				detail := strings.TrimSpace(stderr)
				if detail == "" && err != nil {
					detail = err.Error()
				}
				return m.pushToastDetail(toast.Warn, "The background task did not start", wtBgTail(detail))
			}
			return m.pushToastDetail(toast.Info, agent+" works on "+title+" in the background",
				"B in WORKTREES lists it; Enter follows its output.")
		})
	}
}

func wtBgTail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// worktreeBgTasks is B: list the task containers.
func (m *Model) worktreeBgTasks() tea.Cmd {
	if m.wt == nil {
		m.wt = newWTState()
	}
	session, _ := m.wtBgContext()
	e := wtBgLocate(session)
	if e.Binary == "" {
		return m.pushToastDetail(toast.Info, "No container engine", "Background tasks need docker or podman on this host.")
	}
	m.openWTBgTasksPicker(nil, "Reading the tasks…")
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), wtBgListTimeout)
		defer cancel()
		out, stderr, code, err := wtBgRun(ctx, e, bgtask.ListArgv(e.Kind))
		return applyMsg(func(m *Model) tea.Cmd {
			if !m.pickerOpen || m.pickerKind != pickerKindWTBgTasks {
				return nil
			}
			if err != nil || code != 0 {
				d := strings.TrimSpace(stderr)
				if d == "" && err != nil {
					d = err.Error()
				}
				m.openWTBgTasksPicker(nil, "The engine did not answer: "+wtBgTail(d))
				return nil
			}
			m.wt.bgEngine, m.wt.bgTasks = e, bgtask.ParseList(out)
			m.openWTBgTasksPicker(m.wt.bgTasks, "")
			return nil
		})
	}
}

// wtBgStateWord is a task's state for its row.
func wtBgStateWord(t bgtask.Task) string {
	switch t.State {
	case bgtask.Running:
		return "working"
	case bgtask.Done:
		return "done"
	case bgtask.Failed:
		if t.Exit >= 0 {
			return "failed (exit " + strconv.Itoa(t.Exit) + ")"
		}
		return "failed"
	}
	return strings.ToLower(t.Status)
}

// openWTBgTasksPicker shows the rows (none: a hint row).
func (m *Model) openWTBgTasksPicker(tasks []bgtask.Task, note string) {
	var items []picker.Item
	for _, t := range tasks {
		title := t.Title
		if title == "" {
			title = t.ID
		}
		items = append(items, picker.Item{ID: wtBgTaskPfx + t.ID, Title: title,
			Hint: wtBgStateWord(t) + " · " + wtBgAgentTitle(t.Agent) + " · " + wtShortPath(t.Worktree)})
	}
	if len(items) == 0 {
		items = []picker.Item{{ID: "none", Title: "No background tasks", Hint: "b on a worktree starts one"}}
	}
	m.picker = picker.NewItems(" Background tasks ", items)
	if note != "" {
		m.picker.SetNote([]string{note})
	}
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindWTBgTasks
}

// onWTBgTaskPicked opens the card of one task.
func (m *Model) onWTBgTaskPicked(id string) tea.Cmd {
	tid, ok := strings.CutPrefix(id, wtBgTaskPfx)
	if !ok || m.wt == nil {
		return nil
	}
	for _, t := range m.wt.bgTasks {
		if t.ID != tid {
			continue
		}
		m.wt.bgAct = &wtBgActPending{task: t, engine: m.wt.bgEngine}
		text := wtBgAgentTitle(t.Agent) + " · " + wtBgStateWord(t) + " (" + t.Status + ")\nIn " + wtShortPath(t.Worktree) +
			"\n\n" + t.Title + "\n\nIts edits are in the worktree; Remove container keeps them."
		buttons := []confirm.Button{{ID: "logs", Title: "Follow output", Style: confirm.StylePrimary}}
		if t.State == bgtask.Running {
			buttons = append(buttons, confirm.Button{ID: "stop", Title: "Stop the task", Style: confirm.StyleDestructive})
		} else {
			buttons = append(buttons, confirm.Button{ID: "remove", Title: "Remove container"})
		}
		m.confirm = confirm.Risky("Background task "+t.ID, text, confirm.Button{ID: "cancel", Title: "Close"}, buttons...)
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindWTBgTaskAct
		return nil
	}
	return nil
}

// answerWTBgTaskAct answers a task's card ("" = Esc = Close).
func (m *Model) answerWTBgTaskAct(id string) tea.Cmd {
	if m.wt == nil {
		return nil
	}
	p := m.wt.bgAct
	m.wt.bgAct = nil
	if p == nil {
		return nil
	}
	t, e := p.task, p.engine
	switch id {
	case "logs":
		words := []string{e.Binary}
		words = append(words, bgtask.LogsArgv(t.ID)...)
		line := strings.Join(words, " ")
		env := map[string]any{}
		if kv := e.ContextEnv(); kv != "" {
			k, v, _ := strings.Cut(kv, "=")
			env[k] = v
		}
		if _, err := m.spawnTerminalJob(terminalJob{Label: "task " + t.ID, Cmdline: line, Dir: t.Worktree, Env: env, Service: wtBgTaskPfx + t.ID}); err != nil {
			return m.pushToastDetail(toast.Warn, "The output could not be shown", err.Error())
		}
		return nil
	case "stop", "remove":
		argv := bgtask.StopArgv(t.ID)
		done := "Stopped task " + t.ID
		if id == "remove" {
			argv, done = bgtask.RemoveArgv(t.ID), "Removed the container of task "+t.ID
		}
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), wtBgListTimeout*2)
			defer cancel()
			_, stderr, code, err := wtBgRun(ctx, e, argv)
			return applyMsg(func(m *Model) tea.Cmd {
				if err != nil || code != 0 {
					d := strings.TrimSpace(stderr)
					if d == "" && err != nil {
						d = err.Error()
					}
					return m.pushToastDetail(toast.Warn, "The engine said no", wtBgTail(d))
				}
				return m.pushToastDetail(toast.Info, done, "The worktree and its changes stay.")
			})
		}
	}
	return nil
}

func init() {
	aiActionPolicies[wtBgTaskPaletteID] = aiNever(aiNoPicker + "; the user writes the task and starts it on the card")
	aiActionPolicies[wtBgTasksPaletteID] = aiNever(aiNoPicker)
}
