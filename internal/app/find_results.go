package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/search"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// openFindInFilesPrompt is the entry point for the Sublime-style
// "Find in Files" buffer view. We collect the query through the
// existing prompt overlay; on submit we run ripgrep across every
// workspace root, format the results into a scratch buffer named
// "Find Results", and switch to it.
//
// If the editor has a visual selection when the prompt opens, we seed
// the input with it — same UX as Ctrl+F's prefill.
func (m *Model) openFindInFilesPrompt() {
	initial := m.getNvimSelection()
	if initial == "" && m.onFindResultsPage() {
		initial = m.findRun.query // F8 on the page edits the last query
	}
	m.prompt = prompt.New("Find in Files", "Find:", initial)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindFindInFiles
}

func init() {
	// R on the Find Results page: Replace in Workspace with its query.
	registerNotifyHandler("termocode_find_replace", func(m *Model, args []any) tea.Cmd {
		m.openReplaceInWorkspacePromptWith(notifyArgString(args, 0))
		return nil
	})
	// A click on the Aa / ab / .* chip: search again with it flipped.
	registerNotifyHandler("termocode_find_toggle", func(m *Model, args []any) tea.Cmd {
		opts := m.findRun.opts
		switch notifyArgString(args, 0) {
		case "Aa":
			opts.CaseSensitive = !opts.CaseSensitive
		case "ab":
			opts.WholeWord = !opts.WholeWord
		case ".*":
			opts.Regex = !opts.Regex
		default:
			return nil
		}
		cmd := m.runFindInFilesWith(m.findRun.query, opts)
		m.findRun.reuse = true
		return cmd
	})
	// q / Esc on the page: close it like Ctrl+W does.
	registerNotifyHandler("termocode_find_close", func(m *Model, _ []any) tea.Cmd {
		if m.nvim == nil {
			return nil
		}
		_ = m.nvim.ExecLua(`local b = vim.g.termocode_find_results_buf
if b and vim.api.nvim_buf_is_valid(b) then pcall(vim.cmd, 'bdelete ' .. b) end`)
		return m.fetchStateCmd()
	})
}

// findResultsRun tracks the streamed search that feeds the Find Results
// buffer. A new search bumps gen and cancels the old one, so late batches
// from a superseded run are dropped in handleFindBatch.
type findResultsRun struct {
	gen    int
	cancel context.CancelFunc
	query  string
	opened bool     // the Find Results buffer exists for this run
	roots  []string // workspace folders searched, for display paths
	// reuse: a refresh after files changed. The results replace the text
	// of the open Find Results buffer in place; no window switches to it.
	reuse   bool
	opts    search.Options
	started time.Time
	st      findState
}

// isFindResultsPath says whether a buffer name is the Find Results page.
func isFindResultsPath(p string) bool {
	return p == "Find Results" || strings.HasSuffix(p, "/Find Results")
}

// onFindResultsPage says whether the editor shows the Find Results page.
func (m *Model) onFindResultsPage() bool {
	if m.nvim == nil {
		return false
	}
	ft, err := m.nvim.EvalLuaString(`return vim.bo.filetype`)
	return err == nil && ft == "findresults"
}

// findResultsBufLua says whether the Find Results buffer still exists.
const findResultsBufLua = `local b = vim.g.termocode_find_results_buf
return b ~= nil and vim.api.nvim_buf_is_valid(b)`

// refreshSearchResults searches again after files changed on disk (the
// agent wrote, renamed or deleted files, ran a command, switched
// branches…; the user renamed, deleted or made a file in the explorer), so the results never point at old lines or gone files:
// the Find Results buffer, when it is still open (shown or not), and the
// Find in Files overlay, when it is open.
func (m *Model) refreshSearchResults() tea.Cmd {
	var cmds []tea.Cmd
	if m.searchOpen {
		cmds = append(cmds, m.search.Refresh())
	}
	if m.nvim != nil && strings.TrimSpace(m.findRun.query) != "" {
		var open bool
		if err := m.nvim.ExecLuaResult(findResultsBufLua, &open); err == nil && open {
			cmds = append(cmds, m.runFindInFilesWith(m.findRun.query, m.findRun.opts))
			m.findRun.reuse = true
		}
	}
	return tea.Batch(cmds...)
}

// findBatchMsg carries one search.Batch back into Update. ch rides along so
// the handler can re-arm waitFindBatch without storing the channel.
type findBatchMsg struct {
	gen   int
	ch    <-chan search.Batch
	batch search.Batch
}

// waitFindBatch blocks (off the UI goroutine) for the next batch.
func waitFindBatch(gen int, ch <-chan search.Batch) tea.Cmd {
	return func() tea.Msg {
		b, ok := <-ch
		if !ok {
			b = search.Batch{Done: true}
		}
		return findBatchMsg{gen: gen, ch: ch, batch: b}
	}
}

// searchMaxResults resolves the `search_max_results` setting: missing or
// negative → search.DefaultMaxResults, 0 → unlimited.
func searchMaxResults(cfg settingsConfig) int {
	if cfg.SearchMaxResults == nil || *cfg.SearchMaxResults < 0 {
		return search.DefaultMaxResults
	}
	return *cfg.SearchMaxResults
}

// runFindInFiles is called by handlePromptSubmit when the user submits the
// Find-in-Files query. It starts a streamed search; results reach the
// "Find Results" buffer in batches (handleFindBatch) so the first hits show
// while the rest of the tree is still being searched, and the UI never
// blocks on a big workspace.
func (m *Model) runFindInFiles(query string) tea.Cmd {
	// Same matching as before the overlay grew toggles: smart-case, regex
	// through ripgrep, literal on the built-in walker.
	return m.runFindInFilesWith(query, search.Options{Regex: search.Available()})
}

// runFindInFilesWith runs Find in Files with the page's options (Aa, ab,
// .*); the result cap always comes from the settings.
func (m *Model) runFindInFilesWith(query string, opts search.Options) tea.Cmd {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	if m.findRun.cancel != nil {
		m.findRun.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	gen := m.findRun.gen + 1
	roots := workspaceRoots()
	opts.MaxResults = searchMaxResults(loadSettings())
	engine := "basic"
	if search.Available() {
		engine = "rg"
	}
	m.findRun = findResultsRun{gen: gen, cancel: cancel, query: query, roots: roots, opts: opts,
		started: time.Now(), st: findState{searching: true, engine: engine}}
	return waitFindBatch(gen, search.Stream(ctx, roots, q, opts))
}

// handleFindBatch paints one streamed batch. The first non-empty batch
// opens the page; later ones append and update the summary row; the final
// batch says how many matches and how long it took. With no results at all
// no page is opened — a toast says "No matches" (or shows the error).
func (m *Model) handleFindBatch(msg findBatchMsg) tea.Cmd {
	if msg.gen != m.findRun.gen {
		return nil // superseded run
	}
	run := &m.findRun
	b := msg.batch
	if !b.Done {
		lines, rows := buildFindRows(b.Results, run.roots)
		run.st.matches += len(b.Results)
		for _, r := range rows {
			if r["k"] == "h" {
				run.st.files++
			}
		}
		if !run.opened {
			run.opened = true
			if run.reuse {
				m.resetFindResults(m.findHead(), lines, rows)
			} else {
				m.openFindResultsBuffer(m.findHead(), lines, rows)
			}
		} else {
			m.appendFindResults(m.findHead(), lines, rows)
		}
		return waitFindBatch(msg.gen, msg.ch)
	}

	if run.cancel != nil {
		run.cancel()
		run.cancel = nil
	}
	run.st.searching = false
	run.st.sum = b.Summary
	run.st.elapsed = time.Since(run.started)
	if b.Err != nil {
		run.st.errText = firstLine(b.Err.Error())
	}
	var toastCmd tea.Cmd
	switch {
	case run.reuse && !run.opened && b.Err == nil:
		// The refresh found nothing: the old results must go.
		run.opened = true
		m.resetFindResults(m.findHead(), findEmptyLines(run.query), findEmptyRows())
	case run.opened:
		m.appendFindResults(m.findHead(), nil, nil)
		if b.Err != nil {
			m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Find failed", b.Err.Error())
		}
	case b.Err != nil:
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Find failed", b.Err.Error())
	default:
		m.toast, toastCmd = m.toast.Push(toast.Info, fmt.Sprintf("No matches for %q", strings.TrimSpace(run.query)))
	}
	return toastCmd
}

// findEmptyLines is the page body when a refresh finds nothing.
func findEmptyLines(query string) []string {
	return []string{"", fmt.Sprintf("      No matches for %q", strings.TrimSpace(query)),
		"      Edit the query with " + theme.KeyLabel("f8") + ", or check the Aa / .* options", ""}
}

func findEmptyRows() []findRow {
	return []findRow{{"k": ""}, {"k": "e", "hl": "TermocodeFindSummary"}, {"k": "e", "hl": "TermocodeFindMeta"}, {"k": ""}}
}

// findHead is what the page's top rows show: the query, the summary, the
// option chips, the engine and time, the hint bar and the glyphs the
// decoration provider draws (in the current icon mode).
func (m *Model) findHead() map[string]any {
	run := &m.findRun
	summary, hl := findSummaryLine(run.st)
	meta := run.st.engine
	// The time shows only for slow searches: "0.1 s" is noise (and
	// would make every screenshot differ).
	if !run.st.searching && run.st.elapsed >= time.Second {
		meta += fmt.Sprintf(" · %.1f s", run.st.elapsed.Seconds())
	}
	open, closed := "▾", "▸"
	if theme.CurrentIconMode == theme.IconModeASCII {
		open, closed = "v", ">"
	}
	hints := [][]string{}
	for _, h := range [][2]string{{"enter", "open"}, {"tab", "fold"}, {"R", "replace"}, {"f8", "edit"}, {"esc", "close"}} {
		hints = append(hints, []string{theme.KeyLabel(h[0]), h[1]})
	}
	return map[string]any{
		"query":      strings.TrimSpace(run.query),
		"summary":    summary,
		"summary_hl": hl,
		"meta":       meta,
		"chips": [][]any{
			{"Aa", run.opts.CaseSensitive},
			{"ab", run.opts.WholeWord},
			{".*", run.opts.Regex},
		},
		"hints": hints,
		"g":     map[string]string{"search": theme.IconSearch.String(), "open": open, "closed": closed},
	}
}

// openFindResultsBuffer opens the "Find Results" scratch buffer (named
// after a temp file so the session code knows to skip it) and fills it.
func (m *Model) openFindResultsBuffer(head map[string]any, lines []string, rows []findRow) {
	tmp := filepath.Join(os.TempDir(), "termocode-find-results.txt")
	if err := os.WriteFile(tmp, []byte("\n"), 0o644); err != nil {
		m.toast, _ = m.toast.PushDetail(toast.Errr, "Find failed", err.Error())
		return
	}
	if m.nvim == nil {
		return
	}
	// attachCmd loads the page's Lua; load it here when it is missing
	// (a session that attached without it, tests).
	if ok, err := m.nvim.EvalLuaString(`return _G._termocode_find_set and 'y' or 'n'`); err == nil && ok != "y" {
		_ = m.nvim.ExecLua(findResultsLua)
	}
	lua := fmt.Sprintf(`
local tmp = %q
vim.cmd('keepalt edit ' .. vim.fn.fnameescape(tmp))
local buf = vim.api.nvim_get_current_buf()
pcall(vim.api.nvim_buf_set_name, buf, vim.fn.fnamemodify(tmp, ':h') .. '/Find Results')
vim.bo[buf].buftype    = 'nofile'
vim.bo[buf].swapfile   = false
vim.bo[buf].buflisted  = true
vim.bo[buf].modifiable = false
-- Remember the buffer so F4 / Shift+F4 can navigate even when the user
-- is on a different file, and so later batches know where to append.
vim.g.termocode_find_results_buf = buf
-- Setting filetype LAST triggers the FileType autocmd (findResultsLua):
-- keys, mouse and the window look.
vim.bo[buf].filetype = 'findresults'
-- The "BufEnter * startinsert" autocmd fired before buftype was set;
-- without this the first Enter is eaten by insert mode.
vim.cmd('stopinsert')
`, tmp)
	_ = m.nvim.ExecLua(lua)
	m.resetFindResults(head, lines, rows)
	m.focus = FocusEditor
}

// resetFindResults replaces the whole page (a new search or a refresh),
// without switching any window to it.
func (m *Model) resetFindResults(head map[string]any, lines []string, rows []findRow) {
	if m.nvim == nil {
		return
	}
	if err := m.nvim.ExecLuaArgs(`local h, l, r = ...
if _G._termocode_find_set then _G._termocode_find_set(h, l, r) end`, head, nonNilLines(lines), nonNilRows(rows)); err != nil {
		recordError(fmt.Sprintf("find results: %v", err))
	}
}

// appendFindResults adds one batch and rewrites the summary row. A page
// the user already closed is skipped.
func (m *Model) appendFindResults(head map[string]any, lines []string, rows []findRow) {
	if m.nvim == nil {
		return
	}
	err := m.nvim.ExecLuaArgs(`local h, l, r = ...
if _G._termocode_find_append then _G._termocode_find_append(h, l, r) end`, head, nonNilLines(lines), nonNilRows(rows))
	if err != nil {
		recordError(fmt.Sprintf("find results: %v", err))
	}
}

// nonNilLines / nonNilRows send an empty Lua list instead of nil.
func nonNilLines(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

func nonNilRows(r []findRow) []findRow {
	if r == nil {
		return []findRow{}
	}
	return r
}

// findDisplayPath is how a result file is named in Find Results: relative
// to its workspace folder, and with several folders prefixed by the
// folder's name (VSCode style: "otherproj/lib/x.go"). A path outside every
// folder stays absolute.
func findDisplayPath(abs string, roots []string) string {
	best := ""
	for _, r := range roots {
		rel, err := filepath.Rel(r, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if len(r) > len(best) {
			best = r
		}
	}
	if best == "" {
		return abs
	}
	rel, _ := filepath.Rel(best, abs)
	if len(roots) > 1 {
		return filepath.Join(filepath.Base(best), rel)
	}
	return rel
}

// plurals returns "" for n==1, "s" otherwise — used for "files".
func plurals(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// matchPlural returns "" for n==1, "es" otherwise — used for "matches".
func matchPlural(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}

// findResultNav moves to the next (dir=1) or previous (dir=-1) Find in
// Files result via the Lua helper from findResultsLua (F4 / Shift+F4).
func (m *Model) findResultNav(dir int) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	res, err := m.nvim.EvalLuaString(fmt.Sprintf(
		`local f = _G._termocode_find_nav; if not f then return 'none' end; return f(%d) or 'none'`, dir))
	if err != nil {
		recordError(fmt.Sprintf("find result nav: %v", err))
		return nil
	}
	text := findNavToast(res, dir)
	if text == "" {
		return nil
	}
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, text)
	return c
}

// findNavToast is the toast text for a _termocode_find_nav result
// ("" = no toast).
func findNavToast(res string, dir int) string {
	switch res {
	case "none":
		return "No search results — run Find in Files (F8) first"
	case "end":
		if dir < 0 {
			return "No previous search result"
		}
		return "No more search results"
	}
	return ""
}
