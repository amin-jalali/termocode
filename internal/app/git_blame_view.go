package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/cfgfile"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Blame in the editor (GITX-3, GITX-4, GITX-5): the cursor line's
// author in the status bar and at the end of the line, blame
// annotations beside every line coloured by age, the line's commit,
// blame before a change, line history and stepping a file through its
// revisions. One `git blame` per file and refresh feeds all of them.

const pickerKindBlameOptions pickerKindEnum = 3640

// gitRevScheme names a read-only buffer that shows a file at a commit:
// termocode-rev://<hash>/<path in the repo>.
const gitRevScheme = "termocode-rev://"

// gitRevMaxBytes caps the text of one revision buffer.
const gitRevMaxBytes = 8 << 20

// gitBlameState is the blame of the current file and the blame options.
type gitBlameState struct {
	init bool
	// Settings: inline (git_inline_blame), status (git_status_blame),
	// ignoreWS (git_blame_ignore_whitespace), moves (git_blame_detect_moves).
	inline, status, ignoreWS, moves bool
	// annotate: blame annotations beside every line (not saved).
	annotate bool

	key     string // file + options the lines belong to
	loading string // key of the load in flight
	lines   []git.BlameRangeLine
	err     string

	inlineText string        // text painted at the end of the cursor line
	rows       []blameAnnRow // annotations painted beside the lines
}

// gitBlameLoadedMsg is a finished blame of key.
type gitBlameLoadedMsg struct {
	key   string
	lines []git.BlameRangeLine
	err   error
}

// gitRevisionReadyMsg is a file at a commit, ready to open read-only.
type gitRevisionReadyMsg struct {
	rev, rel, text string
	line           int  // cursor line (1-based)
	annotate       bool // blame annotations on (Blame Before This Change)
	note           string
}

func gitRevBufName(rev, rel string) string { return gitRevScheme + rev + "/" + rel }

func parseGitRevBufName(name string) (rev, rel string, ok bool) {
	rest, ok := strings.CutPrefix(name, gitRevScheme)
	if !ok {
		return "", "", false
	}
	rev, rel, ok = strings.Cut(rest, "/")
	return rev, rel, ok && rev != "" && rel != ""
}

// blameStatusText is the status-bar text of one line: "Tess, 3 days ago".
func blameStatusText(l git.BlameRangeLine) string {
	if l.Hash == "uncommitted" || l.Full == "" && l.When == 0 {
		return "Not committed yet"
	}
	return l.Author + ", " + git.Ago(l.When)
}

// blameInlineText is the end-of-line text: the status text and subject.
func blameInlineText(l git.BlameRangeLine) string {
	if l.Hash == "uncommitted" || l.Full == "" && l.When == 0 {
		return "You · Not committed yet"
	}
	return blameStatusText(l) + " · " + l.Subject
}

// blameAnnRow is one line's annotation: text (all rows one width) and
// age bucket 0 (newest) … 4 (oldest) for the colour.
type blameAnnRow struct {
	Text string
	Age  int
}

const blameAnnAuthorW = 12

// blameAnnotateRows names each run of lines from one commit once
// ("abc1234 Tess         3d"), pads the rest, and buckets the ages
// between the file's oldest and newest commit.
func blameAnnotateRows(ls []git.BlameRangeLine) []blameAnnRow {
	var lo, hi int64
	for _, l := range ls {
		if l.When == 0 {
			continue
		}
		if lo == 0 || l.When < lo {
			lo = l.When
		}
		if l.When > hi {
			hi = l.When
		}
	}
	texts := make([]string, len(ls))
	w := 0
	for i, l := range ls {
		if i > 0 && ls[i-1].Hash == l.Hash {
			continue
		}
		var t string
		if l.Hash == "uncommitted" {
			t = "not committed yet"
		} else {
			a := []rune(l.Author)
			if len(a) > blameAnnAuthorW {
				a = append(a[:blameAnnAuthorW-1], '…')
			}
			t = fmt.Sprintf("%s %-*s %s", l.Hash, blameAnnAuthorW, string(a), blameShortAge(l.When))
		}
		texts[i] = t
		w = max(w, len([]rune(t)))
	}
	rows := make([]blameAnnRow, len(ls))
	for i, l := range ls {
		t := texts[i]
		rows[i].Text = t + strings.Repeat(" ", w-len([]rune(t)))
		if l.When > 0 && hi > lo {
			rows[i].Age = int(4 * (hi - l.When) / (hi - lo))
		}
	}
	return rows
}

// blameShortAge is a compact age: 5m, 3h, 2d, 6w, 4mo, 2y.
func blameShortAge(when int64) string {
	d := clock.Now().Unix() - when
	switch {
	case d < 3600:
		return fmt.Sprintf("%dm", max(d/60, 0))
	case d < 86400:
		return fmt.Sprintf("%dh", d/3600)
	case d < 14*86400:
		return fmt.Sprintf("%dd", d/86400)
	case d < 60*86400:
		return fmt.Sprintf("%dw", d/(7*86400))
	case d < 365*86400:
		return fmt.Sprintf("%dmo", d/(30*86400))
	}
	return fmt.Sprintf("%dy", d/(365*86400))
}

// gitBlameInit reads the blame settings once.
func (m *Model) gitBlameInit() {
	if m.gitBlame.init {
		return
	}
	c := loadSettings()
	on := func(p *bool) bool { return p != nil && *p }
	m.gitBlame.init = true
	m.gitBlame.inline, m.gitBlame.status = on(c.GitInlineBlame), on(c.GitStatusBlame)
	m.gitBlame.ignoreWS, m.gitBlame.moves = on(c.GitBlameIgnoreWS), on(c.GitBlameDetectMoves)
}

// saveGitBlameSettings writes the four blame settings to config.json.
func (m *Model) saveGitBlameSettings() {
	path, err := settingsConfigPath()
	if err != nil {
		return
	}
	b := m.gitBlame
	_ = cfgfile.Update(path, func(c map[string]any) error {
		c["git_inline_blame"], c["git_status_blame"] = b.inline, b.status
		c["git_blame_ignore_whitespace"], c["git_blame_detect_moves"] = b.ignoreWS, b.moves
		return nil
	})
}

func (m *Model) gitBlameWanted() bool {
	return m.gitBlame.inline || m.gitBlame.status || m.gitBlame.annotate
}

// gitBlameTarget is the file to blame: a file in the repo, or a revision
// buffer (rev set).
func (m *Model) gitBlameTarget() (cwd, rel, rev string, ok bool) {
	p := m.editor.Path()
	if p == "" {
		return "", "", "", false
	}
	cwd, err := m.rootDir()
	if err != nil {
		return "", "", "", false
	}
	if r, rl, ok := parseGitRevBufName(p); ok {
		return cwd, rl, r, true
	}
	rel, err = filepath.Rel(cwd, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", "", false
	}
	return cwd, filepath.ToSlash(rel), "", true
}

// gitBlameKey names what a blame belongs to: the file, the revision, the
// options and, for a working file, its size and time and HEAD, so a save
// or a commit loads it again.
func (m *Model) gitBlameKey() string {
	cwd, rel, rev, ok := m.gitBlameTarget()
	if !ok {
		return ""
	}
	key := fmt.Sprintf("%s@%s ws=%v moves=%v", rel, rev, m.gitBlame.ignoreWS, m.gitBlame.moves)
	if rev == "" {
		if st, err := os.Stat(filepath.Join(cwd, filepath.FromSlash(rel))); err == nil {
			key += fmt.Sprintf(" %d/%d", st.Size(), st.ModTime().UnixNano())
		}
		key += " head=" + gitGraphHead(m.gitCommits)
	}
	return key
}

// gitBlameRefreshCmd loads the current file's blame when something
// shows it and it is not loaded (force: load again, the file or HEAD
// changed).
func (m *Model) gitBlameRefreshCmd(force bool) tea.Cmd {
	if !m.gitBlameWanted() {
		return nil
	}
	cwd, rel, rev, ok := m.gitBlameTarget()
	key := m.gitBlameKey()
	if !ok {
		m.gitBlame.key, m.gitBlame.lines = "", nil
		m.paintGitBlame()
		return nil
	}
	if !force && (key == m.gitBlame.key || key == m.gitBlame.loading) {
		return nil
	}
	m.gitBlame.loading = key
	opts := git.BlameOptions{Rev: rev, IgnoreWhitespace: m.gitBlame.ignoreWS, DetectMoves: m.gitBlame.moves}
	return func() tea.Msg {
		ls, err := git.BlameFile(cwd, rel, opts)
		return gitBlameLoadedMsg{key: key, lines: ls, err: err}
	}
}

// onGitBlameLoaded keeps the blame of the file still shown and paints it.
func (m *Model) onGitBlameLoaded(msg gitBlameLoadedMsg) {
	if msg.key == m.gitBlame.loading {
		m.gitBlame.loading = ""
	}
	if msg.key != m.gitBlameKey() {
		return // the user moved on to another file
	}
	m.gitBlame.key, m.gitBlame.lines, m.gitBlame.err = msg.key, msg.lines, ""
	if msg.err != nil {
		m.gitBlame.lines, m.gitBlame.err = nil, msg.err.Error()
	}
	m.paintGitBlame()
}

// gitBlameLineAt is the loaded blame of line n of the current file.
func (m *Model) gitBlameLineAt(n int) (git.BlameRangeLine, bool) {
	if m.gitBlame.key == "" || m.gitBlame.key != m.gitBlameKey() {
		return git.BlameRangeLine{}, false
	}
	for _, l := range m.gitBlame.lines {
		if l.Line == n {
			return l, true
		}
	}
	return git.BlameRangeLine{}, false
}

// gitBlameStatusTexts is the status-bar item of the cursor line.
func (m Model) gitBlameStatusTexts() []string {
	if !m.gitBlame.status || m.gitBlame.key == "" {
		return nil
	}
	l, ok := m.gitBlameLineAt(max(m.cursorLine, 1))
	if !ok {
		return nil
	}
	return []string{"{{muted}}" + gitBlameGlyph() + "{{/}} " + blameStatusText(l)}
}

func gitBlameGlyph() string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return "@"
	}
	return "⊙"
}

// paintGitBlame paints the inline text and the annotations.
func (m *Model) paintGitBlame() {
	m.paintInlineBlame()
	m.paintBlameAnnotations()
}

// paintInlineBlame puts the cursor line's blame at its end.
func (m *Model) paintInlineBlame() {
	text, line := "", max(m.cursorLine, 1)
	if m.gitBlame.inline {
		if l, ok := m.gitBlameLineAt(line); ok {
			text = blameInlineText(l)
		}
	}
	if text == m.gitBlame.inlineText && text == "" {
		return
	}
	m.gitBlame.inlineText = text
	if m.nvim != nil {
		_, _ = m.nvim.EvalLuaStringArgs(gitBlameInlineLua, text, line, hexOfToken(theme.TextDim))
	}
}

// paintBlameAnnotations puts every line's annotation beside it.
func (m *Model) paintBlameAnnotations() {
	var rows []blameAnnRow
	if m.gitBlame.annotate && m.gitBlame.key != "" {
		rows = blameAnnotateRows(m.gitBlame.lines)
	}
	had := m.gitBlame.rows != nil
	m.gitBlame.rows = rows
	if m.nvim == nil || (!had && rows == nil) {
		return
	}
	texts := make([]any, len(rows))
	ages := make([]any, len(rows))
	for i, r := range rows {
		texts[i], ages[i] = r.Text, r.Age
	}
	_, _ = m.nvim.EvalLuaStringArgs(gitBlameAnnotateLua, texts, ages, gitBlameAgeColours())
}

// gitBlameAgeColours are fg/bg pairs from newest (amber) to oldest (blue).
func gitBlameAgeColours() []any {
	bg := hexOfToken(theme.BgEditor)
	nw, old := hexOfToken(theme.AccentAmber), hexOfToken(theme.AccentBlue)
	var out []any
	for i := 0; i < 5; i++ {
		c := blendHex(old, nw, float64(i)/4)
		out = append(out, []any{c, blendHex(c, bg, 0.14)})
	}
	return out
}

const gitBlameInlineLua = `
local text, line, fg = ...
local ns = vim.api.nvim_create_namespace('termocode_blame_inline')
vim.api.nvim_set_hl(0, 'TermocodeBlameInline', { fg = fg, italic = true })
local prev = _G.termocode_blame_inline_buf
if prev and vim.api.nvim_buf_is_valid(prev) then vim.api.nvim_buf_clear_namespace(prev, ns, 0, -1) end
_G.termocode_blame_inline_buf = nil
if text == '' then return '' end
local b = vim.api.nvim_get_current_buf()
pcall(vim.api.nvim_buf_set_extmark, b, ns, line - 1, 0, { virt_text = { { '    ' .. text, 'TermocodeBlameInline' } }, virt_text_pos = 'eol', hl_mode = 'combine' })
_G.termocode_blame_inline_buf = b
return ''
`

const gitBlameAnnotateLua = `
local texts, ages, colours = ...
local ns = vim.api.nvim_create_namespace('termocode_blame_annotate')
for i, c in ipairs(colours) do
  vim.api.nvim_set_hl(0, 'TermocodeBlameAge' .. (i - 1), { fg = c[1], bg = c[2] })
end
local prev = _G.termocode_blame_annotate_buf
if prev and vim.api.nvim_buf_is_valid(prev) then vim.api.nvim_buf_clear_namespace(prev, ns, 0, -1) end
_G.termocode_blame_annotate_buf = nil
if #texts == 0 then return '' end
local b = vim.api.nvim_get_current_buf()
local n = vim.api.nvim_buf_line_count(b)
for i, t in ipairs(texts) do
  if i > n then break end
  local ok = pcall(vim.api.nvim_buf_set_extmark, b, ns, i - 1, 0, { virt_text = { { t .. ' ', 'TermocodeBlameAge' .. ages[i] }, { ' ', 'Normal' } }, virt_text_pos = 'inline', right_gravity = false })
  if not ok then
    pcall(vim.api.nvim_buf_set_extmark, b, ns, i - 1, 0, { virt_text = { { t, 'TermocodeBlameAge' .. ages[i] } }, virt_text_pos = 'right_align' })
  end
end
_G.termocode_blame_annotate_buf = b
return ''
`

// onGitBlameState follows the cursor (inline blame) and loads the blame
// of a file that became current.
func (m *Model) onGitBlameState() tea.Cmd {
	m.gitBlameInit()
	if !m.gitBlameWanted() {
		return nil
	}
	cmd := m.gitBlameRefreshCmd(false)
	if cmd == nil {
		m.paintInlineBlame()
	}
	return cmd
}

func (m *Model) gitBlameToast(level toast.Severity, text, detail string) tea.Cmd {
	var c tea.Cmd
	if detail != "" {
		m.toast, c = m.toast.PushDetail(level, text, detail)
	} else {
		m.toast, c = m.toast.Push(level, text)
	}
	return c
}

// toggleInlineBlame turns the end-of-line blame on or off (saved).
func (m *Model) toggleInlineBlame() tea.Cmd {
	m.gitBlameInit()
	m.gitBlame.inline = !m.gitBlame.inline
	m.saveGitBlameSettings()
	if !m.gitBlame.inline {
		m.paintInlineBlame()
		return m.gitBlameToast(toast.Info, "Inline blame off", "")
	}
	return tea.Batch(m.gitBlameShowCmd(), m.gitBlameToast(toast.Info, "Inline blame on", ""))
}

// toggleStatusBlame turns the status-bar blame on or off (saved).
func (m *Model) toggleStatusBlame() tea.Cmd {
	m.gitBlameInit()
	m.gitBlame.status = !m.gitBlame.status
	m.saveGitBlameSettings()
	if !m.gitBlame.status {
		return m.gitBlameToast(toast.Info, "Blame in the status bar off", "")
	}
	return tea.Batch(m.gitBlameShowCmd(), m.gitBlameToast(toast.Info, "Blame in the status bar on", "Click it to open the line's commit"))
}

// toggleBlameAnnotate turns the annotations beside every line on or off.
func (m *Model) toggleBlameAnnotate() tea.Cmd {
	m.gitBlameInit()
	m.gitBlame.annotate = !m.gitBlame.annotate
	if !m.gitBlame.annotate {
		m.paintBlameAnnotations()
		return nil
	}
	if _, _, _, ok := m.gitBlameTarget(); !ok {
		m.gitBlame.annotate = false
		return m.gitBlameToast(toast.Warn, "No file in the repository to blame", "")
	}
	return tea.Batch(m.gitBlameShowCmd(), m.gitBlameToast(toast.Info, "Blame annotations on",
		"Open Commit of Line opens its commit"))
}

// gitBlameShowCmd loads the blame, or paints the loaded one.
func (m *Model) gitBlameShowCmd() tea.Cmd {
	if cmd := m.gitBlameRefreshCmd(false); cmd != nil {
		return cmd
	}
	m.paintGitBlame()
	return nil
}

// gitBlameCursorLine blames the cursor line now (the cached blame when it
// is loaded, else one `git blame -L`).
func (m *Model) gitBlameCursorLine() (git.BlameRangeLine, tea.Cmd, bool) {
	line := max(m.cursorLine, 1)
	if l, ok := m.gitBlameLineAt(line); ok {
		return l, nil, true
	}
	cwd, rel, rev, ok := m.gitBlameTarget()
	if !ok {
		return git.BlameRangeLine{}, m.gitBlameToast(toast.Warn, "No file in the repository to blame", ""), false
	}
	ls, err := git.BlameFile(cwd, rel, git.BlameOptions{Rev: rev, IgnoreWhitespace: m.gitBlame.ignoreWS, DetectMoves: m.gitBlame.moves})
	if err != nil {
		return git.BlameRangeLine{}, m.gitBlameToast(toast.Errr, "Blame failed", err.Error()), false
	}
	for _, l := range ls {
		if l.Line == line {
			return l, nil, true
		}
	}
	return git.BlameRangeLine{}, m.gitBlameToast(toast.Warn, "No blame for this line", ""), false
}

// gitOpenLineCommit opens the commit that last changed the cursor line.
func (m *Model) gitOpenLineCommit() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	m.gitBlameInit()
	l, cmd, ok := m.gitBlameCursorLine()
	if !ok {
		return cmd
	}
	if l.Full == "" {
		return m.gitBlameToast(toast.Info, "Not committed yet", "This line has changes no commit has")
	}
	return m.gitShowCommitCmd(l.Hash)
}

// gitBlameBefore opens the file as it was before the cursor line's
// commit, annotated, on the line's old number.
func (m *Model) gitBlameBefore() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	m.gitBlameInit()
	l, cmd, ok := m.gitBlameCursorLine()
	if !ok {
		return cmd
	}
	if l.Full == "" {
		return m.gitBlameToast(toast.Info, "Not committed yet", "Commit the line first; then blame shows what came before")
	}
	if l.PrevHash == "" {
		return m.gitBlameToast(toast.Info, "This is the line's first commit", l.Hash+" · "+l.Subject)
	}
	cwd, _ := m.rootDir()
	prev := l.PrevHash[:min(7, len(l.PrevHash))]
	return gitRevisionCmd(cwd, prev, l.PrevPath, max(l.OrigLine, 1), true, "Before "+l.Hash+" · "+l.Subject)
}

// gitRevisionCmd reads rel at rev for a revision buffer.
func gitRevisionCmd(cwd, rev, rel string, line int, annotate bool, note string) tea.Cmd {
	return func() tea.Msg {
		text, found, cut, err := git.FileAt(cwd, rev, rel, gitRevMaxBytes)
		if err != nil {
			return ErrMsg{Err: err}
		}
		if !found {
			return ErrMsg{Err: fmt.Errorf("%s is not in %s", rel, rev)}
		}
		if cut {
			note += " (cut at 8 MB)"
		}
		return gitRevisionReadyMsg{rev: rev, rel: rel, text: text, line: line, annotate: annotate, note: note}
	}
}

// openGitRevision shows a file at a commit in a read-only buffer.
func (m *Model) openGitRevision(msg gitRevisionReadyMsg) tea.Cmd {
	m.gitBlameInit()
	if msg.annotate {
		m.gitBlame.annotate = true
	}
	c := m.gitBlameToast(toast.Info, msg.rel+" at "+msg.rev, msg.note)
	if m.nvim == nil {
		return c
	}
	tmp, err := os.CreateTemp("", "termocode-rev-*")
	if err != nil {
		return c
	}
	defer os.Remove(tmp.Name())
	_, _ = tmp.WriteString(msg.text)
	_ = tmp.Close()
	m.ensureEditorWindowCurrent()
	if _, err := m.nvim.EvalLuaStringArgs(gitRevisionLua, tmp.Name(), gitRevBufName(msg.rev, msg.rel), msg.rel, msg.line); err != nil {
		m.err = "revision: " + err.Error()
	}
	m.focus = FocusEditor
	return tea.Batch(c, m.fetchStateCmd())
}

const gitRevisionLua = `
local path, name, rel, line = ...
local old = vim.fn.bufnr(name)
if old > 0 then pcall(vim.api.nvim_buf_delete, old, { force = true }) end
vim.cmd('enew')
local b = vim.api.nvim_get_current_buf()
vim.bo[b].buftype = 'nofile'
vim.bo[b].swapfile = false
vim.bo[b].bufhidden = 'wipe'
local ok, lines = pcall(vim.fn.readfile, path)
if ok then vim.api.nvim_buf_set_lines(b, 0, -1, false, lines) end
pcall(vim.api.nvim_buf_set_name, b, name)
local ft = vim.filetype.match({ filename = rel, buf = b })
if ft then vim.bo[b].filetype = ft end
vim.bo[b].modifiable = false
vim.bo[b].readonly = true
pcall(vim.api.nvim_win_set_cursor, 0, { math.min(line, vim.api.nvim_buf_line_count(b)), 0 })
return ''
`

// gitLineHistoryPicker lists the commits that changed the cursor line
// (or the selected lines); Enter opens one.
func (m *Model) gitLineHistoryPicker() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, rel, rev, ok := m.gitBlameTarget()
	if !ok || rev != "" {
		return m.gitBlameToast(toast.Warn, "Open a file of the repository first", "")
	}
	from, to := m.gitLineRange()
	from, to = max(from, 1), max(to, 1)
	cs, err := git.LineHistory(cwd, rel, from, to)
	if err != nil {
		return m.gitBlameToast(toast.Errr, "Line history failed", err.Error())
	}
	if len(cs) == 0 {
		return m.gitBlameToast(toast.Info, "No commits changed these lines yet", "")
	}
	items := make([]picker.Item, 0, len(cs))
	for _, c := range cs {
		items = append(items, picker.Item{ID: "commit-" + c.Hash, Title: c.Hash + "  " + c.Subject, Hint: c.Author + ", " + git.Ago(c.When)})
	}
	lines := fmt.Sprintf("line %d", from)
	if to > from {
		lines = fmt.Sprintf("lines %d–%d", from, to)
	}
	m.picker = picker.NewItems(" History of "+lines+" · "+rel+" ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindGitLog // Enter opens the commit, as in the log
	return nil
}

// gitStepRevision opens the file's previous (step -1) or next (+1)
// revision. From the working file, "previous" is HEAD's version when the
// file has changes, else the one before.
func (m *Model) gitStepRevision(step int) tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, rel, rev, ok := m.gitBlameTarget()
	if !ok {
		return m.gitBlameToast(toast.Warn, "Open a file of the repository first", "")
	}
	cs, err := git.FileCommits(cwd, rel, 500)
	if err != nil {
		return m.gitBlameToast(toast.Errr, "File history failed", err.Error())
	}
	if len(cs) == 0 {
		return m.gitBlameToast(toast.Info, "No history (file untracked or new)", "")
	}
	idx := -1 // the working file
	if rev != "" {
		for i, c := range cs {
			if strings.HasPrefix(c.Hash, rev) || strings.HasPrefix(rev, c.Hash) {
				idx = i
				break
			}
		}
		if idx < 0 {
			return m.gitBlameToast(toast.Warn, rev+" did not change "+rel, "Previous / Next Revision step through the commits that did")
		}
	}
	next := idx - step // newest first: older is a higher index
	if idx < 0 && step < 0 {
		next = 0
		if head, found, _, err := git.FileAt(cwd, cs[0].Hash, rel, gitRevMaxBytes); err == nil && found {
			if disk, err := os.ReadFile(filepath.Join(cwd, filepath.FromSlash(rel))); err == nil && string(disk) == head {
				next = 1
			}
		}
	}
	switch {
	case idx < 0 && step > 0:
		return m.gitBlameToast(toast.Info, "This is the working file", "Previous Revision steps back")
	case next < 0:
		if m.nvim != nil {
			m.ensureEditorWindowCurrent()
			_ = m.nvim.ExecLuaArgs(`vim.cmd('edit ' .. vim.fn.fnameescape(...))`, filepath.Join(cwd, filepath.FromSlash(rel)))
		}
		return m.gitBlameToast(toast.Info, "Back at the working file", rel)
	case next >= len(cs):
		return m.gitBlameToast(toast.Info, "This is the oldest revision", cs[len(cs)-1].Hash+" · "+cs[len(cs)-1].Subject)
	}
	c := cs[next]
	note := fmt.Sprintf("%s · %s · %d of %d", c.Subject, c.Author, len(cs)-next, len(cs))
	return gitRevisionCmd(cwd, c.Hash, rel, max(m.cursorLine, 1), m.gitBlame.annotate, note)
}

// openBlameOptionsPicker shows the blame options as toggles.
func (m *Model) openBlameOptionsPicker() tea.Cmd {
	m.gitBlameInit()
	mark := func(on bool) string {
		if on {
			return "on"
		}
		return "off"
	}
	items := []picker.Item{
		{ID: "blame-opt-ws", Title: "Ignore whitespace changes (-w)", Hint: mark(m.gitBlame.ignoreWS)},
		{ID: "blame-opt-moves", Title: "Detect moved or copied lines (-M -C)", Hint: mark(m.gitBlame.moves)},
		{ID: "blame-opt-inline", Title: "Inline blame at the end of the line", Hint: mark(m.gitBlame.inline)},
		{ID: "blame-opt-status", Title: "Blame in the status bar", Hint: mark(m.gitBlame.status)},
	}
	m.picker = picker.NewItems(" Blame Options (Enter turns on or off) ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindBlameOptions
	return nil
}

// onBlameOptionPicked flips one option, saves it and blames again.
func (m *Model) onBlameOptionPicked(id string) tea.Cmd {
	m.gitBlameInit()
	switch id {
	case "blame-opt-ws":
		m.gitBlame.ignoreWS = !m.gitBlame.ignoreWS
	case "blame-opt-moves":
		m.gitBlame.moves = !m.gitBlame.moves
	case "blame-opt-inline":
		return m.toggleInlineBlame()
	case "blame-opt-status":
		return m.toggleStatusBlame()
	default:
		return nil
	}
	m.saveGitBlameSettings()
	m.gitBlame.key = ""
	if cmd := m.gitBlameRefreshCmd(true); cmd != nil {
		return cmd
	}
	return m.gitBlameToast(toast.Info, "Blame options saved", "")
}
