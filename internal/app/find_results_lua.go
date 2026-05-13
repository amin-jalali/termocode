package app

// findResultsLua wires up the scratch "Find Results" buffer:
//
//   - syntax highlighting per Sublime convention (path headers,
//     line numbers, footer summary)
//   - buffer-local <CR> / <2-LeftMouse>  → open the file at the cursor's
//     result row
//   - global F4 / Shift+F4 → next / previous match (works from anywhere
//     in the editor, not just inside the results buffer)
//
// Loaded once during attach (see model.go::attachCmd) and triggers on
// the FileType autocmd whenever a buffer announces filetype=findresults.
const findResultsLua = `
-- Syntax — applied per-buffer via FileType autocmd below.
vim.api.nvim_create_augroup('TermocodeFindResults', { clear = true })

vim.api.nvim_create_autocmd('FileType', {
  group = 'TermocodeFindResults',
  pattern = 'findresults',
  callback = function(args)
    local buf = args.buf

    -- Syntax. Colours pulled from the editor's own VSCode Dark+ palette
    -- so the results buffer feels like a sibling of the main editor.
    vim.cmd([[
      syntax clear
      " Path header (with optional " (N)" hit count). The trailing
      " count is captured in its own group so we can paint it dimmer.
      syntax match findResultsHeaderCount /\s(\d\+):$/ contained
      syntax match findResultsHeader      /^[^ \t].*:$/  contains=findResultsHeaderCount
      " Line-number column (4-space indent + line number).
      syntax match findResultsLineNum     /^\s\+\d\+[: ] /
      " Footer summary + the initial "Searching N files for ..." line.
      syntax match findResultsFooter      /^\d\+ match\(es\)\? \(across\|for\) .*$/
      syntax match findResultsFooter      /^Searching \d\+ files\? for ".*"$/

      " File path header: teal (editor "type" colour), bold.
      highlight default findResultsHeader      guifg=#4ec9b0 gui=bold
      " The "(N)" hit count uses the dim header grey — readable but
      " visibly secondary to the path.
      highlight default findResultsHeaderCount guifg=#6c7080 gui=italic
      " Line numbers: gutter grey.
      highlight default findResultsLineNum     guifg=#858585
      " Footer / searching line: comment green italic.
      highlight default findResultsFooter      guifg=#6a9955 gui=italic
      " Match highlight: subtle dark-green background ONLY. Foreground
      " stays at the default content colour so the match reads as
      " "content with a tint" rather than "stand-out coloured token".
      highlight default findResultsMatch       guibg=#2a3f24
      " Active line in the results buffer — one shade lighter than the
      " editor bg so the cursor row is unambiguous without being loud.
      highlight default CursorLine             guibg=#23262e
    ]])

    -- Per-search query highlight: every literal occurrence of the
    -- query gets the match bg via a syntax rule.
    local q = vim.g.termocode_find_query or ''
    if q ~= '' then
      local escaped = q:gsub('\\', '\\\\'):gsub('/', '\\/')
      vim.cmd(string.format(
        [[syntax match findResultsMatch /\V%s/ contained containedin=ALL]], escaped))
    end

    -- Buffer-local options that make the view feel like a results pane.
    vim.bo[buf].swapfile  = false
    vim.bo[buf].buflisted = true
    vim.wo.cursorline     = true
    vim.wo.wrap           = false

    -- <CR> on a result line: parse "    NNN: ..." or "    NNN  ..." for
    -- the line number, then scan upward for the nearest "/path:" header.
    -- On a header itself, just :edit the file from line 1.
    -- strip_count removes the trailing " (N)" hit-count suffix the
    -- formatter appends to header lines (e.g. "/abs/path (3):" → "/abs/path").
    -- Without this, :edit tried to open the literal "/abs/path (3)" string
    -- which doesn't exist, opening an empty buffer.
    local function strip_count(p)
      return (p:gsub(' %(%d+%)$', ''))
    end
    local function jump_from_cursor()
      local row = vim.api.nvim_win_get_cursor(0)[1]
      local cur = vim.api.nvim_buf_get_lines(0, row-1, row, false)[1] or ''
      -- Header line?
      local p = cur:match('^([^ \t].+):$')
      if p then
        vim.cmd('edit ' .. vim.fn.fnameescape(strip_count(p)))
        return
      end
      -- Result / context line?
      local lineno = cur:match('^%s+(%d+)[: ] ')
      if not lineno then return end
      local file
      for r = row-1, 1, -1 do
        local s = vim.api.nvim_buf_get_lines(0, r-1, r, false)[1] or ''
        local hp = s:match('^([^ \t].+):$')
        if hp then file = strip_count(hp); break end
      end
      if file then
        vim.cmd('edit +' .. lineno .. ' ' .. vim.fn.fnameescape(file))
      end
    end

    vim.keymap.set('n', '<CR>', jump_from_cursor, { buffer = buf, silent = true })

    -- Mouse handling. nvim's default mouse=a behaviour is:
    --   <LeftMouse>   = position cursor + start visual selection
    --   <LeftDrag>    = extend the selection
    --   <LeftRelease> = finalise it
    -- That makes a single click in a results buffer feel like the
    -- cursor "stuck on" mid-drag — the user reported this as
    -- "click stays held down and turns into selection mode". We
    -- override every mouse event so a click is just "position +
    -- jump" with NO visual / drag side effects.
    local function mouse_jump()
      local pos = vim.fn.getmousepos()
      if pos.line > 0 then
        local col = pos.column - 1
        if col < 0 then col = 0 end
        pcall(vim.api.nvim_win_set_cursor, 0, { pos.line, col })
      end
      jump_from_cursor()
    end
    vim.keymap.set('n', '<LeftMouse>',    mouse_jump, { buffer = buf, silent = true })
    vim.keymap.set('n', '<2-LeftMouse>',  mouse_jump, { buffer = buf, silent = true })
    vim.keymap.set('n', '<LeftDrag>',     '<Nop>',    { buffer = buf, silent = true })
    vim.keymap.set('n', '<LeftRelease>',  '<Nop>',    { buffer = buf, silent = true })

    -- Force normal mode every time the user enters this buffer. The
    -- global "BufEnter * if &buftype=='' | startinsert" autocmd fires
    -- BEFORE we (re-)set buftype on a freshly-created buffer, so a
    -- second visit to the results tab can land in insert mode on a
    -- non-modifiable buffer — the "E21 modifiable is off" the user
    -- reported. A buffer-local BufEnter / WinEnter that calls
    -- stopinsert undoes that.
    vim.api.nvim_create_autocmd({ 'BufEnter', 'WinEnter' }, {
      buffer = buf,
      callback = function() vim.cmd('stopinsert') end,
    })
  end,
})

-- Global F4 / Shift+F4 navigation. Walks the saved Find Results buffer
-- looking for the next / previous match line (those have ":" between
-- the line number and the content; context lines have " "). Opens the
-- corresponding file as if the user pressed <CR> on the row, AND moves
-- the cursor in the results buffer so the user sees what they jumped
-- from.
local function navigate_results(direction)
  local buf = vim.g.termocode_find_results_buf
  if not buf or not vim.api.nvim_buf_is_valid(buf) then return end
  local total = vim.api.nvim_buf_line_count(buf)
  if total == 0 then return end

  -- Find the row currently selected in the results window (if any). If
  -- the results buffer isn't visible we start from the top.
  local cur_row = 1
  for _, win in ipairs(vim.api.nvim_list_wins()) do
    if vim.api.nvim_win_get_buf(win) == buf then
      cur_row = vim.api.nvim_win_get_cursor(win)[1]
      break
    end
  end

  local step = direction
  local r = cur_row + step
  while r >= 1 and r <= total do
    local line = vim.api.nvim_buf_get_lines(buf, r-1, r, false)[1] or ''
    if line:match('^%s+%d+:') then
      -- Found a match row. Walk back to the nearest header and edit.
      local lineno = line:match('^%s+(%d+):')
      local file
      for s = r-1, 1, -1 do
        local sline = vim.api.nvim_buf_get_lines(buf, s-1, s, false)[1] or ''
        local p = sline:match('^([^ \t].+):$')
        if p then file = p; break end
      end
      if file then
        -- Move the cursor in the results buffer (best-effort visual
        -- feedback). Keep results window visible if any.
        for _, win in ipairs(vim.api.nvim_list_wins()) do
          if vim.api.nvim_win_get_buf(win) == buf then
            vim.api.nvim_win_set_cursor(win, { r, 0 })
            break
          end
        end
        vim.cmd('edit +' .. lineno .. ' ' .. vim.fn.fnameescape(file))
      end
      return
    end
    r = r + step
  end
end

vim.keymap.set('n', '<F4>',     function() navigate_results( 1) end, { silent = true })
vim.keymap.set('n', '<S-F4>',   function() navigate_results(-1) end, { silent = true })
vim.keymap.set('i', '<F4>',     function() navigate_results( 1) end, { silent = true })
vim.keymap.set('i', '<S-F4>',   function() navigate_results(-1) end, { silent = true })
`
