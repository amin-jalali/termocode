package app

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentctl"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
)

// Start a background agent task from outside (CMP-39): `termocode agent
// start <task…>` and the MCP tool agent_task_start send a task-start
// request over the control socket. termocode opens its New worktree
// card filled in (a name drafted from the task, the agent, the task);
// Cancel is first and nothing is made until the user presses Create +
// Start. The caller gets the new worktree's path and branch back, so a
// script can follow the agent with `termocode agent list / wait`.

// wtTaskPreset is what a task-start fills into the New worktree card.
type wtTaskPreset struct {
	name, from, task string
	agent            string // an agent id ("" = the first installed one)
	asker            string // who asks, in plain words
}

// wtTaskAsker names the caller on the card.
const (
	wtTaskAskerCLI = "termocode agent start"
	wtTaskAskerMCP = "an outside agent (agent_task_start)"
)

// taskNameFromTask drafts the worktree name from the task: "task/" and
// its first words (the branch is made from the name).
func taskNameFromTask(task string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(task), "\n")
	words := strings.Fields(strings.NewReplacer("/", " ", "\\", " ").Replace(line))
	if len(words) > 5 {
		words = words[:5]
	}
	if len(words) == 0 {
		return ""
	}
	return "task/" + strings.Join(words, " ")
}

// onTaskStartCtl checks a task-start request and opens the filled-in New
// worktree card (the repository is read off the UI thread first).
func (m *Model) onTaskStartCtl(call agentCtlCall) tea.Cmd {
	r := call.req
	fail := func(s string) tea.Cmd { call.reply <- agentctl.Reply{Error: s}; return nil }
	task := strings.TrimSpace(r.Text)
	switch {
	case task == "":
		return fail("the task is empty")
	case len(r.Text) > 32<<10:
		return fail("the task is over 32 KB")
	case m.wt.off:
		return fail("this termocode runs a demo")
	case m.wt.taskAsk || m.wt.form != nil || m.connectOpen || m.confirmOpen:
		return fail("termocode is asking something else: try again")
	}
	pre := &wtTaskPreset{name: strings.TrimSpace(r.Name), from: strings.TrimSpace(r.From), task: task, agent: strings.TrimSpace(r.Agent), asker: wtTaskAskerCLI}
	if r.Tab == wtTaskMCPTab {
		pre.asker = wtTaskAskerMCP
	}
	if pre.agent != "" {
		if _, ok := agents.ByID(pre.agent); !ok {
			return fail("unknown agent " + pre.agent + " (one of " + strings.Join(taskAgentIDs(), ", ") + ")")
		}
	}
	if pre.name == "" {
		pre.name = taskNameFromTask(task)
	}
	root := strings.TrimSpace(r.Dir)
	if root == "" {
		root, _ = m.rootDir()
	}
	setting := worktreeDirSetting()
	m.wt.taskAsk = true
	c := call
	return func() tea.Msg {
		msg := wtNewPrep(root, setting)
		msg.ctl, msg.pre = &c, pre
		return msg
	}
}

// taskAgentIDs lists the agent ids a task can start.
func taskAgentIDs() []string {
	out := make([]string, 0, len(agents.Registry))
	for _, a := range agents.Registry {
		out = append(out, a.ID)
	}
	return out
}

// wtTaskAgents puts the asked agent first among the installed ones (so
// it is on the card's Agent row); ok=false: it is not installed.
func wtTaskAgents(installed []agents.Agent, id string) ([]agents.Agent, bool) {
	if id == "" {
		return installed, len(installed) > 0
	}
	out := make([]agents.Agent, 0, len(installed))
	found := false
	for _, a := range installed {
		if a.ID == id {
			out = append([]agents.Agent{a}, out...)
			found = true
		} else {
			out = append(out, a)
		}
	}
	return out, found
}

// wtTaskFill fills the opened form from the preset (the card reads
// f.vals once and then the typed values).
func wtTaskFill(f *wtForm, pre *wtTaskPreset) {
	from := f.from
	if pre.from != "" {
		from = pre.from
	}
	f.vals = map[string]string{"name": pre.name, "from": from, "task": pre.task}
	f.pick = 0
	if len(f.agents) > 0 {
		f.pick = 1
	}
	f.asker = pre.asker
}

// wtTaskReply answers the waiting task-start caller, once.
func (f *wtForm) wtTaskReply(rep agentctl.Reply) {
	if f == nil || f.ctl == nil {
		return
	}
	f.ctl.reply <- rep
	f.ctl = nil
}

// wtTaskMCPTab marks a task-start sent by the MCP tool (it has no tab).
const wtTaskMCPTab = "mcp"

// agentCtlHandleCtx is agentCtlHandle for a caller inside this process
// (the MCP tool): it gives up when ctx ends.
func agentCtlHandleCtx(ctx context.Context, req agentctl.Request) agentctl.Reply {
	call := agentCtlCall{req: req, reply: make(chan agentctl.Reply, 1)}
	select {
	case agentCtlReqs <- call:
	case <-ctx.Done():
		return agentctl.Reply{Error: ctx.Err().Error()}
	}
	select {
	case r := <-call.reply:
		return r
	case <-ctx.Done():
		return agentctl.Reply{Error: "no answer from the user yet (" + ctx.Err().Error() + "); the card stays open"}
	}
}

// aiTaskStartTool is agent_task_start for outside agents (the MCP server
// only: the chat has outside_agent_start and agent_race_start). It asks
// on the New worktree card itself, so the bridge adds no card.
func aiTaskStartTool(root string) ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	return ai.Tool{
		Name: "agent_task_start",
		Description: "Hand a task to a new background agent: termocode makes a new git worktree, starts a coding agent in it " +
			"and types the task. The user sees the New worktree card filled in and must press Create + Start (Cancel is first). " +
			"Returns the new worktree's path and branch; follow it with worktrees_list.",
		Params: map[string]any{
			"task":  str("what the agent should do (typed into it once it is ready)"),
			"agent": map[string]any{"type": "string", "enum": taskAgentIDs(), "description": "optional: which agent; the first installed one when left out"},
			"name":  str("optional: the worktree's name (the branch is made from it); drafted from the task when left out"),
			"from":  str("optional: the branch, tag or commit it starts from; the current branch when left out"),
		},
		Required: []string{"task"},
		Describe: func(a map[string]any) string { return "Start a background agent task" },
		Validate: func(a map[string]any) error {
			if strings.TrimSpace(ai.ArgString(a, "task")) == "" {
				return errors.New("task is empty")
			}
			return nil
		},
		// The card is the question: no second card from the bridge, but
		// not read only either.
		Gate: func(map[string]any) (bool, string) { return false, "" },
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			ctx, cancel := context.WithTimeout(ctx, agentctl.TaskStartTimeout)
			defer cancel()
			rep := agentCtlHandleCtx(ctx, agentctl.Request{Op: agentctl.OpTaskStart, Tab: wtTaskMCPTab, Dir: root,
				Text: ai.ArgString(a, "task"), Agent: ai.ArgString(a, "agent"), Name: ai.ArgString(a, "name"), From: ai.ArgString(a, "from")})
			if !rep.OK {
				return "", errors.New(rep.Error)
			}
			return taskStartText(rep), nil
		},
	}
}

// taskStartText is a task-start reply in words.
func taskStartText(rep agentctl.Reply) string {
	if len(rep.Worktrees) == 0 {
		return "Done."
	}
	w := rep.Worktrees[0]
	if rep.State == "started" {
		return "Started " + w.Agent + " in the new worktree " + w.Path + " (branch " + w.Branch + ")."
	}
	return "The user made the worktree " + w.Path + " (branch " + w.Branch + ") but started no agent."
}
