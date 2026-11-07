package app

import (
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/profileform"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/worktreeform"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// Start an agent in a new lane from the phone (MOB-65, mobile plan 3.9).
// The laptop does what Worktree: New… with Create + Start does: a new
// git worktree on a lane branch next to the repository (the repository's
// .termocode/worktrees.json files and setup apply, setup after its own
// trust card) and the agent started in it, the task typed once its input
// box shows. The phone's request becomes a laptop card first (Cancel
// first) naming the agent, the folder, the branch and the task. Only an
// agent whose screen termocode reads is offered: the task must be typed
// for it. Sign-in stays on the laptop.

func init() {
	webOps[webproto.OpAILaneAgents] = (*Model).webAILaneAgents
	webAskLaterOps[webproto.OpAILaneStart] = (*Model).webAILaneStart
}

// webLaneStartAgent starts the agent in the new worktree (a var: tests).
var webLaneStartAgent = func(m *Model, a agents.Agent, dir, task string) tea.Cmd {
	return m.startAgentWithTask(a, dir, task)
}

// webLaneTaskShown caps the task on the laptop's card.
const webLaneTaskShown = 300

// webLaneAgents are the agents a lane can start: installed, with screen
// rules, and their sign-in as a word (cli is the laptop's last check).
func webLaneAgents(cli map[string]profileform.CLIState) []webproto.AILaneAgent {
	out := []webproto.AILaneAgent{}
	for _, a := range agents.Registry {
		if len(out) == webproto.MaxAILaneAgents {
			break
		}
		if !agents.HasScreenRules(a.ID) || !agentInstalled(a) {
			continue
		}
		st := "unknown"
		switch agentSignInState(cli, a.ID) {
		case profileform.CLIReady:
			st = "ready"
		case profileform.CLILoggedOut:
			st = "signin"
		}
		out = append(out, webproto.AILaneAgent{ID: a.ID, Title: webName(a.Title), State: st})
	}
	return out
}

func (m *Model) webAILaneAgents(call *webCall) tea.Cmd {
	var cli map[string]profileform.CLIState
	if m.ai != nil {
		cli = maps.Clone(m.ai.cliStates)
	}
	root, _ := m.rootDir()
	call.later()
	return func() tea.Msg {
		out := webproto.AILaneAgents{Items: webLaneAgents(cli), NoRepo: root == "" || !git.IsRepo(root)}
		call.reply(out)
		return nil
	}
}

// webLane is a lane the laptop card would make.
type webLane struct {
	f              *wtForm
	agent          agents.Agent
	task, from     string
	branch, folder string
}

// webLaneBranch is the lane's branch: "lane/<agent>-<first words of
// the task>".
func webLaneBranch(agent, task string) string {
	words := strings.Fields(strings.ReplaceAll(task, "/", " "))
	if len(words) > 5 {
		words = words[:5]
	}
	b := worktreeform.BranchFromName(agent + " " + strings.Join(words, " "))
	if len(b) > 48 {
		b = b[:48]
		if i := strings.LastIndexByte(b, '-'); i > len(agent) {
			b = b[:i]
		}
		b = strings.TrimRight(b, "-")
	}
	if b == "" {
		b = agent
	}
	return "lane/" + b
}

// webLanePlan reads the repository (off the UI thread) and picks the
// start point, a free branch and its folder. The error is for people.
func webLanePlan(root, setting string, a agents.Agent, task, from string) (*webLane, webproto.ErrCode, string) {
	prep := wtNewPrep(root, setting)
	if prep.err != nil || prep.f == nil {
		return nil, webproto.CodeBadRequest, "The laptop's folder is not in a git repository: a lane needs one."
	}
	f := prep.f
	switch {
	case from == "":
		from = f.from
	case !slices.Contains(f.refs, from):
		return nil, webproto.CodeBadRequest, "That start point is not a branch or tag on the laptop."
	}
	base := webLaneBranch(a.ID, task)
	taken := func(branch, folder string) bool {
		if slices.Contains(f.refs, branch) {
			return true
		}
		_, err := os.Lstat(folder)
		return err == nil || !os.IsNotExist(err)
	}
	branch := base
	folder := worktreeform.Folder(f.base, branch)
	for n := 2; taken(branch, folder); n++ {
		if n > 99 {
			return nil, webproto.CodeConflict, "Too many lanes for this task. Remove old ones on the laptop first."
		}
		branch = base + "-" + strconv.Itoa(n)
		folder = worktreeform.Folder(f.base, branch)
	}
	return &webLane{f: f, agent: a, task: task, from: from, branch: branch, folder: folder}, "", ""
}

func (m *Model) webAILaneStart(call *webCall, open func(*webAsk) tea.Msg) tea.Cmd {
	args := call.args.(*webproto.AILaneStartArgs)
	a, ok := agents.ByID(args.Agent)
	if !ok {
		call.fail(webproto.CodeNotFound, "The laptop has no such agent.")
		return nil
	}
	if !agents.HasScreenRules(a.ID) {
		call.fail(webproto.CodeBadRequest, "termocode cannot read "+a.Title+"'s screen, so the task cannot be typed safely. Start it on the laptop.")
		return nil
	}
	task := strings.Join(strings.Fields(args.Task), " ") // one line, typed into the agent
	if aiLooksSecret(task) {
		call.fail(webproto.CodeBadRequest, "The task looks like it holds a key or a password. Leave it out: the agent reads secrets on the laptop.")
		return nil
	}
	root, err := m.rootDir()
	if err != nil {
		call.fail(webproto.CodeBadRequest, "The laptop's folder is not in a git repository: a lane needs one.")
		return nil
	}
	setting := worktreeDirSetting()
	from := args.From
	call.later()
	return func() tea.Msg {
		if !agentInstalled(a) {
			call.fail(webproto.CodeNotFound, a.Title+" is not installed on the laptop.")
			return nil
		}
		lane, code, why := webLanePlan(root, setting, a, task, from)
		if lane == nil {
			call.fail(code, why)
			return nil
		}
		shown := task
		if len(shown) > webLaneTaskShown {
			shown = strings.ToValidUTF8(shown[:webLaneTaskShown], "") + "…"
		}
		body := "Start " + a.Title + " in a new git worktree?\n\n" +
			"Folder: " + wtShortPath(lane.folder) + "\n" +
			"Branch: " + lane.branch + " (from " + lane.from + ")\n" +
			"Task: " + aiCleanText(shown) + "\n\n" +
			"The agent runs in a terminal tab on this laptop and works only in that folder. Your files change only when you merge its branch."
		return open(&webAsk{
			summary: a.ID + " " + lane.branch,
			title:   "Start an agent in a new lane?",
			body:    body,
			action:  "Start",
			run:     func(m *Model) tea.Cmd { return webLaneCreateCmd(lane) },
		})
	}
}

// webLaneCreateCmd makes the worktree off the UI thread, then starts the
// agent in it.
func webLaneCreateCmd(lane *webLane) tea.Cmd {
	return func() tea.Msg {
		files, err := wtCreateRun(lane.f, lane.folder, lane.branch, lane.from)
		return applyMsg(func(m *Model) tea.Cmd { return m.onWebLaneCreated(lane, files, err) })
	}
}

// onWebLaneCreated saves the lane's agent in worktrees.json (so the
// WORKTREES view and the worktrees topic show it as an agent's) and
// starts the agent with the task.
func (m *Model) onWebLaneCreated(lane *webLane, files []worktreeform.File, err error) tea.Cmd {
	if err != nil {
		return m.pushToastDetail(toast.Errr, "No lane was made", wtCreateError(err))
	}
	_ = worktrees.Update(lane.f.common, func(mm map[string]worktrees.Meta) {
		md := mm[lane.folder]
		md.Created, md.LastSeen = clock.Now(), clock.Now()
		md.Agent = lane.agent.ID
		mm[lane.folder] = md
	})
	detail := lane.agent.Title + " starts in " + wtShortPath(lane.folder)
	if c, l := worktreeform.Counts(files); c+l > 0 {
		detail += " · copied " + strconv.Itoa(c) + " · linked " + strconv.Itoa(l)
	}
	cmds := []tea.Cmd{
		m.pushToastDetail(toast.Info, "Lane "+lane.branch+" is ready", detail),
		m.worktreesReload(),
		webLaneStartAgent(m, lane.agent, lane.folder, lane.task),
	}
	if m.wt != nil && len(lane.f.cfg.Setup) > 0 && lane.f.cfgErr == "" {
		cmds = append(cmds, m.wtSetupAsk(lane.f, lane.folder))
	}
	return tea.Batch(cmds...)
}
