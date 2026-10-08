package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/search"
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
	m.prompt = prompt.New("Find in Files", "Find:", initial)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindFindInFiles
}

// findResultsRun tracks the streamed search that feeds the Find Results
// buffer. A new search bumps gen and cancels the old one, so late batches
// from a superseded run are dropped in handleFindBatch.
type findResultsRun struct {
	gen    int
	cancel context.CancelFunc
	query  string
	opened bool // the Find Results buffer exists for this run
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
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	if m.findRun.cancel != nil {
		m.findRun.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	gen := m.findRun.gen + 1
	m.findRun = findResultsRun{gen: gen, cancel: cancel, query: query}
	// Same matching as before the overlay grew toggles: smart-case, regex
	// through ripgrep, literal on the built-in walker.
	opts := search.Options{
		Regex:      search.Available(),
		MaxResults: searchMaxResults(loadSettings()),
	}
	return waitFindBatch(gen, search.Stream(ctx, workspaceRoots(), q, opts))
}

// handleFindBatch paints one streamed batch. The first non-empty batch
// opens the buffer; later ones append; the final batch rewrites the
// header and adds the footer. With no results at all no buffer is opened —
// a toast says "No matches" (or shows the error) instead.
func (m *Model) handleFindBatch(msg findBatchMsg) tea.Cmd {
	if msg.gen != m.findRun.gen {
		return nil // superseded run
	}
	run := &m.findRun
	b := msg.batch
	if !b.Done {
		lines := formatFindGroups(b.Results)
		if !run.opened {
			run.opened = true
			m.openFindResultsBuffer(run.query, append([]string{
				fmt.Sprintf("Searching for %q…", run.query), "",
			}, lines...))
		} else {
			m.appendFindResults("", lines)
		}
		return waitFindBatch(msg.gen, msg.ch)
	}

	if run.cancel != nil {
		run.cancel()
		run.cancel = nil
	}
	var toastCmd tea.Cmd
	switch {
	case run.opened:
		header := fmt.Sprintf("Searching %d file%s for %q", b.Summary.Files, plurals(b.Summary.Files), run.query)
		m.appendFindResults(header, []string{findResultsFooter(b.Summary)})
	case b.Err != nil:
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Find failed", b.Err.Error())
	default:
		m.toast, toastCmd = m.toast.Push(toast.Info, fmt.Sprintf("No matches for %q", strings.TrimSpace(run.query)))
	}
	return toastCmd
}

// openFindResultsBuffer writes the first lines to a temp file and opens it
// in nvim as the "Find Results" scratch buffer.
func (m *Model) openFindResultsBuffer(query string, lines []string) {
	tmp := filepath.Join(os.TempDir(), "termocode-find-results.txt")
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		m.toast, _ = m.toast.PushDetail(toast.Errr, "Find failed", err.Error())
		return
	}

	// Open the scratch file in nvim. The FileType=findresults autocmd
	// (registered once at startup, see findResultsLua) picks it up and
	// installs the syntax + buffer-local mappings.
	if m.nvim == nil {
		return
	}
	lua := fmt.Sprintf(`
local tmp   = %q
local query = %q
-- Stash the query in a global so the FileType autocmd can build a
-- per-buffer syntax match for it. Re-set on every search so changing
-- the query re-paints the highlight.
vim.g.termocode_find_query = query
vim.cmd('keepalt edit ' .. vim.fn.fnameescape(tmp))
local buf = vim.api.nvim_get_current_buf()
pcall(vim.api.nvim_buf_set_name, buf, vim.fn.fnamemodify(tmp, ':h') .. '/Find Results')
vim.bo[buf].buftype    = 'nofile'
vim.bo[buf].swapfile   = false
vim.bo[buf].buflisted  = true
vim.bo[buf].modifiable = false
-- Setting filetype LAST triggers the FileType autocmd which paints the
-- buffer + binds <CR> / <2-LeftMouse> / <LeftRelease>.
vim.bo[buf].filetype = 'findresults'
-- Force normal mode immediately. The "BufEnter * if &buftype=='' |
-- startinsert" autocmd fires BEFORE we set buftype=nofile, so the user
-- lands in insert mode on a non-modifiable buffer — the first <Enter>
-- they hit is consumed by an attempted (and rejected) newline insert,
-- and only the second <Enter> reaches our keymap. stopinsert fixes it.
vim.cmd('stopinsert')
-- Remember the buffer so F4 / Shift+F4 can navigate even when the user
-- is on a different file, and so later batches know where to append.
vim.g.termocode_find_results_buf = buf
`, tmp, query)
	_ = m.nvim.ExecLua(lua)
	m.focus = FocusEditor
}

// appendFindResults appends lines to the Find Results buffer and, when
// header is non-empty, replaces its first line. The text goes through a
// temp file (read back with readfile) so no Lua string escaping is needed
// for arbitrary file content. A buffer the user already closed is skipped.
func (m *Model) appendFindResults(header string, lines []string) {
	if m.nvim == nil {
		return
	}
	tmp := filepath.Join(os.TempDir(), "termocode-find-results-batch.txt")
	body := append([]string{header}, lines...)
	if err := os.WriteFile(tmp, []byte(strings.Join(body, "\n")+"\n"), 0o644); err != nil {
		return
	}
	_ = m.nvim.ExecLua(fmt.Sprintf(`
local buf = vim.g.termocode_find_results_buf
if not buf or not vim.api.nvim_buf_is_valid(buf) then return end
local lines = vim.fn.readfile(%q)
local head = table.remove(lines, 1)
vim.bo[buf].modifiable = true
if head ~= nil and head ~= '' then
  vim.api.nvim_buf_set_lines(buf, 0, 1, false, { head })
end
if #lines > 0 then
  vim.api.nvim_buf_set_lines(buf, -1, -1, false, lines)
end
vim.bo[buf].modifiable = false
`, tmp))
}

// findResultsFooter is the last line of the buffer. When the
// search_max_results cap dropped hits it says so: "Showing N of M …".
func findResultsFooter(s search.Summary) string {
	if s.Truncated() {
		return fmt.Sprintf("Showing %d of %d matches across %d file%s (search_max_results)",
			s.Shown, s.Total, s.Files, plurals(s.Files))
	}
	return fmt.Sprintf("%d match%s across %d file%s",
		s.Shown, matchPlural(s.Shown), s.Files, plurals(s.Files))
}

// formatFindGroups renders results as Sublime-style file groups, one
// "path (N):" header per file followed by its rows and a blank line. A
// streamed batch always holds whole files, so groups never split.
func formatFindGroups(results []search.Result) []string {
	// Group by Path while preserving first-seen order.
	type bucket struct {
		path string
		hits []search.Result
	}
	order := []*bucket{}
	byPath := map[string]*bucket{}
	for _, r := range results {
		b, ok := byPath[r.Path]
		if !ok {
			b = &bucket{path: r.Path}
			byPath[r.Path] = b
			order = append(order, b)
		}
		b.hits = append(b.hits, r)
	}

	const maxPreview = 250
	var out []string
	for _, bk := range order {
		sort.SliceStable(bk.hits, func(i, j int) bool { return bk.hits[i].Line < bk.hits[j].Line })
		// Header: "/abs/path (N):" — N hits in this file. The (N) is
		// rendered in the dim header colour (see syntax in find_results_lua.go),
		// giving the user a per-file count at a glance.
		out = append(out, fmt.Sprintf("%s (%d):", bk.path, len(bk.hits)))
		for _, h := range bk.hits {
			content := strings.TrimRight(h.Preview, "\n")
			if len([]rune(content)) > maxPreview {
				r := []rune(content)
				content = string(r[:maxPreview]) + "…"
			}
			// "    NNN:    <content>" — 4-space leading indent, line
			// number right-padded to 4, ":" sigil for match rows,
			// 4-space gap before content.
			out = append(out, fmt.Sprintf("    %4d:    %s", h.Line, content))
		}
		out = append(out, "")
	}
	return out
}

// formatFindResults builds the Sublime-style result body:
//
//	Searching N files for "query"
//
//	/abs/path/to/file.go:
//	      42:    matched line content
//	      45:    another match
//
//	/abs/path/to/other.go:
//	      13:    yet another match
//
//	M matches across N files
//
// Match rows use ":" between the line number and the content;
// context rows (not yet emitted by the caller, but the format
// supports them) would use " " — that's the convention the click
// handler relies on to tell the two apart.
//
// Long preview lines are truncated to 250 columns with a "…" suffix
// so a single minified JavaScript file can't blow the buffer up.
func formatFindResults(query string, results []search.Result) string {
	if len(results) == 0 {
		return fmt.Sprintf("0 matches for %q\n", query)
	}
	files := map[string]bool{}
	for _, r := range results {
		files[r.Path] = true
	}
	sum := search.Summary{Shown: len(results), Total: len(results), Files: len(files)}
	lines := []string{fmt.Sprintf("Searching %d file%s for %q", sum.Files, plurals(sum.Files), query), ""}
	lines = append(lines, formatFindGroups(results)...)
	lines = append(lines, findResultsFooter(sum))
	return strings.Join(lines, "\n") + "\n"
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
