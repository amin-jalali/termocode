package app

import (
	"os"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/worktreeform"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// A new task from the page (CMP-49): the phone or the desktop page asks
// for a new background task: a name (optional: the branch is drafted
// from the task), a start point, an installed agent and one task line.
// The laptop does what Worktree: New… with Create + Start does: git
// worktree add off the UI thread, the include / shared files of
// .termocode/worktrees.json, the agent started in the new worktree with
// the task typed in once it is ready. It only adds (a new branch and
// folder; nothing of the user's changes), so no laptop card waits; the
// page asks first (Cancel first). The repository's setup commands run
// only when the laptop trusts them already; otherwise the laptop asks
// (its own card), as for a worktree made there. A note on the new
// worktree says which device asked and the task.

func init() {
	webOps[webproto.OpWorktreesTaskForm] = (*Model).webWorktreesTaskForm
	webOps[webproto.OpWorktreesNewTask] = (*Model).webWorktreesNewTask
}

// webTaskNoteMax caps the task text kept in the worktree's note.
const webTaskNoteMax = 300

// webWorktreesTaskForm reads the repository off the UI thread and
// answers with what the form offers.
func (m *Model) webWorktreesTaskForm(call *webCall) tea.Cmd {
	if m.webWTState().off {
		call.reply(webTaskFormNone())
		return nil
	}
	root, _ := m.rootDir()
	setting := worktreeDirSetting()
	trusted := loadSession().WorktreeSetupTrusted
	call.later()
	return func() tea.Msg {
		call.reply(webTaskFormOf(wtNewPrep(root, setting), wtNewAgents(), trusted))
		return nil
	}
}

func webTaskFormNone() webproto.WorktreeTaskForm {
	return webproto.WorktreeTaskForm{State: "none", Refs: []string{}, Agents: []webproto.WorktreeTaskAgent{}}
}

// webTaskFormOf is the form's answer for a read repository (pure).
func webTaskFormOf(prep wtNewPrepMsg, ags []agents.Agent, trusted string) webproto.WorktreeTaskForm {
	if prep.err != nil || prep.f == nil {
		return webTaskFormNone()
	}
	f := prep.f
	home, _ := os.UserHomeDir()
	out := webproto.WorktreeTaskForm{State: "ready", From: webName(f.from), Refs: []string{}, Agents: []webproto.WorktreeTaskAgent{},
		Base: webName(homeShort(f.base, home))}
	for _, r := range f.refs {
		if len(out.Refs) == webproto.MaxTaskRefs {
			break
		}
		if r = webName(r); r != "" {
			out.Refs = append(out.Refs, r)
		}
	}
	for _, a := range ags {
		if len(out.Agents) == webproto.MaxTaskAgents {
			break
		}
		out.Agents = append(out.Agents, webproto.WorktreeTaskAgent{ID: a.ID, Title: webName(a.Title)})
	}
	if len(f.cfg.Setup) > 0 && f.cfgErr == "" {
		out.Setup = "asks"
		if worktreeform.SetupTrusted(f.hash, trusted) {
			out.Setup = "trusted"
		}
	}
	return out
}

// webTaskAgent is the installed agent with id.
func webTaskAgent(id string) (agents.Agent, bool) {
	for _, a := range wtNewAgents() {
		if a.ID == id {
			return a, true
		}
	}
	return agents.Agent{}, false
}

// wtWebTaskMsg is the end of a page's new task (made or failed).
type wtWebTaskMsg struct {
	call           *webCall
	f              *wtForm
	folder, branch string
	files          []worktreeform.File
	agent          agents.Agent
	task, device   string
	code           webproto.ErrCode // a failure: the answer's code …
	err            string           // … and text
}

// webWorktreesNewTask checks the task and makes the worktree off the UI
// thread; onWTWebTask finishes on the laptop.
func (m *Model) webWorktreesNewTask(call *webCall) tea.Cmd {
	a := call.args.(*webproto.WorktreeTaskArgs)
	if m.webWTState().off {
		call.fail(webproto.CodeConflict, "New tasks cannot start here.")
		return nil
	}
	ag, ok := webTaskAgent(a.Agent)
	if !ok {
		call.fail(webproto.CodeNotFound, "That agent is not installed on the laptop.")
		return nil
	}
	task := strings.TrimSpace(a.Task)
	branch := worktreeform.BranchFromName(strings.TrimSpace(a.Name))
	if branch == "" {
		branch = worktreeform.BranchFromTask(task, "task/") // CMP-26 rules; web tasks go under task/
	}
	if branch == "" {
		call.fail(webproto.CodeBadRequest, "Type a name for the task.")
		return nil
	}
	device := webName(call.conn.device)
	if device == "" {
		device = "web"
	}
	root, _ := m.rootDir()
	setting := worktreeDirSetting()
	from := strings.TrimSpace(a.From)
	call.later()
	return func() tea.Msg {
		msg := wtWebTaskMsg{call: call, branch: branch, agent: ag, task: task, device: device}
		prep := wtNewPrep(root, setting)
		if prep.err != nil || prep.f == nil {
			msg.code, msg.err = webproto.CodeConflict, "This folder is not in a git repository."
			return msg
		}
		msg.f = prep.f
		if from == "" {
			from = prep.f.from
		}
		msg.folder = worktreeform.Folder(prep.f.base, branch)
		files, err := wtCreateRun(prep.f, msg.folder, branch, from)
		if err != nil {
			msg.code, msg.err = webproto.CodeConflict, webSentence(wtCreateError(err))
			return msg
		}
		msg.files = files
		return msg
	}
}

// onWTWebTask stores the new worktree, answers the page, starts the
// agent with the task and runs (or asks for) the setup.
func (m *Model) onWTWebTask(msg wtWebTaskMsg) tea.Cmd {
	call := msg.call
	if msg.err != "" {
		m.webAuditOp(call, msg.branch, "failed")
		call.fail(msg.code, msg.err)
		return nil
	}
	f := msg.f
	now := clock.Now()
	_ = worktrees.Update(f.common, func(mm map[string]worktrees.Meta) {
		md := mm[msg.folder]
		md.Created, md.LastSeen = now, now
		md.Agent = msg.agent.ID
		mm[msg.folder] = md
	})
	_ = wtAddNote(f.common, msg.folder, "Task from "+msg.device+": "+webTaskCut(msg.task, webTaskNoteMax), "")
	st := m.webWTState()
	st.cursorPath = msg.folder
	setup := len(f.cfg.Setup) > 0 && f.cfgErr == ""
	trusted := setup && worktreeform.SetupTrusted(f.hash, loadSession().WorktreeSetupTrusted)
	home, _ := os.UserHomeDir()
	done := webproto.WorktreeTaskDone{ID: webWorktreeID(msg.folder), Branch: webName(msg.branch), Where: webName(homeShort(msg.folder, home))}
	cmds := []tea.Cmd{
		m.pushToastDetail(toast.Info, "Task "+msg.branch+" started from "+msg.device, wtShortPath(msg.folder)+theme.ASCIIText(" · ")+msg.agent.Title),
		m.webWorktreesReload(),
		m.startAgentWithTask(msg.agent, msg.folder, msg.task),
	}
	switch {
	case !setup:
	case trusted:
		cmds = append(cmds, m.wtSetupAsk(f, msg.folder))
	case m.confirmOpen || m.connectOpen || m.pickerOpen:
		done.Note = "The repository's setup did not run: the laptop was asking something else. Run it there."
	default:
		cmds = append(cmds, m.wtSetupAsk(f, msg.folder))
		done.Note = "The repository's setup asks on the laptop first."
	}
	m.webAuditOp(call, msg.branch, "ok")
	call.reply(done)
	return tea.Batch(cmds...)
}

// webTaskCut is s cut to at most n bytes on a rune border, with … when
// cut.
func webTaskCut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
