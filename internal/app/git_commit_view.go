package app

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The commit view (scm-graph design §3): a GRAPH commit opens in the diff
// card (internal/diffview) in commit mode. When the editor is at least
// commitDockMinW cells wide the card docks over it and the sidebar stays
// live (moving the GRAPH cursor shows that commit); otherwise it floats
// over the whole screen. File texts load one by one when selected.

const (
	commitDockMinW  = 80
	commitBlobCache = 32               // file pairs kept (sha:path)
	commitMaxBytes  = 2 << 20          // a side over this is not diffed
	commitMaxLines  = 20000            // nor over this many lines
	commitTooBig    = "Too big to diff here · r raw"
)

// commitViewState is the open (or opening) commit view.
type commitViewState struct {
	want    string // the newest commit asked for; older answers are dropped
	open    bool
	info    git.CommitInfo
	card    diffview.Model
	focused bool // keys go to a docked card (a floating one always has them)
	blobs   map[string][2]string
	order   []string // blobs, oldest first
}

// commitViewReadyMsg carries a commit's meta and files.
type commitViewReadyMsg struct {
	want string
	info git.CommitInfo
	err  error
}

// commitFileTextMsg carries one file's two sides (or a note).
type commitFileTextMsg struct {
	sha      string
	file     int
	path     string
	old, new string
	note     string
}

// commitViewShown reports whether the commit card is on screen.
func (m Model) commitViewShown() bool { return m.commitView != nil && m.commitView.open }

// gitOpenCommitViewCmd opens (or moves) the commit view to hash. The git
// call runs off the loop; the newest request wins.
func (m *Model) gitOpenCommitViewCmd(hash string) tea.Cmd {
	if hash == "" {
		return nil
	}
	if m.commitView == nil {
		m.commitView = &commitViewState{blobs: map[string][2]string{}}
	}
	cv := m.commitView
	if cv.open && (cv.info.FullHash == hash || cv.info.Hash == hash) {
		cv.want = hash
		return nil
	}
	cv.want = hash
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		info, err := git.CommitFiles(cwd, hash)
		return commitViewReadyMsg{want: hash, info: info, err: err}
	}
}

// onCommitViewReady builds the card for a loaded commit.
func (m *Model) onCommitViewReady(msg commitViewReadyMsg) tea.Cmd {
	cv := m.commitView
	if cv == nil || msg.want != cv.want {
		return nil
	}
	if msg.err != nil {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Errr, "Could not read the commit", msg.err.Error())
		return cmd
	}
	info := msg.info
	wasOpen := cv.open
	cv.info = info
	cv.card = diffview.NewCommit(commitHeader(info), commitFiles(info), commitButtons())
	cv.open = true
	if !wasOpen {
		cv.focused = false
	}
	return m.commitLoadFile(0)
}

// commitHeader maps a git commit to the card header.
func commitHeader(info git.CommitInfo) diffview.Header {
	h := diffview.Header{
		Hash: info.Hash, FullHash: info.FullHash, Subject: info.Subject, Body: info.Body,
		Author: info.Author, DateRel: info.DateRel, DateAbs: info.DateAbs, Parents: info.Parents,
		Files: info.CommitDetail.Files, Add: info.Add, Del: info.Del,
		NewLabel: "+ " + info.Hash + " (this commit)",
	}
	for _, r := range info.Refs {
		h.Refs = append(h.Refs, diffview.Ref{Name: r.Name, Kind: diffview.RefKind(r.Kind)})
	}
	switch len(info.Parents) {
	case 0:
		h.OldLabel = "− (no parent)"
	default:
		h.OldLabel = "− " + info.Parents[0] + " (parent)"
	}
	return h
}

func commitFiles(info git.CommitInfo) []diffview.File {
	out := make([]diffview.File, 0, len(info.Files))
	for _, f := range info.Files {
		df := diffview.File{Name: f.Path, Status: f.Status, Add: f.Add, Del: f.Del, Binary: f.Binary, Pending: !f.Binary}
		if f.Status == 'R' && f.OldPath != "" {
			df.Tag = "from " + f.OldPath
		}
		out = append(out, df)
	}
	return out
}

func commitButtons() []diffview.Button {
	return []diffview.Button{
		{ID: "copy-sha", Label: "Copy SHA"},
		{ID: "open-file", Label: "Open file"},
		{ID: "open-at-commit", Label: "File @ commit"},
		{ID: "revert", Label: "Revert…"},
		{ID: "cherry-pick", Label: "Cherry-pick…"},
		{ID: "hide", Label: "Close"},
	}
}

// closeCommitView hides the card.
func (m *Model) closeCommitView() {
	if m.commitView != nil {
		m.commitView.open = false
		m.commitView.focused = false
		m.commitView.want = ""
	}
}

// commitDocked reports whether the card docks over the editor (and the
// dock rectangle).
func (m Model) commitDocked() (x, y, w, h int, ok bool) {
	if m.zenMode || !m.showExp || m.activity.Active() != activity.ViewGit {
		return 0, 0, 0, 0, false
	}
	pane := m.editorPaneLayout()
	if pane.W < commitDockMinW {
		return 0, 0, 0, 0, false
	}
	return pane.X, 0, m.w - pane.X - m.actionsColumnWidth(), max(m.h-1, 1), true
}

// commitIsDocked reports whether the card docks over the editor.
func (m Model) commitIsDocked() bool {
	_, _, _, _, ok := m.commitDocked()
	return ok
}

// commitCard is the card placed for the current screen.
func (m Model) commitCard() diffview.Model {
	c := m.commitView.card
	c.SetSize(m.w, m.h)
	if x, y, w, h, ok := m.commitDocked(); ok {
		c.SetBounds(x, y, w, h)
	} else {
		c.SetBounds(0, 0, 0, 0)
	}
	return c
}

// commitViewLayer draws the card over base: docked without a backdrop,
// floating like the review card.
func (m Model) commitViewLayer(base string) string {
	if !m.commitViewShown() {
		return base
	}
	c := m.commitCard()
	box := c.Box()
	if box == "" {
		return base
	}
	o := ModalOpts{At: true}
	o.X, o.Y = c.Origin()
	if _, _, _, _, ok := m.commitDocked(); ok {
		o.NoDim, o.NoShadow = true, true
	}
	return modalOverlay(base, box, m.w, m.h, &o)
}

// routeToCommitView gives the card its input: every key and click while
// it floats; while it is docked, clicks on it and keys once it has the
// focus (a click on it gives it the focus, a click elsewhere takes it
// away). ok is false when the message goes on to the normal path.
func (m Model) routeToCommitView(msg tea.Msg) (Model, tea.Cmd, bool) {
	cv := m.commitView
	_, _, _, _, docked := m.commitDocked()
	switch msg := msg.(type) {
	case tea.MouseMsg:
		c := m.commitCard()
		x, y := c.Origin()
		box := c.Box()
		w, h := textWidth(strings.SplitN(box, "\n", 2)[0]), strings.Count(box, "\n")+1
		inside := msg.X >= x && msg.X < x+w && msg.Y >= y && msg.Y < y+h
		if docked && !inside {
			if msg.Action == tea.MouseActionPress {
				cv.focused = false
			}
			return m, nil, false
		}
		if m.mouseProbe != nil {
			if id := c.ButtonAt(msg.X, msg.Y); id != "" {
				m.probed("commit:" + id)
			} else {
				m.probed("commit")
			}
			return m, nil, true
		}
		if msg.Action == tea.MouseActionPress {
			cv.focused = true
		}
		var cmd tea.Cmd
		c, cmd, _ = c.HandleMouse(msg.X, msg.Y, msg.Action, msg.Button)
		cv.card = c
		return m, m.runCommitCardCmd(cmd), true
	case tea.KeyMsg:
		if docked && !cv.focused {
			return m, nil, false
		}
		c := m.commitCard()
		var cmd tea.Cmd
		c, cmd = c.Update(msg)
		cv.card = c
		return m, m.runCommitCardCmd(cmd), true
	}
	return m, nil, false
}

// runCommitCardCmd runs what the card asked for. The card's Cmds are
// pure (they only build a message), so they run here and never reach the
// shared message path (where HideMsg means the AI review card).
func (m *Model) runCommitCardCmd(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case diffview.HideMsg:
		m.closeCommitView()
	case diffview.LoadFileMsg:
		return m.commitLoadFile(msg.File)
	case diffview.ActionMsg:
		return m.commitAction(msg.ID, msg.File)
	case nil:
	default:
		return func() tea.Msg { return msg }
	}
	return nil
}

// commitLoadFile loads file i's two sides (from the cache when it can).
func (m *Model) commitLoadFile(i int) tea.Cmd {
	cv := m.commitView
	if cv == nil || !cv.open {
		return nil
	}
	f := cv.card.File(i)
	if !f.Pending {
		return nil
	}
	sha := cv.info.FullHash
	key := sha + ":" + f.Name
	if t, ok := cv.blobs[key]; ok {
		cv.card.SetFileText(i, t[0], t[1])
		return nil
	}
	var cf git.CommitFile
	if i >= 0 && i < len(cv.info.Files) {
		cf = cv.info.Files[i]
	}
	hasParent := len(cv.info.Parents) > 0
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		out := commitFileTextMsg{sha: sha, file: i, path: cf.Path}
		oldPath := cf.Path
		if cf.OldPath != "" {
			oldPath = cf.OldPath
		}
		if hasParent && cf.Status != 'A' {
			out.old, _ = git.ShowFileAtRevision(cwd, sha+"^", oldPath)
		}
		if cf.Status != 'D' {
			out.new, _ = git.ShowFileAtRevision(cwd, sha, cf.Path)
		}
		if tooBigToDiff(out.old) || tooBigToDiff(out.new) {
			out.old, out.new, out.note = "", "", commitTooBig
		}
		return out
	}
}

func tooBigToDiff(s string) bool {
	return len(s) > commitMaxBytes || strings.Count(s, "\n") > commitMaxLines
}

// onCommitFileText puts a loaded file on the card (if it is still the
// commit on show) and keeps it in the cache.
func (m *Model) onCommitFileText(msg commitFileTextMsg) {
	cv := m.commitView
	if cv == nil || !cv.open || cv.info.FullHash != msg.sha {
		return
	}
	if msg.note != "" {
		cv.card.SetFileNote(msg.file, msg.note)
		return
	}
	cv.card.SetFileText(msg.file, msg.old, msg.new)
	key := msg.sha + ":" + msg.path
	if _, ok := cv.blobs[key]; !ok {
		cv.order = append(cv.order, key)
		if len(cv.order) > commitBlobCache {
			delete(cv.blobs, cv.order[0])
			cv.order = cv.order[1:]
		}
	}
	cv.blobs[key] = [2]string{msg.old, msg.new}
}

// commitViewFollow moves an open card to the GRAPH commit under the
// cursor (after j / k).
func (m *Model) commitViewFollow() tea.Cmd {
	if !m.commitViewShown() {
		return nil
	}
	if cur, ok := m.gitCurrentRow(); ok && cur.kind == gitRowCommit {
		return m.gitOpenCommitViewCmd(cur.hash)
	}
	return nil
}

// commitAction runs a card action on file i.
func (m *Model) commitAction(id string, i int) tea.Cmd {
	cv := m.commitView
	if cv == nil {
		return nil
	}
	info := cv.info
	switch {
	case id == "copy-sha":
		m.copyToClipboard(info.FullHash)
		var cmd tea.Cmd
		m.toast, cmd = m.toast.Push(toast.Info, "Copied "+info.Hash)
		return cmd
	case id == "raw":
		m.closeCommitView()
		return m.gitShowCommitCmd(info.Hash)
	case strings.HasPrefix(id, "parent:"):
		return m.gitOpenCommitViewCmd(strings.TrimPrefix(id, "parent:"))
	}
	return nil
}

// commitViewTitle is the card's title for messages ("a1b2c3d \"Fix…\"").
func commitViewTitle(info git.CommitInfo) string {
	s := info.Subject
	if len([]rune(s)) > 40 {
		s = string([]rune(s)[:39]) + "…"
	}
	return fmt.Sprintf("%s %q", info.Hash, s)
}
