package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentctl"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/checkpoint"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
)

// CMP-62: code checkpoints for every agent, also outside CLI agents in a
// terminal tab (internal/checkpoint keeps them as private git refs and
// never touches the index, branch or stash).
//
//   - An outside agent's turn: when its hook reports "working" after
//     another state (agent_ctl.go), the tab's folder (its agent
//     worktree, else its shell folder) gets a checkpoint "before <agent>
//     turn" first. The hook's reply waits for it, so the agent's first
//     edit comes after the save.
//   - AI: Save Code Checkpoint saves the workspace by hand.
//   - AI: Restore Code Checkpoint… lists the checkpoints of the
//     workspace and of every agent tab's folder; picking one shows what
//     a restore changes on a card with Cancel first. The restore saves
//     the files as they are now first, so it can be undone the same way.

const (
	pickerKindCheckpoint         pickerKindEnum  = 6263
	confirmKindCheckpointRestore confirmKindEnum = 6263
)

// checkpointShown is how many checkpoints the picker lists per folder.
const checkpointShown = 30

// agentTurnStarts: an outside agent's hook report starts a new turn.
func agentTurnStarts(was, now agents.State) bool {
	return now == agents.Working && was != agents.Working
}

// checkpointDirFor is the folder a tab's agent works in.
func checkpointDirFor(t *terminalTab) string {
	switch {
	case t == nil:
		return aiWorkspaceRoot()
	case t.Worktree != "":
		return t.Worktree
	case t.Cwd != "":
		return t.Cwd
	}
	return aiWorkspaceRoot()
}

// checkpointAgentTurn saves a checkpoint of the tab's folder, then sends
// the hook's reply (the agent waits for it, up to its short timeout).
func (m *Model) checkpointAgentTurn(call agentCtlCall, rep agentctl.Reply, t *terminalTab) tea.Cmd {
	dir := checkpointDirFor(t)
	label := "before " + t.agentTitle() + " turn"
	return func() tea.Msg {
		_, err := checkpoint.Save(dir, label)
		call.reply <- rep
		if err != nil && !errors.Is(err, checkpoint.ErrNotRepo) {
			recordError("[checkpoint] " + label + ": " + err.Error())
		}
		return nil
	}
}

// checkpointSaveNow saves the workspace as a checkpoint (off Update).
func (m *Model) checkpointSaveNow() tea.Cmd {
	dir := aiWorkspaceRoot()
	return func() tea.Msg {
		p, err := checkpoint.Save(dir, "saved by hand")
		return applyMsg(func(m *Model) tea.Cmd {
			if err != nil {
				return m.aiToastErr("Could not save a code checkpoint", err)
			}
			return m.aiInfo("Code checkpoint saved at " + p.At.Format("15:04:05") + " — AI: Restore Code Checkpoint… puts it back")
		})
	}
}

// checkpointDirs are the folders the picker lists: the workspace first,
// then each agent tab's folder (one per working tree).
func (m *Model) checkpointDirs() []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		top, err := checkpoint.Top(d)
		if err != nil || seen[top] {
			return
		}
		seen[top] = true
		dirs = append(dirs, top)
	}
	add(aiWorkspaceRoot())
	for i := range m.terminalTabs {
		if t := &m.terminalTabs[i]; t.Agent != "" {
			add(checkpointDirFor(t))
		}
	}
	return dirs
}

// openCheckpointPicker lists the checkpoints, newest first per folder.
func (m *Model) openCheckpointPicker() tea.Cmd {
	s := m.aiS()
	dirs := m.checkpointDirs()
	if len(dirs) == 0 {
		return m.aiInfo("Code checkpoints need a git repository")
	}
	root := aiWorkspaceRoot()
	var items []picker.Item
	for k, d := range dirs {
		pts, err := checkpoint.List(d)
		if err != nil {
			recordError("[checkpoint] list " + d + ": " + err.Error())
			continue
		}
		where := "workspace"
		if k > 0 {
			where = filepath.Base(d)
			if r, err := filepath.Rel(root, d); err == nil && !strings.HasPrefix(r, "..") {
				where = filepath.ToSlash(r)
			}
		}
		for n, p := range pts {
			if n >= checkpointShown {
				break
			}
			items = append(items, picker.Item{
				ID:    strconv.Itoa(k) + ":" + p.ID,
				Title: p.Label,
				Hint:  where + " · " + p.At.Format("Jan 2 15:04:05"),
			})
		}
	}
	if len(items) == 0 {
		return m.aiInfo("No code checkpoint yet: AI: Save Code Checkpoint saves one; outside agents get one at each turn")
	}
	s.cpDirs = dirs
	m.picker = picker.NewItems(" Restore Code Checkpoint ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindCheckpoint
	return nil
}

// onCheckpointPicked shows what a restore of the picked checkpoint
// changes, on a card with Cancel first.
func (m *Model) onCheckpointPicked(id string) tea.Cmd {
	s := m.aiS()
	ks, pid, ok := strings.Cut(id, ":")
	k, err := strconv.Atoi(ks)
	if !ok || err != nil || k < 0 || k >= len(s.cpDirs) {
		return nil
	}
	dir := s.cpDirs[k]
	p, err := checkpoint.Find(dir, pid)
	if err != nil {
		return m.aiToastErr("Could not read the checkpoint", err)
	}
	ch, err := checkpoint.Changes(dir, p)
	if err != nil {
		return m.aiToastErr("Could not compare the checkpoint", err)
	}
	if len(ch) == 0 {
		return m.aiInfo("Nothing to restore: the files match this checkpoint")
	}
	// Never write under an open tab's unsaved edits.
	rel := aiRel(dir)
	bufs, _ := m.aiBuffersUnder(dir)
	dirty := map[string]bool{}
	for _, d := range aiUnsaved(bufs, rel) {
		dirty[d] = true
	}
	var blocked, names []string
	for _, c := range ch {
		if dirty[c.Path] {
			blocked = append(blocked, c.Path)
		}
		names = append(names, c.Path)
	}
	if len(blocked) > 0 {
		return m.aiInfo("Save or discard the unsaved edits first: " + aiListNames(blocked))
	}
	body := fmt.Sprintf("%s (%s)\n%s: %s.\nYour files as they are now are saved as a checkpoint first, so this can be undone here too. Ignored files, the git index and the branch stay as they are.",
		p.Label, p.At.Format("Jan 2 15:04:05"), checkpoint.Summary(ch), aiListNames(names))
	s.cpPending, s.cpDir = &p, dir
	m.confirm = confirm.Risky("↶ Restore code checkpoint?", body,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "restore", Title: "Restore", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindCheckpointRestore
	return nil
}

// answerCheckpointRestore answers the restore card (the restore runs off
// Update; the IDE reloads the files after it).
func (m *Model) answerCheckpointRestore(id string) tea.Cmd {
	s := m.aiS()
	p, dir := s.cpPending, s.cpDir
	s.cpPending, s.cpDir = nil, ""
	if id != "restore" || p == nil {
		return nil
	}
	pt := *p
	return func() tea.Msg {
		res, err := checkpoint.Restore(dir, pt)
		return applyMsg(func(m *Model) tea.Cmd {
			if err != nil {
				return m.aiToastErr("Could not restore the checkpoint", err)
			}
			_, sync := m.aiSyncIDE("", aiSyncDisk)
			text := fmt.Sprintf("Restored %q: %d file%s back, %d new file%s deleted", pt.Label,
				len(res.Restored), plural(len(res.Restored)), len(res.Removed), plural(len(res.Removed)))
			if len(res.Failed) > 0 {
				text += "; not deleted: " + aiListNames(res.Failed)
			}
			m.aiAudit(clock.Now(), "checkpoint_restore", "checkpoint="+pt.ID, "user", "user", text)
			return tea.Batch(sync, m.aiInfo(text+" (undo: AI: Restore Code Checkpoint…)"))
		})
	}
}
