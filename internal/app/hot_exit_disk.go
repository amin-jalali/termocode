package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/toast"
)

// SCN-9: a backup restored after a crash may be older than the file on
// disk: someone changed it, deleted it, put a folder in its place or
// switched the git branch while termocode was down. The restored text
// still comes back (it is the user's work), but the user is told, so a
// save does not overwrite the new disk text without a word.

// hotDiskState is what happened to a backed-up file on disk.
type hotDiskState int

const (
	hotDiskSame    hotDiskState = iota
	hotDiskChanged              // written after the backup
	hotDiskDeleted              // gone
	hotDiskFolder               // a folder now has its name
)

// hotDiskStateOf compares the file of b with the time of its backup.
func hotDiskStateOf(b hotBackup) hotDiskState {
	fi, err := os.Stat(b.Path)
	switch {
	case err != nil:
		return hotDiskDeleted
	case fi.IsDir():
		return hotDiskFolder
	case !b.Saved.IsZero() && fi.ModTime().After(b.Saved):
		return hotDiskChanged
	}
	return hotDiskSame
}

// hotRestoredName is a free file name next to a folder that took the
// place of a backed-up file: "a.txt" → "a.txt.restored" (then
// "a.txt.restored-2", …). The text goes to a new buffer of that name.
func hotRestoredName(p string) string {
	for i := 1; ; i++ {
		c := p + ".restored"
		if i > 1 {
			c = fmt.Sprintf("%s.restored-%d", p, i)
		}
		if _, err := os.Lstat(c); err != nil {
			return c
		}
	}
}

// hotExitDiskCheck looks at the disk before a crash restore: a backup
// whose file is now a folder is moved to a new name (its text must not
// go into a folder buffer). It returns the backups to restore and what
// to tell the user afterwards.
func hotExitDiskCheck(b []hotBackup) ([]hotBackup, hotDiskReport) {
	var r hotDiskReport
	out := make([]hotBackup, len(b))
	for i, x := range b {
		out[i] = x
		switch hotDiskStateOf(x) {
		case hotDiskChanged:
			r.changed = append(r.changed, x)
		case hotDiskDeleted:
			r.deleted = append(r.deleted, x.Path)
		case hotDiskFolder:
			out[i].Path = hotRestoredName(x.Path)
			r.folder = append(r.folder, [2]string{x.Path, out[i].Path})
		}
	}
	return out, r
}

// hotDiskReport lists the restored files whose disk changed.
type hotDiskReport struct {
	changed []hotBackup
	deleted []string
	folder  [][2]string // old path, new buffer name
}

// tell shows the report: a card for changed files (Close first, then
// Compare), a warning toast for deleted ones and folders.
func (m *Model) tellHotDisk(r hotDiskReport) tea.Cmd {
	var cmds []tea.Cmd
	var warn []string
	for _, p := range r.deleted {
		warn = append(warn, filepath.Base(p)+" was deleted on disk. Save to create it again.")
	}
	for _, f := range r.folder {
		warn = append(warn, fmt.Sprintf("%s is now a folder. Your text is in %s.", filepath.Base(f[0]), filepath.Base(f[1])))
	}
	if len(warn) > 0 {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Warn, "Restored text: file changed on disk", strings.Join(warn, "\n"))
		cmds = append(cmds, cmd)
	}
	if len(r.changed) > 0 {
		m.promptHotDiskChanged(r.changed)
	}
	return tea.Batch(cmds...)
}

// promptHotDiskChanged asks nothing risky: Close (first, focused) keeps
// the restored text in the editor and the disk as it is; Compare shows
// the disk text next to the restored text.
func (m *Model) promptHotDiskChanged(b []hotBackup) {
	m.hotDiskChanged = b
	names := make([]string, 0, len(b))
	for _, x := range b {
		names = append(names, filepath.Base(x.Path))
	}
	title := names[0] + " changed on disk"
	if len(names) > 1 {
		title = fmt.Sprintf("%d files changed on disk", len(names))
	}
	body := "It changed after your unsaved edits were backed up. Your text is restored in the editor; saving replaces the disk version. Compare them first."
	m.confirm = confirm.New(title, body, []confirm.Button{
		{ID: "close", Title: "Close"},
		{ID: "compare", Title: "Compare", Style: confirm.StylePrimary},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindHotExitDisk
}

// onHotDiskConfirm handles Close / Compare.
func (m *Model) onHotDiskConfirm(id string) tea.Cmd {
	b := m.hotDiskChanged
	m.hotDiskChanged = nil
	if id != "compare" || len(b) == 0 {
		return nil
	}
	files := make([]diffview.File, 0, len(b))
	rel := aiRel(aiWorkspaceRoot())
	for _, x := range b {
		disk, err := os.ReadFile(x.Path)
		if err != nil || len(disk) > maxCompareBytes {
			continue
		}
		files = append(files, diffview.File{
			Name: rel(x.Path) + " (disk ↔ your text)",
			Old:  string(disk),
			New:  strings.Join(x.Lines, "\n") + "\n",
		})
	}
	if len(files) == 0 {
		return nil
	}
	s := m.aiS()
	if s.review != nil && m.aiReviewPending() > 0 {
		return m.aiInfo("Finish the AI review first (AI: Review Pending Changes)")
	}
	s.review = &aiReview{card: diffview.New("Disk changed", files, false)}
	return m.showAIReview()
}
