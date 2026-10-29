package app

// persistentUndoLua turns on persistent undo: per-file undo history in
// stdpath('data')/termocode-undo, so Ctrl+Z keeps working across
// sessions. Best-effort: if the directory can't be created, undo just
// stays in memory.
const persistentUndoLua = `
local undodir = vim.fn.stdpath('data') .. '/termocode-undo'
pcall(vim.fn.mkdir, undodir, 'p')
vim.opt.undodir = undodir
vim.opt.undofile = true
`
