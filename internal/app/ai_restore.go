package app

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/session"
)

// Restore points (Group R3). Before an agent turn first changes a file
// (write, create, delete, rename, an accepted review) the file's old text
// is kept. "AI: Undo Last Turn" or the "Undo AI changes" button under the
// turn in the chat puts every file back: changed files get their old text
// (an open tab shows it as one undo step), files the turn created are
// deleted. When a file changed again after the turn, a dialog asks first.
//
// Points are stored next to the hot-exit backups:
// <state>/termocode/ai-restore/<workspace key>/<chat id>.json, so the
// button survives a restart. Files over aiRestoreMaxBytes are not kept
// (the dialog says so).
//
// Shell commands (run_command, terminal_send) and LSP edits applied with
// ai_edit_review = auto change files the IDE does not see one by one, so
// they are not part of a restore point.

const (
	aiRestoreMaxBytes = 2 << 20
	aiRestoreMaxFiles = 300 // per turn (a deleted folder is walked)
	aiRestoreMaxTurns = 30  // per chat
)

// aiRestoreFile is one file's state before the turn and after it.
type aiRestoreFile struct {
	Path    string `json:"path"`
	Existed bool   `json:"existed"`
	Before  string `json:"before,omitempty"`
	TooBig  bool   `json:"too_big,omitempty"`
	// After is the file when the turn ended (AfterExists false: gone).
	AfterSet    bool   `json:"after_set,omitempty"`
	AfterExists bool   `json:"after_exists,omitempty"`
	After       string `json:"after,omitempty"`
}

// aiRestoreTurn is one turn's restore point. Msg is the index of the
// turn's user message in the chat.
type aiRestoreTurn struct {
	Msg   int             `json:"msg"`
	AtMs  int64           `json:"at_ms"`
	Files []aiRestoreFile `json:"files"`
}

// aiRestorePoints are the restore points of one chat.
type aiRestorePoints struct {
	Conv  string          `json:"conv"`
	Turns []aiRestoreTurn `json:"turns"`
}

// aiRestoreDir is where restore points live for the workspace.
func aiRestoreDir() (string, error) {
	root, err := hotExitRoot(aiWorkspaceRoot())
	if err != nil {
		return "", err
	}
	// <state>/termocode/backup/<key> → <state>/termocode/ai-restore/<key>
	return filepath.Join(filepath.Dir(filepath.Dir(root)), "ai-restore", session.Key(aiWorkspaceRoot())), nil
}

func aiRestorePath(conv string) (string, error) {
	d, err := aiRestoreDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, conv+".json"), nil
}

// aiRestoreLoad returns the chat's points (loaded once per chat).
func (s *aiState) aiRestoreLoad() *aiRestorePoints {
	if s.restore != nil && s.restore.Conv == s.conv.ID {
		return s.restore
	}
	p := &aiRestorePoints{Conv: s.conv.ID}
	if path, err := aiRestorePath(s.conv.ID); err == nil {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, p)
			p.Conv = s.conv.ID
		}
	}
	s.restore = p
	s.restoreGen++
	return p
}

func (s *aiState) aiRestoreSave() {
	p := s.restore
	if p == nil || p.Conv == "" {
		return
	}
	path, err := aiRestorePath(p.Conv)
	if err != nil {
		return
	}
	if len(p.Turns) == 0 {
		_ = os.Remove(path)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

// aiRestoreTurnFor returns the point of the turn starting at user message
// msg (nil when none).
func (p *aiRestorePoints) turn(msg int) *aiRestoreTurn {
	for i := range p.Turns {
		if p.Turns[i].Msg == msg {
			return &p.Turns[i]
		}
	}
	return nil
}

// aiRestoreNote keeps path's current state (and, for a folder, of the
// files in it) before the running agent turn first changes it. It runs
// on Update; outside an agent turn it does nothing.
func (m *Model) aiRestoreNote(path string) {
	s := m.aiS()
	if !s.streaming || s.turnMsg < 0 || s.conv.ID == "" {
		return
	}
	p := s.aiRestoreLoad()
	t := p.turn(s.turnMsg)
	if t == nil {
		p.Turns = append(p.Turns, aiRestoreTurn{Msg: s.turnMsg, AtMs: clock.Now().UnixMilli()})
		if len(p.Turns) > aiRestoreMaxTurns {
			p.Turns = p.Turns[len(p.Turns)-aiRestoreMaxTurns:]
		}
		t = &p.Turns[len(p.Turns)-1]
		s.restoreGen++
	}
	has := func(f string) bool {
		for _, x := range t.Files {
			if x.Path == f {
				return true
			}
		}
		return false
	}
	add := func(f string, fi fs.FileInfo) {
		if has(f) || len(t.Files) >= aiRestoreMaxFiles {
			return
		}
		rf := aiRestoreFile{Path: f, Existed: fi != nil}
		if fi != nil {
			if fi.Size() > aiRestoreMaxBytes {
				rf.TooBig = true
			} else if b, err := os.ReadFile(f); err == nil {
				rf.Before = string(b)
			} else {
				rf.TooBig = true
			}
		}
		t.Files = append(t.Files, rf)
		s.restoreGen++
	}
	fi, err := os.Lstat(path)
	switch {
	case err != nil:
		add(path, nil) // created by the turn
	case fi.IsDir():
		_ = filepath.WalkDir(path, func(f string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if i, err := d.Info(); err == nil && i.Mode().IsRegular() {
				add(f, i)
			}
			return nil
		})
	case fi.Mode().IsRegular():
		add(path, fi)
	}
}

// aiRestoreTurnDone records how the turn left its files and saves.
func (m *Model) aiRestoreTurnDone() {
	s := m.aiS()
	if s.restore == nil || s.restore.Conv != s.conv.ID {
		return
	}
	t := s.restore.turn(s.turnMsg)
	if t == nil {
		return
	}
	for i := range t.Files {
		f := &t.Files[i]
		f.AfterSet = true
		f.AfterExists, f.After = false, ""
		if fi, err := os.Stat(f.Path); err == nil && fi.Mode().IsRegular() && fi.Size() <= aiRestoreMaxBytes {
			if b, err := os.ReadFile(f.Path); err == nil {
				f.AfterExists, f.After = true, string(b)
			}
		}
	}
	s.aiRestoreSave()
}

// aiLastRestoreTurn is the newest turn with a restore point (-1: none).
func (m *Model) aiLastRestoreTurn() int {
	p := m.aiS().aiRestoreLoad()
	if len(p.Turns) == 0 {
		return -1
	}
	return p.Turns[len(p.Turns)-1].Msg
}

// aiRestoreChanged sorts the files of t that need a word before the
// undo: changed after the turn (restoring loses that), open with unsaved
// edits (left as they are: aiWriteFile never writes under them), and too
// big to restore. A file that already has its old text needs nothing.
func (m *Model) aiRestoreChanged(t *aiRestoreTurn) (changed, unsaved, tooBig []string) {
	defer m.aiBeginBatch()() // one buffer list for all files; it changes nothing
	rel := aiRel(aiWorkspaceRoot())
	for _, f := range t.Files {
		if f.TooBig {
			tooBig = append(tooBig, rel(f.Path))
			continue
		}
		b, err := os.ReadFile(f.Path)
		exists := err == nil
		if exists == f.Existed && (!exists || string(b) == f.Before) {
			continue // nothing to restore
		}
		if bufs, _ := m.aiBuffersUnder(f.Path); len(aiUnsaved(bufs, rel)) > 0 {
			unsaved = append(unsaved, rel(f.Path))
			continue
		}
		if f.AfterSet && (exists != f.AfterExists || (exists && string(b) != f.After)) {
			changed = append(changed, rel(f.Path))
		}
	}
	return changed, unsaved, tooBig
}

// aiUndoTurn starts "undo the AI changes of turn msg" (-1: the last one).
func (m *Model) aiUndoTurn(msg int) tea.Cmd {
	s := m.aiS()
	if s.streaming {
		return m.aiInfo("The AI is still working — stop it first")
	}
	if msg < 0 {
		msg = m.aiLastRestoreTurn()
	}
	t := s.aiRestoreLoad().turn(msg)
	if t == nil {
		return m.aiInfo("No AI changes to undo in this chat")
	}
	changed, unsaved, tooBig := m.aiRestoreChanged(t)
	if len(changed) == 0 && len(unsaved) == 0 && len(tooBig) == 0 {
		return m.aiDoUndoTurn(msg)
	}
	var body strings.Builder
	line := func(format string, names []string) {
		if len(names) == 0 {
			return
		}
		if body.Len() > 0 {
			body.WriteString("\n")
		}
		fmt.Fprintf(&body, format, aiListNames(names))
	}
	line("Changed since the AI turn: %s.\nRestoring puts back the text from before the turn; these later changes are lost (an open tab can undo it with u).", changed)
	line("Open with unsaved edits, left as they are: %s. Save or discard the edits first to undo the AI changes there.", unsaved)
	line("Too big to restore, left as they are: %s.", tooBig)
	s.undoTurn = msg
	// Restore can overwrite later work: Cancel comes first and has the
	// focus, so Enter keeps everything (AH-1).
	m.confirm = confirm.Risky("↶ Undo AI changes?", body.String(),
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "restore", Title: "Restore", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIUndoTurn
	return nil
}

func aiListNames(xs []string) string {
	if len(xs) > 5 {
		return strings.Join(xs[:5], ", ") + fmt.Sprintf(" and %d more", len(xs)-5)
	}
	return strings.Join(xs, ", ")
}

// aiDoUndoTurn restores the files of turn msg.
func (m *Model) aiDoUndoTurn(msg int) tea.Cmd {
	s := m.aiS()
	p := s.aiRestoreLoad()
	t := p.turn(msg)
	if t == nil {
		return nil
	}
	rel := aiRel(aiWorkspaceRoot())
	var restored, skipped []string
	var cmds []tea.Cmd
	end := m.aiBeginBatch() // the IDE is refreshed once, after all files
	// Newest first: a file created, then written, is deleted once.
	for i := len(t.Files) - 1; i >= 0; i-- {
		f := t.Files[i]
		if f.TooBig {
			skipped = append(skipped, rel(f.Path))
			continue
		}
		var cmd tea.Cmd
		var err error
		if f.Existed {
			cur, rerr := os.ReadFile(f.Path)
			if rerr == nil && string(cur) == f.Before {
				continue
			}
			_, cmd, err = m.aiWriteFile(f.Path, rel(f.Path), f.Before)
		} else if _, serr := os.Lstat(f.Path); serr == nil {
			_, cmd, err = m.aiDeletePath(f.Path, rel)
		} else {
			continue
		}
		if err != nil {
			skipped = append(skipped, rel(f.Path))
			continue
		}
		restored = append(restored, rel(f.Path))
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, end())
	// Drop the point (and newer ones: they were made on top of it).
	for i := range p.Turns {
		if p.Turns[i].Msg == msg {
			p.Turns = p.Turns[:i]
			break
		}
	}
	s.restoreGen++
	s.aiRestoreSave()
	line := "⚙ Undo AI changes of a turn"
	if len(restored) > 0 {
		line += " → restored " + aiListNames(restored)
	} else {
		line += " → nothing to restore"
	}
	if len(skipped) > 0 {
		line += "; not restored: " + aiListNames(skipped)
	}
	s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredTool, Text: line})
	m.aiAudit(clock.Now(), "undo_turn", fmt.Sprintf("turn=%d", msg), "user", "user", strings.TrimPrefix(line, "⚙ "))
	if err := s.store.Save(&s.conv); err != nil {
		recordError("[ai] save chat: " + err.Error())
	}
	msgText := fmt.Sprintf("Restored %d file%s from before the AI turn", len(restored), plural(len(restored)))
	if len(skipped) > 0 {
		msgText += fmt.Sprintf(" (%d not restored)", len(skipped))
	}
	return tea.Batch(append(cmds, m.aiInfo(msgText))...)
}

// onAIUndoTurnConfirm answers the "Undo AI changes?" dialog.
func (m *Model) onAIUndoTurnConfirm(id string) tea.Cmd {
	s := m.aiS()
	msg := s.undoTurn
	s.undoTurn = -1
	if id != "restore" || msg < 0 {
		return nil
	}
	return m.aiDoUndoTurn(msg)
}

// aiRestoreFiles is the number of files in the restore point of the turn
// starting at user message msg (0: none).
func (m Model) aiRestoreFiles(msg int) int {
	s := m.ai
	if s == nil || s.conv.ID == "" {
		return 0
	}
	if t := s.aiRestoreLoad().turn(msg); t != nil {
		return len(t.Files)
	}
	return 0
}
