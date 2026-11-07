package app

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/cfgfile"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// Workflow ops from the phone (MOB-60, mobile plan §3.9, §2.6).
//
//   - workflows.dry_run starts a dry run at once and answers with its run
//     id: it reads the sources read-only, runs plan steps (AI money; the
//     page says so before it sends) and never writes.
//   - workflows.run is a laptop card (Cancel first): a real run may write
//     to outside systems; each write still asks on its own gate card.
//   - workflows.save is a laptop card: it writes
//     .termocode/workflows/<name>.jsonc and trusts these exact bytes, so
//     the laptop reads the file (or the diff) first. Nothing runs.
//
// A repo file nobody trusted yet, a broken file, workflows turned off and
// a workflow that runs already are refused before any card. Trust itself
// stays on the laptop, where the user reads the file.

func init() {
	webOps[webproto.OpWorkflowsDryRun] = (*Model).webWorkflowsDryRun
	webAskOps[webproto.OpWorkflowsRun] = (*Model).webWorkflowsRun
	webAskOps[webproto.OpWorkflowsSave] = (*Model).webWorkflowsSave
}

// webWorkflowsWS is the workspace whose workflows the phone sees.
func (m *Model) webWorkflowsWS() string {
	if m.wfx != nil && m.wfx.ws != "" {
		return m.wfx.ws
	}
	ws, _ := m.rootDir()
	return ws
}

// webWorkflowReady finds a workflow that can start now; false: it
// answered the call with the reason.
func (m *Model) webWorkflowReady(call *webCall, name string) (wire.Entry, bool) {
	ws := m.webWorkflowsWS()
	if ws == "" {
		call.fail(webproto.CodeNotFound, "No workspace is open on the laptop.")
		return wire.Entry{}, false
	}
	m.wfxLoad(ws)
	e, ok := wire.Find(m.wfxS().list, name)
	switch {
	case !ok:
		call.fail(webproto.CodeNotFound, "There is no such workflow.")
	case e.Broken():
		call.fail(webproto.CodeConflict, name+" has problems in its file; fix them on the laptop first.")
	case !e.Trusted:
		call.fail(webproto.CodeForbidden, "Trust this workflow file on the laptop first (you read it there).")
	case !workflowsEnabled():
		call.fail(webproto.CodeForbidden, "Workflows are turned off on the laptop.")
	case m.wfxRunning(e.Name) != nil:
		call.fail(webproto.CodeConflict, name+" is running already.")
	default:
		return e, true
	}
	return wire.Entry{}, false
}

// webWorkflowAllowed: the admin policy lets the file run for real.
func webWorkflowAllowed(e wire.Entry) bool {
	return e.File == nil || wfxPolicy().Blocked(e.File) == ""
}

// webWorkflowsDryRun starts a dry run (workflows.dry_run).
func (m *Model) webWorkflowsDryRun(call *webCall) tea.Cmd {
	a := call.args.(*webproto.WorkflowNameArgs)
	e, ok := m.webWorkflowReady(call, a.Name)
	if !ok {
		return nil
	}
	id, _, cmd, err := m.wfxStartRun(e, true, false)
	if err != nil {
		call.fail(webproto.CodeConflict, "Could not start the dry run: "+agentCtlShort(err.Error()))
		return nil
	}
	call.reply(webproto.WorkflowRunArgs{Run: id})
	return tea.Batch(cmd, m.aiInfo(e.Name+": dry run from "+webCallDevice(call)))
}

// webWorkflowsRun asks the laptop to run a workflow for real
// (workflows.run).
func (m *Model) webWorkflowsRun(call *webCall) *webAsk {
	a := call.args.(*webproto.WorkflowNameArgs)
	e, ok := m.webWorkflowReady(call, a.Name)
	if !ok {
		return nil
	}
	if !webWorkflowAllowed(e) {
		call.fail(webproto.CodeForbidden, e.Name+" is blocked by the workflow policy; a dry run still works.")
		return nil
	}
	disk := aiWfxDiskOf(m.wfxS().ws)
	name, hash := e.Name, e.Hash
	return &webAsk{
		summary: name,
		title:   "Run " + name + " now?",
		body:    disk.describeRun(e),
		action:  "Run",
		run: func(m *Model) tea.Cmd {
			m.wfxLoad("")
			cur, ok := wire.Find(m.wfxS().list, name)
			switch {
			case !ok || cur.Hash != hash:
				return m.aiInfo(name + " changed since the card; nothing ran")
			case !cur.Trusted || cur.Broken() || !workflowsEnabled():
				return m.aiInfo(name + " cannot run now; nothing ran")
			case m.wfxRunning(name) != nil:
				return m.aiInfo(name + " is running already")
			}
			_, _, cmd, err := m.wfxStartRun(cur, false, true)
			if err != nil {
				return m.aiInfo(name + ": " + agentCtlShort(err.Error()))
			}
			return cmd
		},
	}
}

// webWorkflowsSave asks the laptop to save a workflow file
// (workflows.save). The text is checked like workflow_draft checks it: a
// file with a problem or a key in it never reaches the card.
func (m *Model) webWorkflowsSave(call *webCall) *webAsk {
	a := call.args.(*webproto.WorkflowSaveArgs)
	ws := m.webWorkflowsWS()
	if ws == "" {
		call.fail(webproto.CodeNotFound, "No workspace is open on the laptop.")
		return nil
	}
	disk := aiWfxDiskOf(ws)
	dr, err := disk.checkDraft(map[string]any{"name": a.Name, "content": a.Content})
	if err != nil {
		call.fail(webproto.CodeBadRequest, aiCardText(err.Error(), 300))
		return nil
	}
	if m.wfxRunning(dr.name) != nil {
		call.fail(webproto.CodeConflict, dr.name+" is running; save it after the run.")
		return nil
	}
	was := workflow.HashBytes(dr.old)
	title := "Create workflow " + dr.name + "?"
	action := "Create"
	if dr.exists {
		title, action = "Change workflow "+dr.name+"?", "Save"
	}
	return &webAsk{
		summary: dr.rel(),
		title:   title,
		body:    disk.describeDraft(dr),
		action:  action,
		run: func(m *Model) tea.Cmd {
			if err := webWorkflowWrite(disk, dr, was); err != nil {
				return m.aiInfo(dr.name + ": " + agentCtlShort(err.Error()))
			}
			m.wfxLoad(disk.ws)
			m.wfxSyncActivity()
			return tea.Batch(m.aiInfo("Saved "+dr.rel()+" from the phone; nothing ran"), m.wfxSendProblems())
		},
	}
}

// errWorkflowChanged: the file on disk changed while the card was open.
var errWorkflowChanged = errors.New("the file changed while the card was open; nothing was saved")

// webWorkflowWrite writes a checked draft when the file on disk still has
// the hash the card showed, then trusts the new bytes.
func webWorkflowWrite(disk aiWfxDisk, dr aiWfxDraft, was string) error {
	now, err := disk.checkDraft(map[string]any{"name": dr.name, "content": string(dr.data)})
	if err != nil {
		return err
	}
	if workflow.HashBytes(now.old) != was {
		return errWorkflowChanged
	}
	if err := cfgfile.WriteFile(dr.path, dr.data); err != nil {
		return err
	}
	if disk.root == "" {
		return nil
	}
	return wire.Trust(disk.root, wire.Entry{Name: dr.name, Hash: workflow.HashBytes(dr.data)})
}

// webCallDevice names the asking device for the laptop's info line.
func webCallDevice(call *webCall) string {
	if d := webName(call.conn.device); d != "" {
		return d
	}
	return "web"
}
