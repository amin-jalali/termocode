package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// workflow_control: the AI does what the Workflows view's run keys do
// (AI parity): stop a live run (x), pause it before its next step and let
// it go on (p), turn a schedule on or off (o), retry an ended run from
// its failed part (t), undo a write of an ended run (u) and read the
// trigger events (Workflows: Trigger Events…).
//
// It asks exactly where the view asks: stop, on, off, retry and
// undo_write show a card there (the safe answer first), so each asks
// every time here, with no "Always allow". pause, continue and triggers
// have no card in the view and run at once. Every write a retried run
// makes still asks on its own gate card.

// aiWfxControlOps are the ops; true: the op asks every time.
var aiWfxControlOps = map[string]bool{
	"stop": true, "on": true, "off": true, "retry": true, "undo_write": true,
	"pause": false, "continue": false, "triggers": false,
}

// aiWfxControlUndoWait caps an undo's command.
var aiWfxControlUndoWait = 2 * time.Minute

func aiWfxControlOpList() []string {
	ops := make([]string, 0, len(aiWfxControlOps))
	for op := range aiWfxControlOps {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	return ops
}

// aiWfxControlReq is a checked call.
type aiWfxControlReq struct {
	op    string
	entry wire.Entry    // the workflow (every op but triggers)
	rf    *run.RunFile  // retry, undo_write: the run (from disk)
	again *run.RunFile  // retry: what runs again
	undo  wire.UndoItem // undo_write: the write
}

// check reads what the call is about from disk (Validate and Describe
// run off the UI goroutine). The UI goroutine checks again in Run.
func (d aiWfxDisk) checkControl(a map[string]any) (aiWfxControlReq, error) {
	r := aiWfxControlReq{op: ai.ArgString(a, "op")}
	if _, ok := aiWfxControlOps[r.op]; !ok {
		return r, errors.New("give op: " + strings.Join(aiWfxControlOpList(), ", "))
	}
	if r.op == "triggers" {
		return r, nil
	}
	if r.op == "retry" || r.op == "undo_write" {
		id := strings.TrimSpace(ai.ArgString(a, "run"))
		if id == "" {
			return r, errors.New("give run: a run id (workflow_runs lists them)")
		}
		rf, err := wire.RunFiles{Root: d.root}.Load(id)
		if err != nil || rf == nil {
			return r, fmt.Errorf("no ended run %q (workflow_runs lists the runs)", aiCardText(id, 40))
		}
		if rf.Dry {
			return r, errors.New("run " + id + " is a dry run: it wrote nothing")
		}
		r.rf = rf
		e, err := d.find(rf.Workflow)
		if err != nil {
			return r, err
		}
		r.entry = e
		if r.op == "retry" {
			if e.File == nil {
				return r, errors.New(e.Name + " has no good file now; fix it first")
			}
			again, err := run.Retry(rf, e.File)
			if err != nil {
				return r, errors.New(e.Name + ": " + err.Error())
			}
			r.again = again
			return r, nil
		}
		u, err := aiWfxUndoItem(d.root, rf, ai.ArgString(a, "step"), ai.ArgString(a, "item"))
		r.undo = u
		return r, err
	}
	e, err := d.find(ai.ArgString(a, "name"))
	if err != nil {
		return r, err
	}
	r.entry = e
	if r.op == "on" || r.op == "off" {
		switch {
		case e.File == nil || e.File.Schedule == nil:
			return r, errors.New(e.Name + " has no schedule; it runs by hand (workflow_run)")
		case !e.Trusted:
			return r, errors.New(e.Name + " is not trusted yet: the user trusts it in the Workflows view first")
		case e.Enabled == (r.op == "on"):
			return r, errors.New(e.Name + " is " + r.op + " already")
		}
	}
	return r, nil
}

// aiWfxUndoItem is the write an undo is about: step/item when given,
// else the first one not undone yet (the view's u).
func aiWfxUndoItem(root string, rf *run.RunFile, step, item string) (wire.UndoItem, error) {
	blocked := ""
	for _, u := range wire.Undoable(root, rf) {
		if step != "" && (u.Step != step || u.Item != item) {
			continue
		}
		switch {
		case u.Undone && step != "":
			return u, errors.New("this write was undone already")
		case u.Undone:
			continue
		case u.Blocked != "":
			if step != "" {
				return u, errors.New("this write cannot be undone here: " + u.Blocked)
			}
			if blocked == "" {
				blocked = "cannot undo " + strings.TrimSpace(u.Step+" "+u.Item) + ": " + u.Blocked
			}
			continue
		}
		return u, nil
	}
	if blocked != "" {
		return wire.UndoItem{}, errors.New(blocked)
	}
	if step != "" {
		return wire.UndoItem{}, errors.New("no such write with an undo in run " + rf.ID)
	}
	return wire.UndoItem{}, errors.New("nothing to undo in run " + rf.ID)
}

// describe is the card text: the view's own card words.
func (r aiWfxControlReq) describe() string {
	name := r.entry.Name
	switch r.op {
	case "stop":
		return "Stop " + name + "?\nThe step that works now ends without its answer; finished items keep their results."
	case "on":
		return "Turn " + name + " on?\nIt then runs by itself on its schedule, also while you are away. Writes still ask unless allowed."
	case "off":
		return "Turn " + name + " off?\nIts schedule stops; you can still run it by hand."
	case "retry":
		return "Retry the run of " + name + "?\n" + wfxRetryBody(r.entry.File, r.again)
	case "undo_write":
		where := "Step " + r.undo.Step
		if r.undo.Item != "" {
			where = r.undo.Step + " · item " + r.undo.Item
		}
		return "Undo this write of run " + r.rf.ID + "?\n" + where + "\n" + r.undo.Target + "\n\nUndo: " + r.undo.Title +
			"\nIt runs once; what was written stays in the run's history."
	case "pause":
		return "Pause " + name + " before its next step"
	case "continue":
		return "Let " + name + " go on"
	}
	return "Read the workflow trigger events"
}

// aiWorkflowControlTool is workflow_control of workspace ws.
func aiWorkflowControlTool(ws string, call aiIDECaller) ai.Tool {
	disk := aiWfxDiskOf(ws)
	str := func(s string) map[string]any { return map[string]any{"type": "string", "description": s} }
	ui := func(ctx context.Context, fn func(m *Model) (string, tea.Cmd, error)) (string, error) {
		if call == nil {
			return "", errors.New("the Workflows view is not available")
		}
		return call(ctx, func(m *Model) (string, tea.Cmd, error) {
			m.wfxLoad(disk.ws)
			return fn(m)
		})
	}
	return ai.Tool{
		Name:    "workflow_control",
		Display: "control a workflow",
		Description: "Control workflows as the Workflows view does. op pause (before its next step), continue and triggers " +
			"(recent trigger events) run at once; stop (a live run), on / off (its schedule), retry (an ended run from its failed " +
			"part; every new write still asks on its own card) and undo_write (a write of an ended run) ask the user every time. " +
			"Give name for stop, pause, continue, on, off; run (an id from workflow_runs) for retry and undo_write.",
		Params: map[string]any{
			"op":   map[string]any{"type": "string", "enum": aiWfxControlOpList(), "description": "what to do"},
			"name": str("workflow name from workflow_list (stop, pause, continue, on, off)"),
			"run":  str("run id from workflow_runs (retry, undo_write)"),
			"step": str("undo_write: the step id of the write (default: the first write not undone)"),
			"item": str("undo_write: the item of the write, with step"),
		},
		Required: []string{"op"},
		Validate: func(a map[string]any) error { _, err := disk.checkControl(a); return err },
		Describe: func(a map[string]any) string {
			r, err := disk.checkControl(a)
			if err != nil {
				return "workflow_control (" + aiCardText(err.Error(), 80) + ")"
			}
			return r.describe()
		},
		Gate: func(a map[string]any) (bool, string) {
			ask, ok := aiWfxControlOps[ai.ArgString(a, "op")]
			return ask || !ok, ""
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			r, err := disk.checkControl(a)
			if err != nil {
				return "", err
			}
			name := r.entry.Name
			switch r.op {
			case "triggers":
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) { return m.wfxTrigText(), nil, nil })
			case "stop", "pause", "continue":
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					l := m.wfxRunning(name)
					if l == nil {
						return "", nil, errors.New(name + " is not running")
					}
					switch {
					case r.op == "stop":
						l.h.Cancel()
						return "Stopping " + name + "; finished items keep their results.", nil, nil
					case r.op == "pause" && l.h.Held():
						return "", nil, errors.New(name + " is paused already (op continue goes on)")
					case r.op == "pause":
						l.h.Pause()
						return name + " pauses before its next step (op continue goes on).", nil, nil
					case !l.h.Held():
						return "", nil, errors.New(name + " is not paused")
					}
					l.h.Continue()
					return name + " continues.", nil, nil
				})
			case "on", "off":
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					e, ok := wire.Find(m.wfxS().list, name)
					switch {
					case !ok:
						return "", nil, errors.New(name + " is gone")
					case e.Enabled == (r.op == "on"):
						return "", nil, errors.New(name + " is " + r.op + " already")
					}
					cmd := m.wfxToggle(e)
					e, _ = wire.Find(m.wfxS().list, name)
					if e.Enabled != (r.op == "on") {
						return "", cmd, errors.New(name + " could not be turned " + r.op)
					}
					out := name + " is " + r.op
					if next, ok := m.wfxNextRun(name); ok && e.Enabled {
						out += "; next run " + next.In(wfxLoc).Format("Mon Jan 2 15:04")
					}
					return out, cmd, nil
				})
			case "retry":
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					st := m.wfxS()
					rf, ok := m.wfxRunByID(r.rf.ID)
					if !ok {
						return "", nil, errors.New("that run is gone")
					}
					e, ok := wire.Find(st.list, rf.Workflow)
					switch {
					case !ok || e.File == nil:
						return "", nil, errors.New(rf.Workflow + " has no good file now; fix it first")
					case st.live[rf.ID] != nil || m.wfxRunning(e.Name) != nil:
						return "", nil, errors.New(e.Name + " is running; stop it or wait")
					case !workflowsEnabled():
						return "", nil, errors.New("workflows are turned off (workflows_enabled); only the user turns them on")
					}
					again, err := run.Retry(rf, e.File)
					if err != nil {
						return "", nil, errors.New(e.Name + ": " + err.Error())
					}
					_, _, cmd, err := m.wfxStartRunFrom(e, false, true, again)
					if err != nil {
						return "", nil, errors.New(e.Name + ": " + agentCtlShort(err.Error()))
					}
					st.stepTab = 0
					return "Retrying run " + rf.ID + " of " + e.Name + "; each new write asks the user on its card. workflow_runs with run=" + rf.ID + " shows how it goes.", cmd, nil
				})
			case "undo_write":
				var u wire.Undoer
				var rf *run.RunFile
				if _, err := ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					st := m.wfxS()
					var ok bool
					if rf, ok = m.wfxRunByID(r.rf.ID); !ok || st.root == "" {
						return "", nil, errors.New("that run is gone")
					}
					if st.live[rf.ID] != nil {
						return "", nil, errors.New("undo works once the run has ended")
					}
					u = wire.Undoer{Root: st.root, Workspace: st.ws, Exec: wfxUndoExec}
					if mc := newWFMCP(m.aiMCP(), m.aiMCPTrustAnnotations()); mc != nil {
						u.Tools = mc
					}
					return "", nil, nil
				}); err != nil {
					return "", err
				}
				uctx, cancel := context.WithTimeout(ctx, aiWfxControlUndoWait)
				defer cancel()
				if err := u.Undo(uctx, rf, r.undo.Step, r.undo.Item); err != nil {
					return "", errors.New("undo failed: " + agentCtlShort(err.Error()))
				}
				return "Undone: " + strings.TrimSpace(r.undo.Step+" "+r.undo.Item) + " (" + r.undo.Title + ")", nil
			}
			return "", errors.New("unknown op")
		},
	}
}
