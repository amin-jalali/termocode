# ADR 0003 — Auto-bootstrap a few nvim plugins by `git clone` on first launch

Status: **Accepted** (2026-05-13, backfilled)

## Context
Some features are much better served by an existing nvim plugin than by our own
code: multi-cursor (`mg979/vim-visual-multi`), the DAP client
(`mfussenegger/nvim-dap`) and bracket-pair colours
(`HiPhish/rainbow-delimiters.nvim`). We do not want users to install a plugin
manager or edit an `init.lua` just to use termocode. Options:

1. Vendor the plugin sources into this repo and embed them.
2. Depend on a plugin manager (lazy.nvim) and a user config.
3. Clone the few plugins we need ourselves, once, into a termocode-owned folder.

Vendoring means tracking upstream licences and updates by hand. A plugin manager
brings in the user's whole config, which breaks the "termocode sets up nvim
itself" rule from [ADR 0001](0001-embedded-neovim-editor-engine.md).

## Decision
On startup, `ensureVimVisualMulti` (`internal/app/multi_cursor.go`),
`ensureNvimDap` (`internal/app/dap.go`) and the Lua in `bracket_pair_lua.go` run
`git clone --depth 1` into the plugins dir if the plugin is not already there:

- `$TERMOCODE_PLUGINS_DIR`, else `$XDG_DATA_HOME/termocode/plugins`, else
  `~/.local/share/termocode/plugins`.

The rules for every bootstrap:

- **Idempotent** — an existing `.git` folder means "already installed".
- **Silent** — no output on success.
- **Non-fatal** — no git, no network or a failed clone only logs a warning; the
  editor starts without that feature. A partial clone is removed so the next
  launch retries.

## Consequences
- **+** Zero-config: features work after the first online launch.
- **+** Plugins are isolated from the user's own nvim setup.
- **−** First launch needs `git` and network; offline users silently lose those
  features until a later online launch.
- **−** We track upstream `HEAD`, not a pinned version. An upstream break can
  reach users without a termocode release. Pinning to a tag/commit is a
  possible follow-up.
- **−** No update path: plugins are never pulled again. Users can delete the
  plugins dir to refresh.
