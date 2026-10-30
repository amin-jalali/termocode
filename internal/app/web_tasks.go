package app

import (
	"errors"
	"reflect"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/tasks"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The tasks topic and its requests (WEB-25, web design 2.8). The list is
// what the laptop's task picker lists, each task with its newest run;
// the web starts a task only by its label (no command text ever comes
// from the web) and it runs in a laptop terminal like Alt+R. The list is
// read again when the task runner's books change (a start, an exit), so
// an idle Update costs a short fingerprint. Until the terminal view can
// follow a task, tasks.tail reads the end of its output, capped.

func init() {
	webTopics["tasks"] = webTopicDef{snapshot: webTasksSnapshot, apply: webApplyReplace[webproto.TasksSnap]}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishTasks)
	webOps[webproto.OpTasksRun] = (*Model).webTasksRun
	webOps[webproto.OpTasksTerminate] = (*Model).webTasksTerminate
	webOps[webproto.OpTasksTail] = (*Model).webTasksTail
}

// Tests swap these: start a task's job, stop it, read a terminal's last
// n lines (off the UI thread).
var (
	webRunTask   = func(m *Model, t tasks.Task) { _ = m.runTask(t) }
	webStopTask  = (*Model).stopTaskRun
	webTaskLines = func(c *nvim.Client, buf, n int) ([]string, error) {
		if c == nil {
			return nil, errors.New("the editor is not ready")
		}
		var lines []string
		err := c.EvalLuaArgs(webTaskTailLua, &lines, buf, n)
		return lines, err
	}
)

// webTailSpare is how many rows more than the tail a read asks for: the
// blank rows under a terminal's output.
const webTailSpare = 100

// webTaskTailLua returns the last n lines of a loaded buffer.
const webTaskTailLua = `
	local b, n = ...
	if not vim.api.nvim_buf_is_loaded(b) then return {} end
	local c = vim.api.nvim_buf_line_count(b)
	return vim.api.nvim_buf_get_lines(b, math.max(c - n, 0), -1, false)
`

// webTasksDelta is a tasks delta: the whole list; a newer one replaces a
// queued one.
type webTasksDelta webproto.TasksSnap

func (webTasksDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webTasksDelta)
	return n, ok
}

// webTasksPub is what the tasks topic sent last (Update only).
type webTasksPub struct {
	key  string
	last *webproto.TasksSnap
}

// webTasksKey fingerprints the runner's books: every run's buffer, and
// whether it runs or how it ended.
func (m *Model) webTasksKey() string {
	tr := m.ensureTasks()
	var b strings.Builder
	for _, buf := range tr.order {
		if r := tr.runs[buf]; r != nil {
			b.WriteString(strconv.Itoa(buf))
			if r.Running {
				b.WriteString("r,")
			} else {
				b.WriteString("e" + strconv.Itoa(r.Code) + ",")
			}
		}
	}
	return b.String()
}

func webTasksSnapshot(m *Model, _ string) (any, error) {
	snap := m.webTasksBuild()
	if m.web != nil {
		m.web.tasks = webTasksPub{key: m.webTasksKey(), last: &snap}
	}
	return snap, nil
}

// webPublishTasks sends the list when the runner's books changed and the
// list with them.
func (m *Model) webPublishTasks(tea.Msg) tea.Cmd {
	h := m.web
	p := &h.tasks
	if !h.webListens("tasks") {
		p.last = nil
		return nil
	}
	key := m.webTasksKey()
	if p.last != nil && key == p.key {
		return nil
	}
	p.key = key
	cur := m.webTasksBuild()
	if p.last != nil && reflect.DeepEqual(*p.last, cur) {
		return nil
	}
	p.last = &cur
	h.publish("tasks", webTasksDelta(cur))
	return nil
}

// webTasksBuild lists the tasks with their newest runs.
func (m *Model) webTasksBuild() webproto.TasksSnap {
	list, err := m.allTasks()
	s := webproto.TasksSnap{Tasks: []webproto.TaskItem{}}
	if err != nil {
		s.Error = webName(err.Error())
	}
	tr := m.ensureTasks()
	v := m.taskVars()
	for i, t := range list {
		if i == webproto.MaxTasks {
			break
		}
		it := webproto.TaskItem{Label: t.Label, Group: t.Group, Default: t.IsDefault, Source: t.Source, Detail: webName(t.CommandLine(v))}
		if r := tr.lastRun(t.Label); r != nil {
			last := webproto.TaskLast{State: "running", When: r.Started.UnixMilli(), Buf: r.Buf}
			if !r.Running {
				last.State, last.Code = "ok", r.Code
				if r.Code != 0 {
					last.State = "failed"
				}
			}
			it.Last = &last
		}
		s.Tasks = append(s.Tasks, it)
	}
	return s
}

// webTasksRun starts a task the laptop lists, by its label, in a laptop
// terminal; it answers the terminal's buffer.
func (m *Model) webTasksRun(call *webCall) tea.Cmd {
	label := call.args.(*webproto.LabelArgs).Label
	list, _ := m.allTasks()
	t, ok := tasks.Find(list, label)
	if !ok {
		call.fail(webproto.CodeNotFound, "That task is not defined on the laptop.")
		return nil
	}
	tr := m.ensureTasks()
	before := tr.lastRun(label)
	webRunTask(m, t)
	r := tr.lastRun(label)
	if r == nil || r == before || !r.Running {
		m.webAuditOp(call, label, "failed")
		call.fail(webproto.CodeConflict, "The task did not start. The laptop has the details.")
		return nil
	}
	m.webAuditOp(call, label, "ok")
	call.reply(webproto.TaskStarted{Buf: r.Buf})
	return nil
}

// webTasksRunning is the newest running run of label.
func (m *Model) webTasksRunning(label string) *taskRun {
	for _, r := range m.ensureTasks().running() {
		if r.Task.Label == label {
			return r
		}
	}
	return nil
}

// webTasksTerminate stops the task's running job (the page asked first).
func (m *Model) webTasksTerminate(call *webCall) tea.Cmd {
	label := call.args.(*webproto.LabelArgs).Label
	r := m.webTasksRunning(label)
	if r == nil {
		call.fail(webproto.CodeConflict, "That task is not running.")
		return nil
	}
	webStopTask(m, r)
	m.webAuditOp(call, label, "ok")
	call.reply(nil)
	return nil
}

// webTasksTail answers the end of the task's newest run, read off the UI
// thread.
func (m *Model) webTasksTail(call *webCall) tea.Cmd {
	label := call.args.(*webproto.LabelArgs).Label
	r := m.ensureTasks().lastRun(label)
	if r == nil {
		call.fail(webproto.CodeNotFound, "That task has not run yet.")
		return nil
	}
	buf, running, code, c := r.Buf, r.Running, r.Code, m.nvim
	call.later()
	crash.GoWith("web task tail", func() {
		// A terminal buffer ends in blank rows (the rest of its screen):
		// read some more than needed, then drop them.
		want := webproto.MaxTailLines + webTailSpare
		lines, err := webTaskLines(c, buf, want)
		if err != nil {
			call.fail(webproto.CodeConflict, "The task's output cannot be read now.")
			return
		}
		full := len(lines) >= want // older lines may exist
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		tail := webproto.TaskTail{Buf: buf, Running: running, Code: code, Truncated: full}
		if len(lines) > webproto.MaxTailLines {
			lines, tail.Truncated = lines[len(lines)-webproto.MaxTailLines:], true
		}
		size, from := 0, len(lines)
		for from > 0 && size+len(lines[from-1])+1 <= webproto.MaxTailBytes {
			from--
			size += len(lines[from]) + 1
		}
		if from > 0 {
			tail.Truncated = true
		}
		tail.Lines = append([]string{}, lines[from:]...)
		call.reply(tail)
	}, func() { call.fail(webproto.CodeInternal, "Something went wrong.") })
	return nil
}
