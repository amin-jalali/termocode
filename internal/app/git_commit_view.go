package app

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The commit view (scm-graph design §3): a GRAPH commit opens in the diff
// card (internal/diffview) in commit mode. When the editor is at least
// commitDockMinW cells wide the card docks over it and the sidebar stays
// live (moving the GRAPH cursor shows that commit); otherwise it floats
// over the whole screen. File texts load one by one when selected.

const (
	commitDockMinW  = 80
	commitBlobCache = 32      // file pairs kept (sha:path)
	commitMaxBytes  = 2 << 20 // a side over this is not diffed
	commitMaxLines  = 20000   // nor over this many lines
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
	syn     map[string][2][][]diffview.Tok // tokens of the cached blobs
	order   []string                       // blobs, oldest first
	pick    string                         // the commit a revert / cherry-pick confirm is about
	// The two sides of the files on show: sha^ and sha for a commit, a
	// and b for a compare.
	oldRev, newRev string
	compare        *commitRange // set: the card compares two commits
	mark           string       // a GRAPH commit marked for compare (Space)
	mark2          string       // the second mark, until the compare opens
}

// commitRange is a compare: full and short hashes, older first.
type commitRange struct{ a, b, sa, sb string }

// commitViewReadyMsg carries a commit's (or a range's) meta and files.
type commitViewReadyMsg struct {
	want    string
	info    git.CommitInfo
	compare *commitRange
	err     error
}

// commitFileTextMsg carries one file's two sides (or a note).
type commitFileTextMsg struct {
	sides    string // oldRev..newRev they were read at
	file     int
	path     string
	old, new string
	note     string
}

// commitFileSyntaxMsg carries one loaded file's syntax tokens.
type commitFileSyntaxMsg struct {
	sides    string
	file     int
	path     string
	old, new [][]diffview.Tok
}

// commitViewShown reports whether the commit card is on screen.
func (m Model) commitViewShown() bool { return m.commitView != nil && m.commitView.open }

// gitOpenCommitViewCmd opens (or moves) the commit view to hash. The git
// call runs off the loop; the newest request wins.
func (m *Model) gitOpenCommitViewCmd(hash string) tea.Cmd {
	if hash == "" {
		return nil
	}
	cv := m.commitViewS()
	if cv.open && cv.compare == nil && (cv.info.FullHash == hash || cv.info.Hash == hash) {
		cv.want = hash
		return nil
	}
	cv.want = hash
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		info, err := git.CommitFiles(cwd, hash)
		return commitViewReadyMsg{want: hash, info: info, err: err}
	}
}

// commitViewS is the commit view state (made on first use).
func (m *Model) commitViewS() *commitViewState {
	if m.commitView == nil {
		m.commitView = &commitViewState{blobs: map[string][2]string{}, syn: map[string][2][][]diffview.Tok{}}
	}
	return m.commitView
}

// gitOpenCompareCmd opens the card on the change from commit a to b
// (a older).
func (m *Model) gitOpenCompareCmd(r commitRange) tea.Cmd {
	cv := m.commitViewS()
	cv.want = r.a + ".." + r.b
	want := cv.want
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		info, err := git.CommitFilesBetween(cwd, r.a, r.b)
		return commitViewReadyMsg{want: want, info: info, compare: &r, err: err}
	}
}

// gitCompareMark marks GRAPH commit ci for compare; with another commit
// marked already it opens the compare card (older → newer).
func (m *Model) gitCompareMark(ci int) tea.Cmd {
	if ci < 0 || ci >= len(m.gitCommits) {
		return nil
	}
	cv := m.commitViewS()
	c := m.gitCommits[ci]
	var cmd tea.Cmd
	if cv.mark == "" || cv.mark == c.FullHash {
		cv.mark = c.FullHash
		m.toast, cmd = m.toast.Push(toast.Info, "Marked "+c.Hash+" · Space on another commit to compare")
		return cmd
	}
	other := -1
	for i, g := range m.gitCommits {
		if g.FullHash == cv.mark {
			other = i
		}
	}
	if other < 0 {
		cv.mark = ""
		return nil
	}
	cv.mark2 = c.FullHash // both show until the compare card is ready
	older, newer := m.gitCommits[other], c
	if other < ci { // the list is newest first
		older, newer = c, m.gitCommits[other]
	}
	return m.gitOpenCompareCmd(commitRange{a: older.FullHash, b: newer.FullHash, sa: older.Hash, sb: newer.Hash})
}

// gitCompareMarkOf is the compare mark ("1", "2" or "") of a GRAPH commit.
func (m Model) gitCompareMarkOf(full string) string {
	cv := m.commitView
	switch {
	case cv == nil || full == "":
		return ""
	case full == cv.mark:
		return "1"
	case full == cv.mark2:
		return "2"
	}
	return ""
}

// clearCompareMarks drops the GRAPH compare marks; it reports whether
// there were any.
func (m *Model) clearCompareMarks() bool {
	cv := m.commitView
	if cv == nil || cv.mark == "" && cv.mark2 == "" {
		return false
	}
	cv.mark, cv.mark2 = "", ""
	return true
}

// onCommitViewReady builds the card for a loaded commit.
func (m *Model) onCommitViewReady(msg commitViewReadyMsg) tea.Cmd {
	cv := m.commitView
	if cv == nil || msg.want != cv.want {
		return nil
	}
	if msg.compare != nil {
		m.clearCompareMarks() // the compare is open (or failed): marks done
	}
	if msg.err != nil {
		var cmd tea.Cmd
		m.toast, cmd = m.toast.PushDetail(toast.Errr, "Could not read the commit", msg.err.Error())
		return cmd
	}
	info := msg.info
	wasOpen := cv.open
	cv.info, cv.compare = info, msg.compare
	if r := msg.compare; r != nil {
		cv.oldRev, cv.newRev = r.a, r.b
		cv.card = diffview.NewCommit(compareHeader(info, *r), commitFiles(info), []diffview.Button{
			{ID: "copy-range", Label: "Copy range"}, {ID: "open-file", Label: "Open file"}, {ID: "hide", Label: "Close"}})
	} else {
		cv.oldRev, cv.newRev = "", info.FullHash
		if len(info.Parents) > 0 {
			cv.oldRev = info.FullHash + "^"
		}
		cv.card = diffview.NewCommit(commitHeader(info), commitFiles(info), commitButtons())
	}
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

// compareHeader is the header of a compare card.
func compareHeader(info git.CommitInfo, r commitRange) diffview.Header {
	word := "commits"
	if info.Commits == 1 {
		word = "commit"
	}
	return diffview.Header{
		Title:   "Compare " + r.sa + " → " + r.sb,
		Subject: fmt.Sprintf("%d %s from %s to %s", info.Commits, word, r.sa, r.sb),
		Files:   info.CommitDetail.Files, Add: info.Add, Del: info.Del,
		OldLabel: "− " + r.sa, NewLabel: "+ " + r.sb,
	}
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
		x, y, w, h := c.Rect()
		inside := msg.X >= x && msg.X < x+w && msg.Y >= y && msg.Y < y+h
		if docked && !inside {
			if msg.Action == tea.MouseActionPress && m.mouseProbe == nil {
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
	sides := cv.oldRev + ".." + cv.newRev
	key := sides + ":" + f.Name
	if t, ok := cv.blobs[key]; ok {
		cv.card.SetFileText(i, t[0], t[1])
		if s, ok := cv.syn[key]; ok {
			cv.card.SetFileSyntax(i, s[0], s[1])
			return nil
		}
		return commitTokenizeCmd(sides, i, f.Name, t[0], t[1])
	}
	var cf git.CommitFile
	if i >= 0 && i < len(cv.info.Files) {
		cf = cv.info.Files[i]
	}
	oldRev, newRev := cv.oldRev, cv.newRev
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		out := commitFileTextMsg{sides: sides, file: i, path: cf.Path}
		oldPath := cf.Path
		if cf.OldPath != "" {
			oldPath = cf.OldPath
		}
		if oldRev != "" && cf.Status != 'A' {
			out.old, _ = git.ShowFileAtRevision(cwd, oldRev, oldPath)
		}
		if cf.Status != 'D' {
			out.new, _ = git.ShowFileAtRevision(cwd, newRev, cf.Path)
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
// commit on show) and keeps it in the cache. It returns the Cmd that
// computes the file's syntax tokens: the card shows plain until then.
func (m *Model) onCommitFileText(msg commitFileTextMsg) tea.Cmd {
	cv := m.commitView
	if cv == nil || !cv.open || cv.oldRev+".."+cv.newRev != msg.sides {
		return nil
	}
	if msg.note != "" {
		cv.card.SetFileNote(msg.file, msg.note)
		return nil
	}
	cv.card.SetFileText(msg.file, msg.old, msg.new)
	key := msg.sides + ":" + msg.path
	if _, ok := cv.blobs[key]; !ok {
		cv.order = append(cv.order, key)
		if len(cv.order) > commitBlobCache {
			delete(cv.blobs, cv.order[0])
			delete(cv.syn, cv.order[0])
			cv.order = cv.order[1:]
		}
	}
	cv.blobs[key] = [2]string{msg.old, msg.new}
	return commitTokenizeCmd(msg.sides, msg.file, msg.path, msg.old, msg.new)
}

// commitTokenizeCmd computes a file's syntax tokens off the loop (both
// sides at once); nil when chroma does not know the file type.
func commitTokenizeCmd(sides string, file int, path, old, new string) tea.Cmd {
	if !diffview.HasLexer(path) {
		return nil
	}
	return func() tea.Msg {
		out := commitFileSyntaxMsg{sides: sides, file: file, path: path}
		done := make(chan struct{})
		crash.Go("the syntax colors", func() {
			defer close(done) // also after a panic: the wait below ends
			out.old = diffview.Tokenize(path, old)
		})
		out.new = diffview.Tokenize(path, new)
		<-done
		return out
	}
}

// onCommitFileSyntax colours a loaded file (if it is still on show) and
// keeps the tokens next to its cached text.
func (m *Model) onCommitFileSyntax(msg commitFileSyntaxMsg) {
	cv := m.commitView
	if cv == nil || !cv.open || cv.oldRev+".."+cv.newRev != msg.sides {
		return
	}
	key := msg.sides + ":" + msg.path
	if _, ok := cv.blobs[key]; ok {
		cv.syn[key] = [2][][]diffview.Tok{msg.old, msg.new}
	}
	if f := cv.card.File(msg.file); f.Name == msg.path && !f.Pending {
		cv.card.SetFileSyntax(msg.file, msg.old, msg.new)
	}
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
	if r := cv.compare; r != nil {
		switch id {
		case "copy-sha", "copy-range":
			m.copyToClipboard(r.a + ".." + r.b)
			var cmd tea.Cmd
			m.toast, cmd = m.toast.Push(toast.Info, "Copied "+r.sa+".."+r.sb)
			return cmd
		case "raw", "revert", "cherry-pick", "open-at-commit", "more":
			if id != "more" {
				return nil // commit-only actions
			}
		}
	}
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
	case id == "more":
		m.openCommitMoreMenu()
		return nil
	case id == "revert", id == "cherry-pick":
		return m.openCommitPickConfirm(id)
	}
	if i < 0 || i >= len(info.Files) {
		return nil
	}
	f := info.Files[i]
	switch id {
	case "open-file":
		abs := gitFileAbsPath(f.Path)
		if _, err := os.Stat(abs); err != nil {
			var cmd tea.Cmd
			m.toast, cmd = m.toast.Push(toast.Warn, f.Path+" is not in the working tree")
			return cmd
		}
		m.closeCommitView()
		if m.nvim != nil {
			m.ensureEditorWindowCurrent()
			if err := m.nvim.OpenFile("edit", abs); err != nil {
				m.err = err.Error()
			}
		}
		m.focus = FocusEditor
	case "open-at-commit":
		if f.Status == 'D' {
			var cmd tea.Cmd
			m.toast, cmd = m.toast.Push(toast.Warn, f.Path+" was deleted in this commit")
			return cmd
		}
		cwd, err := m.rootDir()
		if err != nil {
			return nil
		}
		sha, short := info.FullHash, info.Hash
		return func() tea.Msg {
			text, _ := git.ShowFileAtRevision(cwd, sha, f.Path)
			return commitFileAtMsg{path: f.Path, short: short, text: text}
		}
	}
	return nil
}

// commitFileAtMsg carries a file as it was in a commit (File @ commit).
type commitFileAtMsg struct {
	path, short, text string
}

// openFileAtCommit shows a file at a commit in a read-only scratch buffer
// named "path @ sha" (no temp file: the text goes over RPC).
func (m *Model) openFileAtCommit(msg commitFileAtMsg) {
	m.closeCommitView()
	m.focus = FocusEditor
	if m.nvim == nil {
		return
	}
	m.ensureEditorWindowCurrent()
	if err := m.openReadOnlyPage(msg.path+" @ "+msg.short, msg.text, msg.path, ""); err != nil {
		m.err = "file at commit: " + err.Error()
	}
}

func (m *Model) openReadOnlyPage(name, text, path, filetype string) error {
	if m.nvim == nil {
		return nil
	}
	return m.nvim.ExecLuaArgs(openReadOnlyPageLua, name, text, path, filetype)
}

const openReadOnlyPageLua = `
local name, text, path, ft = ...
vim.cmd('enew')
local buf = vim.api.nvim_get_current_buf()
vim.bo[buf].buftype = 'nofile'
vim.bo[buf].swapfile = false
vim.bo[buf].bufhidden = 'wipe'
local old = vim.fn.bufnr(name)
if old > 0 and old ~= buf then pcall(vim.api.nvim_buf_delete, old, { force = true }) end
pcall(vim.api.nvim_buf_set_name, buf, name)
local lines = vim.split(text, '\n', { plain = true })
if #lines > 1 and lines[#lines] == '' then table.remove(lines) end
vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
vim.bo[buf].modifiable = false
vim.bo[buf].readonly = true
if ft == '' then ft = vim.filetype.match({ filename = path }) end
if ft then vim.bo[buf].filetype = ft end
pcall(vim.api.nvim_win_set_cursor, 0, { 1, 0 })
vim.cmd('stopinsert')
`

// openCommitMoreMenu lists the footer buttons that folded into ⋯.
func (m *Model) openCommitMoreMenu() {
	c := m.commitCard()
	var items []menu.Item
	for _, b := range c.FoldedButtons() {
		items = append(items, menu.Item{ID: b.ID, Title: b.Label})
	}
	if len(items) == 0 {
		return
	}
	x, y := c.Origin()
	for _, h := range c.Hits() {
		if h.ID == "more" {
			x, y = h.X, h.Y
		}
	}
	m.menu = menu.New(items, x, y)
	m.menu.SetScreenSize(m.w, m.h)
	m.menuOpen = true
	m.menuKind = menuKindCommitMore
}

// openCommitPickConfirm asks before a revert or cherry-pick of the
// commit on show.
func (m *Model) openCommitPickConfirm(verb string) tea.Cmd {
	cv := m.commitView
	info := cv.info
	if verb == "cherry-pick" {
		for _, c := range m.gitCommits {
			if c.IsHead && c.FullHash == info.FullHash {
				var cmd tea.Cmd
				m.toast, cmd = m.toast.Push(toast.Info, "This commit is HEAD already")
				return cmd
			}
		}
	}
	title, body, button := "Revert commit?",
		fmt.Sprintf("Revert %s? This creates a new commit that undoes it.", commitViewTitle(info)), "Revert"
	kind := confirmKindCommitRevert
	if verb == "cherry-pick" {
		title, body, button = "Cherry-pick commit?",
			fmt.Sprintf("Apply %s on top of HEAD? This creates a new commit.", commitViewTitle(info)), "Cherry-pick"
		kind = confirmKindCommitCherryPick
	}
	cv.pick = info.FullHash
	m.confirm = confirm.Risky(title, body,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: verb, Title: button, Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = kind
	return nil
}

// commitPickDoneMsg is the end of a revert / cherry-pick.
type commitPickDoneMsg struct {
	verb, short string
	conflicts   []string
	err         error
}

// runCommitPick runs the confirmed revert / cherry-pick off the loop.
func (m *Model) runCommitPick(id string) tea.Cmd {
	cv := m.commitView
	if cv == nil || cv.pick == "" || id == "cancel" {
		return nil
	}
	sha, short := cv.pick, cv.info.Hash
	cv.pick = ""
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		var conflicts []string
		var err error
		if id == "revert" {
			conflicts, err = git.Revert(cwd, sha)
		} else {
			conflicts, err = git.CherryPick(cwd, sha)
		}
		return commitPickDoneMsg{verb: id, short: short, conflicts: conflicts, err: err}
	}
}

// onCommitPickDone says how it went and refreshes Source Control (a
// conflict shows in CONFLICTS).
func (m *Model) onCommitPickDone(msg commitPickDoneMsg) tea.Cmd {
	m.closeCommitView()
	what := "Revert"
	done := "Reverted " + msg.short
	if msg.verb == "cherry-pick" {
		what, done = "Cherry-pick", "Cherry-picked "+msg.short
	}
	var cmd tea.Cmd
	switch {
	case len(msg.conflicts) > 0:
		word := "conflicts"
		if len(msg.conflicts) == 1 {
			word = "conflict"
		}
		m.toast, cmd = m.toast.PushDetail(toast.Warn, fmt.Sprintf("%s stopped: %d %s", what, len(msg.conflicts), word),
			"Fix them in CONFLICTS, then commit.")
	case msg.err != nil:
		m.toast, cmd = m.toast.PushDetail(toast.Errr, what+" failed", msg.err.Error())
	default:
		m.toast, cmd = m.toast.Push(toast.Info, done)
	}
	return tea.Batch(cmd, fetchGitCmd())
}

// commitViewTitle is the card's title for messages ("a1b2c3d \"Fix…\"").
func commitViewTitle(info git.CommitInfo) string {
	s := info.Subject
	if len([]rune(s)) > 40 {
		s = string([]rune(s)[:39]) + "…"
	}
	return fmt.Sprintf("%s %q", info.Hash, s)
}
