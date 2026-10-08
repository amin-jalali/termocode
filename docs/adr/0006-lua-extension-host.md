# ADR 0006 — Extensions run as Lua inside the embedded Neovim

Status: **Accepted** (2026-10-08)

## Context
Users want to add their own commands, panels and status-bar items without
rebuilding termocode. Options:

1. **Go plugins** (`plugin` package) — Linux/macOS only, must be built with
   the exact same Go toolchain and module versions. Not workable for users.
2. **Separate processes over JSON-RPC** (like VS Code / LSP) — any language,
   strong isolation, but every extension needs a runtime, a protocol
   version, process lifecycle and crash handling. Heavy for small tools.
3. **An embedded scripting VM in Go** (gopher-lua, goja) — one more language
   runtime in the binary, and it cannot touch buffers without a bridge.
4. **Neovim's own Lua runtime** — already running (ADR 0001), already has
   the whole `vim.*` API (buffers, autocmds, jobs, LSP), and already talks
   to Go both ways (`ExecLua` and the `termocode_notify` event channel).

## Decision
Tier 1 extensions are Lua files executed inside the embedded `nvim`:
`~/.config/termocode/extensions/<name>/init.lua`, with an optional
`extension.json` (`name`, `version`, `description`) and an optional `lua/`
folder for `require()`.

- `internal/app/ext_lua.go` installs the API table `_G.termocode`
  (`register_command`, `register_panel`, `register_status_item`, `on`,
  `notify`, `prompt`, `pick`, `preview`, `open`, `refresh`, `run`, `escape`,
  `wrap`). Go talks to the private side `_G._termocode_ext` (load, registry,
  poll, run, select, resolve).
- `internal/ext` (pure Go) discovers folders, parses the registry JSON, owns
  the `{{token}}…{{/}}` markup and the New Extension scaffold.
- `internal/app/ext.go` loads extensions after the built-in Lua chunks,
  adds `Ext: <Title>` palette rows, extension activity-bar items
  (`activity.SetExtraItems`), a generic text-panel renderer, status items
  (`statusbar.State.Ext`) and `keymap.json` `"ext:<id>"` bindings.
- Panels render as **lines of markup**, never raw ANSI. Tag names map to
  theme tokens (`accent`, `muted`, `error`, `keyword`, `added`, `bold`, …),
  so extensions follow the user's theme and cannot break the frame.
- **A broken extension never blocks startup.** Every call into extension
  code is `pcall`'d with a time budget (250 ms per call, 2 s for `init.lua`)
  enforced by a debug count hook. LuaJIT skips hooks in compiled traces, so
  extension chunks and their modules are `jit.off()`'d. Errors go to a toast
  and the Error Log; a render or status callback that fails three times is
  switched off until "Extensions: Reload". Go makes every call from a
  `tea.Cmd` with its own timeout, so even a blocking C call
  (`vim.fn.system('sleep 60')`) does not freeze the UI.
- Panels re-render on a 1 s heartbeat and when Lua calls
  `termocode.refresh()` (`termocode_ext_panel_dirty` notification).

## Consequences
- **+** No new runtime, no new protocol, no build step; extensions can use
  all of Neovim (buffers, LSP, jobs) and existing Neovim knowledge.
- **+** Shipping samples is trivial: `extensions/` in the repo is embedded
  and installed with "Extensions: Install Sample Extensions".
- **−** Extensions share one Lua state with termocode's own Lua. They can
  read or break globals, keymaps and options — there is no sandbox. This is
  "run code you trust", like a Neovim plugin.
- **−** Extension Lua runs in the LuaJIT interpreter (JIT off) so the time
  budget holds; fine for UI glue, slow for heavy number crunching. Long
  work belongs in a job (`vim.fn.jobstart`) with `termocode.wrap` callbacks.
- **−** A C call that blocks nvim stalls the editor (not the Go UI) until it
  returns; nothing in Lua can interrupt it.
- Follow-up (Tier 2): out-of-process extensions over JSON-RPC for other
  languages and real isolation. The Tier 1 API names should map onto it.
