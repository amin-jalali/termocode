package app

// gitSignsLua paints +/-/~ signs in the sign column to indicate added,
// deleted, and modified lines vs HEAD. Refreshes on save and on a 1-second
// debounce after CursorHold so it stays accurate without thrashing git on
// every keystroke.
//
// Implementation:
//
//   - For each buffer with a corresponding git tracked path, run
//     `git diff --no-color -U0 -- <file>` and parse the hunk headers to
//     learn which lines changed in which way.
//   - Use nvim's sign API (vim.fn.sign_define + sign_place) to render
//     three sign groups: GitAdd / GitChange / GitDelete in the
//     "TermocodeGit" namespace.
//   - Highlights match VSCode-ish colors (green / blue / red) and live in
//     the global highlight namespace so theme switches don't invalidate
//     them.
//
// Limitations: works for tracked files only — untracked files don't show
// any signs (expected; everything in them would be "+" anyway and the
// explorer's `U` letter already conveys that).
const gitSignsLua = `
-- One-time initialization: register sign glyphs and the highlight groups
-- they reference. We use truecolor hex values so light terminals don't
-- collapse the three states onto a single palette entry.
-- Colours come from the active theme (_G.termocode_colors, theme_lua.go).
do
  local C = _G.termocode_colors or {}
  vim.api.nvim_set_hl(0, 'TermocodeGitAdd',    { fg = C.git_unt })
  vim.api.nvim_set_hl(0, 'TermocodeGitChange', { fg = C.git_mod })
  vim.api.nvim_set_hl(0, 'TermocodeGitDelete', { fg = C.git_del })
end

vim.fn.sign_define('TermocodeGitAdd',    { text = '┃', texthl = 'TermocodeGitAdd' })
vim.fn.sign_define('TermocodeGitChange', { text = '┃', texthl = 'TermocodeGitChange' })
vim.fn.sign_define('TermocodeGitDelete', { text = '▁', texthl = 'TermocodeGitDelete' })

-- The editor's statuscolumn ('%=%l %{%v:lua.TcGitMark()%}', model.go) has
-- no sign column, so placed signs alone are never drawn. TcGitMark draws
-- the one-cell git mark of each line from this table:
-- bufnr -> { [lnum] = sign name }.
_G._tc_git_marks = _G._tc_git_marks or {}
local _tc_mark_text = { TermocodeGitAdd = '┃', TermocodeGitChange = '┃', TermocodeGitDelete = '▁' }
function _G.TcGitMark()
  if vim.v.virtnum ~= 0 then return ' ' end
  local marks = _G._tc_git_marks[vim.api.nvim_get_current_buf()]
  local name = marks and marks[vim.v.lnum]
  if not name then return ' ' end
  return '%#' .. name .. '#' .. _tc_mark_text[name] .. '%*'
end

-- Render: clears the buffer's sign group and re-paints from the diff hunks.
local function _termocode_git_signs(bufnr)
  bufnr = bufnr or vim.api.nvim_get_current_buf()
  _G._tc_git_marks[bufnr] = nil
  local file = vim.api.nvim_buf_get_name(bufnr)
  if file == '' then return end
  -- Only run inside git repos; checking the buffer's actual path lets us
  -- support files in nested git submodules without extra gymnastics.
  local git_dir = vim.fn.systemlist('git -C ' .. vim.fn.shellescape(vim.fn.fnamemodify(file, ':h')) .. ' rev-parse --show-toplevel')[1]
  if not git_dir or git_dir == '' or git_dir:match('fatal:') then return end
  -- Diff -U0 produces compact hunk headers we can parse with a small regex.
  -- Run against HEAD so both staged and unstaged changes show signs (we
  -- want to surface "what's different from the last commit").
  local diff = vim.fn.systemlist({ 'git', '-C', git_dir, 'diff', '--no-color', '-U0', 'HEAD', '--', file })
  vim.fn.sign_unplace('TermocodeGitSigns', { buffer = bufnr })
  local marks = {}
  _G._tc_git_marks[bufnr] = marks
  local function place(name, lnum)
    marks[lnum] = name
    vim.fn.sign_place(0, 'TermocodeGitSigns', name, bufnr, { lnum = lnum, priority = 5 })
  end
  -- (sign_place / sign_unplace also make nvim redraw the statuscolumn.)
  if not diff or #diff == 0 then return end
  for _, line in ipairs(diff) do
    -- Hunk header form: @@ -A,B +C,D @@ ...
    --   A,B = removed range, C,D = added range. Either ,B or ,D may be
    --   omitted (count of 1).
    local minus_n, plus_start, plus_n = line:match('^@@ %-(%d+)[,]?(%d*) %+(%d+)[,]?(%d*)')
    if not minus_n then
      -- Some matches have a different shape; pcall the simpler fallback.
      local ok, ms, ml, ps, pl = pcall(string.match, line, '^@@ %-(%d+),?(%d*) %+(%d+),?(%d*) @@')
      if not ok then goto continue end
      minus_n, _, plus_start, plus_n = ms, ml, ps, pl
    end
    -- The first capture from the first match was minus_n; we want the
    -- size though. Re-parse properly.
    local removed_start, removed_count, added_start, added_count =
      line:match('^@@ %-(%d+),?(%d*) %+(%d+),?(%d*) @@')
    if not removed_start then goto continue end
    removed_count = removed_count == '' and 1 or tonumber(removed_count)
    added_count   = added_count   == '' and 1 or tonumber(added_count)
    added_start   = tonumber(added_start)
    if added_count == 0 then
      -- Pure deletion: place a "deleted" sign on the line where the
      -- removed range USED to live (we anchor it at added_start so the
      -- glyph appears just below the gap).
      place('TermocodeGitDelete', math.max(added_start, 1))
    elseif removed_count == 0 then
      -- Pure addition.
      for i = 0, added_count - 1 do
        place('TermocodeGitAdd', added_start + i)
      end
    else
      -- Modification (changed lines).
      for i = 0, added_count - 1 do
        place('TermocodeGitChange', added_start + i)
      end
    end
    ::continue::
  end
end

-- Throttle: refresh on BufWritePost (immediate, after each save) and on
-- CursorHold (after 'updatetime' = 1s of idle), but never more than once
-- every 500 ms. An event inside the window is not dropped: it schedules
-- one refresh at the window's end (a file opened right after another
-- buffer event would otherwise get no signs until the next idle).
local _git_signs_last, _git_signs_pending = 0, false
local function _git_signs_maybe()
  local now = (vim.uv or vim.loop).now()
  local wait = 500 - (now - _git_signs_last)
  if wait <= 0 then
    _git_signs_last = now
    pcall(_termocode_git_signs)
    return
  end
  if _git_signs_pending then return end
  _git_signs_pending = true
  vim.defer_fn(function()
    _git_signs_pending = false
    _git_signs_last = (vim.uv or vim.loop).now()
    pcall(_termocode_git_signs)
  end, wait)
end

-- Called from Go when the git state changed (commit, stage, checkout,
-- pull …): HEAD moved, so the signs of every shown file may be stale
-- although no buffer event fired.
function _G.termocode_git_signs_refresh()
  local seen = {}
  for _, w in ipairs(vim.api.nvim_list_wins()) do
    local b = vim.api.nvim_win_get_buf(w)
    if not seen[b] and vim.bo[b].buftype == '' then
      seen[b] = true
      pcall(_termocode_git_signs, b)
    end
  end
end

vim.api.nvim_create_augroup('TermocodeGitSigns', { clear = true })
vim.api.nvim_create_autocmd({ 'BufWritePost', 'BufReadPost', 'BufEnter', 'CursorHold' }, {
  group = 'TermocodeGitSigns',
  callback = _git_signs_maybe,
})
`
