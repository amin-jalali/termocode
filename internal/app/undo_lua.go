package app

// persistentUndoLua turns on persistent undo: per-file undo history in
// stdpath('data')/termocode-undo, so Ctrl+Z keeps working across
// sessions. Best-effort: if the directory can't be created, undo just
// stays in memory.
//
// With 'undofile' on, nvim hashes the whole file while reading it to
// check it against its undo file (10 ms for a 50k-line file), even when
// there is no undo file. So a file with no undo file is read with
// 'undofile' off for its buffer, and the option comes back on just
// before the first save (BufWritePre), which then writes the history as
// before. Setting it there is cheap: nvim only hashes and reads an undo
// file for an unchanged buffer.
const persistentUndoLua = `
local undodir = vim.fn.stdpath('data') .. '/termocode-undo'
pcall(vim.fn.mkdir, undodir, 'p')
vim.opt.undodir = undodir
vim.opt.undofile = true
local g = vim.api.nvim_create_augroup('TermocodeUndoLazy', { clear = true })
vim.api.nvim_create_autocmd('BufReadPre', { group = g, callback = function(ev)
  local b = ev.buf
  if not vim.bo[b].undofile then return end
  local u = vim.fn.undofile(vim.api.nvim_buf_get_name(b))
  if u == '' or vim.uv.fs_stat(u) then return end
  vim.bo[b].undofile = false
  vim.b[b].termocode_undo_lazy = true
end })
vim.api.nvim_create_autocmd('BufWritePre', { group = g, callback = function(ev)
  local b = ev.buf
  if not vim.b[b].termocode_undo_lazy then return end
  vim.b[b].termocode_undo_lazy = nil
  vim.bo[b].undofile = vim.go.undofile
end })
`
