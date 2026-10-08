# ADR 0001 — Embedded `nvim --embed` as the editor engine

Status: **Accepted** (2026-05-13, backfilled)

## Context
termocode wants a VSCode-like IDE in the terminal. The hard part of an editor is
not the chrome — it is buffers, undo, motions, syntax, LSP, DAP and the long tail
of text-editing edge cases. Options:

1. Write the text engine in Go (e.g. on top of a Bubble Tea textarea).
2. Embed an existing editor and draw our own chrome around it.
3. Be a Neovim config / plugin (LazyVim style) instead of a separate app.

Option 1 is years of work and would still lack LSP/DAP/Tree-sitter. Option 3
cannot give us full control over the frame (tabs, sidebar, overlays, mouse).

## Decision
Spawn a real Neovim as a child process (`nvim --embed -i NONE -n`) and talk to it
over msgpack-RPC with `neovim/go-client` (`internal/nvim/client.go`). We attach
as a UI with `ext_linegrid` + `rgb`, mirror the grid into `internal/grid`, and
render the cells ourselves in `internal/editor`. Everything around the editor —
tabs, explorer, Source Control, status bar, pickers, overlays — is Go / Bubble Tea.

- `-i NONE` turns off ShaDa so we never touch the user's nvim history / marks.
- `-n` turns off swap files; termocode owns the buffers.
- The user's own `init.lua` is not part of the contract; termocode sets up nvim
  itself from Lua chunks (see [ADR 0002](0002-lua-chunks-in-go-raw-strings.md)).

## Consequences
- **+** Real editing: motions, undo tree, registers, Tree-sitter, built-in LSP
  client, nvim-dap — for free.
- **+** Our chrome stays fully custom; nvim only owns the editor rectangle.
- **−** Hard runtime dependency on `nvim` ≥ 0.10 (0.9.5 mostly works). `Help: Run
  Doctor` and `termocode setup` check for it.
- **−** Two processes and one RPC hop per keystroke; redraw batching matters.
- **−** Some behaviour has two sources of truth (e.g. cursor, mode) that must be
  kept in sync across the RPC boundary.
