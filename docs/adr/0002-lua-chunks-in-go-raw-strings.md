# ADR 0002 — Neovim-side logic as Lua chunks in Go raw strings

Status: **Accepted** (2026-05-13, backfilled)

## Context
Many features need code that runs *inside* Neovim: LSP setup, snippets, git
gutter signs, DAP adapters, theme highlight groups, bracket colouring, find
results buffer. That code has to ship with the Go binary. Options:

1. A separate `runtime/` folder of `.lua` files installed next to the binary.
2. `go:embed` of `.lua` files, loaded at startup.
3. Lua source written directly in Go files as raw-string constants.

Option 1 breaks the "single static binary" release story. Option 2 is clean but
makes it harder to build Lua from Go values (paths, settings).

## Decision
Keep each Lua chunk in a `*_lua.go` file in `internal/app` (e.g. `lsp_lua.go`,
`git_signs_lua.go`, `dap_lua.go`, `theme_lua.go`) as a Go raw string, and run it
with `nvim.ExecLua` / `EvalLuaString`. When a chunk needs Go values, a small
function builds the string (`dapSetupLua(path)`, `multiCursorSetupLua(path)`)
and escapes inputs with `luaEscape`.

## Consequences
- **+** One static binary; no runtime files to find or version.
- **+** The Lua sits next to the Go code that calls it, so both change together.
- **−** **No backticks inside the Lua**, not even in comments — a backtick ends the
  Go raw string and breaks the build. Use `--` comments and plain quotes.
- **−** No Lua syntax highlighting or linting inside Go strings; errors only show
  at runtime (and are usually swallowed by `_ = m.nvim.ExecLua(...)`). Check
  `~/.config/termocode/errors.log` and test with a real nvim.
- If a chunk grows large, moving it to `go:embed` is an allowed later step and
  does not need a new ADR.
