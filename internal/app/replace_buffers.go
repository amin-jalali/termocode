package app

import (
	"strconv"

	"github.com/amin-jalali/termocode/internal/search"
)

// Replace in Workspace and files with unsaved edits (SCN-19).
//
// Writing such a file on disk would leave the editor's text as it was
// (the reload keeps unsaved edits), so the replace would be lost on the
// next save, and there would be no undo. Instead the match is replaced in
// the editor buffer: the user's edit stays, the tab stays modified, the
// disk copy is untouched, and Ctrl+Z undoes the replace in one step.

// replaceBufferLua counts (apply = 0) or replaces (apply = 1) the literal
// find in the loaded, modified buffer of path. It returns the count, or
// "-1" when path has no modified buffer (then the file on disk is used).
// The changed lines are set in one undo block; setting 'undolevels' to
// itself closes the block before and after, so an insert in progress is
// not joined with the replace.
const replaceBufferLua = `
local path, find, repl, apply = ...
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_is_loaded(b) and vim.api.nvim_buf_get_name(b) == path and vim.bo[b].modified then
    local lines = vim.api.nvim_buf_get_lines(b, 0, -1, false)
    local n, changed = 0, {}
    for i, l in ipairs(lines) do
      local out, k, s = {}, 0, 1
      while true do
        local a, e = string.find(l, find, s, true)
        if not a then break end
        out[#out + 1] = l:sub(s, a - 1)
        out[#out + 1] = repl
        s, k = e + 1, k + 1
      end
      if k > 0 then
        out[#out + 1] = l:sub(s)
        changed[i] = table.concat(out)
        n = n + k
      end
    end
    if apply == 1 and n > 0 then
      vim.api.nvim_buf_call(b, function()
        vim.cmd('let &undolevels = &undolevels')
        for i, l in pairs(changed) do
          vim.api.nvim_buf_set_lines(b, i - 1, i, false, { l })
        end
        vim.cmd('let &undolevels = &undolevels')
      end)
    end
    return tostring(n)
  end
end
return '-1'
`

// bufferReplace counts (apply false) or replaces find in path's modified
// buffer. dirty is false when path has no modified buffer.
func (m *Model) bufferReplace(path, find, repl string, apply bool) (n int, dirty bool, err error) {
	if m.nvim == nil || find == "" {
		return 0, false, nil
	}
	a := 0
	if apply {
		a = 1
	}
	s, err := m.nvim.EvalLuaStringArgs(replaceBufferLua, path, find, repl, a)
	if err != nil {
		return 0, false, err
	}
	n, err = strconv.Atoi(s)
	if err != nil {
		return 0, false, err
	}
	if n < 0 {
		return 0, false, nil
	}
	return n, true, nil
}

// modifiedPaths is the set of open files with unsaved edits.
func (m *Model) modifiedPaths() map[string]bool {
	out := map[string]bool{}
	for _, b := range m.bufs {
		if b.Modified && b.Path != "" {
			out[b.Path] = true
		}
	}
	return out
}

// withBufferCounts puts the editor's unsaved text in place of the disk
// copy for files with unsaved edits: their count comes from the buffer (a
// match only on disk is not counted, a match only in the unsaved text
// is), so the dialog's numbers are what gets replaced. roots bound the
// open files that are added.
func (m *Model) withBufferCounts(counts []search.FileCount, roots []string, find string) []search.FileCount {
	dirty := m.modifiedPaths()
	if len(dirty) == 0 {
		return counts
	}
	seen := map[string]bool{}
	out := make([]search.FileCount, 0, len(counts))
	for _, c := range counts {
		seen[c.Path] = true
		if dirty[c.Path] {
			if n, ok, e := m.bufferReplace(c.Path, find, "", false); e == nil && ok {
				if n > 0 {
					out = append(out, search.FileCount{Path: c.Path, Count: n})
				}
				continue
			}
		}
		out = append(out, c)
	}
	for _, b := range m.bufs {
		if !dirty[b.Path] || seen[b.Path] || !underAny(b.Path, roots) {
			continue
		}
		seen[b.Path] = true
		if n, ok, e := m.bufferReplace(b.Path, find, "", false); e == nil && ok && n > 0 {
			out = append(out, search.FileCount{Path: b.Path, Count: n})
		}
	}
	return out
}
