package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/prompt"
	"termocode/internal/search"
	"termocode/internal/toast"
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

// runFindInFiles is called by handlePromptSubmit when the user
// submits the Find-in-Files query. It runs the search, builds the
// formatted body, and opens it in nvim as a scratch buffer.
func (m *Model) runFindInFiles(query string) tea.Cmd {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	roots := workspaceRoots()
	results, err := search.RunDirs(roots, q)
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Find failed", err.Error())
		return toastCmd
	}
	if len(results) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, fmt.Sprintf("No matches for %q", q))
		return toastCmd
	}

	body := formatFindResults(query, results)
	tmp := filepath.Join(os.TempDir(), "termocode-find-results.txt")
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Find failed", err.Error())
		return toastCmd
	}

	// Open the scratch file in nvim. The FileType=findresults autocmd
	// (registered once at startup, see findResultsLua) picks it up and
	// installs the syntax + buffer-local mappings.
	if m.nvim == nil {
		return nil
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
-- is on a different file.
vim.g.termocode_find_results_buf = buf
`, tmp, query)
	_ = m.nvim.ExecLua(lua)
	m.focus = FocusEditor
	return nil
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
	for _, b := range order {
		sort.SliceStable(b.hits, func(i, j int) bool { return b.hits[i].Line < b.hits[j].Line })
	}

	const maxPreview = 250
	var b strings.Builder
	totalFiles := len(order)
	totalMatches := 0
	for _, bk := range order {
		totalMatches += len(bk.hits)
	}
	if totalMatches == 0 {
		fmt.Fprintf(&b, "0 matches for %q\n", query)
		return b.String()
	}
	fmt.Fprintf(&b, "Searching %d file%s for %q\n\n", totalFiles, plurals(totalFiles), query)
	for _, bk := range order {
		// Header: "/abs/path (N):" — N hits in this file. The (N) is
		// rendered in the dim header colour (see syntax in find_results_lua.go),
		// giving the user a per-file count at a glance.
		fmt.Fprintf(&b, "%s (%d):\n", bk.path, len(bk.hits))
		for _, h := range bk.hits {
			content := strings.TrimRight(h.Preview, "\n")
			if len([]rune(content)) > maxPreview {
				r := []rune(content)
				content = string(r[:maxPreview]) + "…"
			}
			// "    NNN:    <content>" — 4-space leading indent, line
			// number right-padded to 4, ":" sigil for match rows,
			// 4-space gap before content.
			fmt.Fprintf(&b, "    %4d:    %s\n", h.Line, content)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "%d match%s across %d file%s\n",
		totalMatches, matchPlural(totalMatches), totalFiles, plurals(totalFiles))
	return b.String()
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
