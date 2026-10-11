package app

// hotExitLua turns on the live hot-exit backup (hot_exit.go). Arg: the
// backup dir of this process. A short timer after the last change writes
// every modified, named file buffer to <dir>/<hash>.bak (path on the first
// line, then the lines; written to a temp file and renamed, so a crash
// mid-write never leaves half a backup) and removes the files of buffers
// that are saved or closed. _G.termocode_hot_exit_flush writes at once.
const hotExitLua = `
local dir = ...
local uv = vim.uv or vim.loop
local timer

local function flush()
  if timer then timer:stop() end
  local keep = {}
  local made = vim.fn.isdirectory(dir) == 1
  for _, b in ipairs(vim.api.nvim_list_bufs()) do
    if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buflisted and vim.bo[b].buftype == ''
        and vim.bo[b].modified then
      local path = vim.api.nvim_buf_get_name(b)
      if path ~= '' then
        if not made then
          vim.fn.mkdir(dir, 'p', 448) -- 0700
          made = true
        end
        local name = vim.fn.sha256(path):sub(1, 16) .. '.bak'
        keep[name] = true
        local lines = vim.api.nvim_buf_get_lines(b, 0, -1, false)
        table.insert(lines, 1, path)
        local tmp = dir .. '/' .. name .. '.tmp'
        if vim.fn.writefile(lines, tmp, 'b') == 0 then
          os.rename(tmp, dir .. '/' .. name)
        end
      end
    end
  end
  if made then
    for _, f in ipairs(vim.fn.readdir(dir)) do
      if not keep[f] then os.remove(dir .. '/' .. f) end
    end
  end
end

local function later()
  if not timer then timer = uv.new_timer() end
  timer:stop()
  timer:start(400, 0, vim.schedule_wrap(flush))
end

local g = vim.api.nvim_create_augroup('termocode_hot_exit', { clear = true })
vim.api.nvim_create_autocmd({ 'TextChanged', 'TextChangedI', 'TextChangedP', 'BufWritePost',
  'BufDelete', 'BufWipeout', 'BufModifiedSet', 'BufFilePost' }, { group = g, callback = later })
_G.termocode_hot_exit_flush = flush
`

// hotExitRestoreLua opens each backed-up file and puts the saved text in
// its buffer (modified, so the user reviews and saves). Args: paths and
// their lines. The first file ends up in front.
const hotExitRestoreLua = `
local paths, contents = ...
for i = #paths, 1, -1 do
  local p = paths[i]
  pcall(vim.cmd, 'silent! edit ' .. vim.fn.fnameescape(p))
  local b = vim.fn.bufnr(p)
  if b > 0 then
    vim.bo[b].buflisted = true
    vim.api.nvim_buf_set_lines(b, 0, -1, false, contents[i])
  end
end
`
