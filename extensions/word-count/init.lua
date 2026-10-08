-- word-count — shows the word count of the current file in the status bar.
-- Click the item (or run "Ext: Word Count Details") for words / lines /
-- characters, and the selection count when there is one.
local tc = termocode

local cache = { buf = -1, tick = -1, words = 0 }

-- The current buffer of the editor window (not a panel or terminal).
local function editor_buf()
  local buf = vim.api.nvim_get_current_buf()
  if vim.bo[buf].buftype ~= '' then return nil end
  return buf
end

local function count_words(buf)
  local tick = vim.api.nvim_buf_get_changedtick(buf)
  if cache.buf == buf and cache.tick == tick then return cache.words end
  -- Very large files: skip (counting runs in the UI heartbeat budget).
  if vim.api.nvim_buf_line_count(buf) > 20000 then return nil end
  local n = 0
  for _, line in ipairs(vim.api.nvim_buf_get_lines(buf, 0, -1, false)) do
    for _ in line:gmatch('%S+') do n = n + 1 end
  end
  cache = { buf = buf, tick = tick, words = n }
  return n
end

tc.register_command({
  id = 'details',
  title = 'Word Count Details',
  run = function()
    local buf = editor_buf()
    if not buf then
      tc.notify('Word count: no file buffer', 'warn')
      return
    end
    local wc = vim.fn.wordcount()
    local detail = string.format('%d words · %d lines · %d characters',
      wc.words or 0, vim.api.nvim_buf_line_count(buf), wc.chars or 0)
    if wc.visual_words then
      detail = detail .. string.format(' · selection: %d words', wc.visual_words)
    end
    tc.notify('Word count', 'info', detail)
  end,
})

tc.register_status_item({
  id = 'words',
  command = 'details',
  text = function()
    local buf = editor_buf()
    if not buf or vim.api.nvim_buf_get_name(buf) == '' then return '' end
    local n = count_words(buf)
    if not n then return '' end
    return '{{muted}}' .. n .. (n == 1 and ' word' or ' words') .. '{{/}}'
  end,
})
