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
After nvim setup, `pluginBootstrapCmds` (`internal/app/plugins.go`) runs
`git clone --depth 1` in the background for each plugin that is not already in
the plugins dir. Startup and typing never wait for it; a plugin is set up in
the running nvim when its clone finishes (update 2026-10: the clones used to
block nvim setup, and the bracket-pair one blocked nvim itself). The plugins
dir is:

- `$TERMOCODE_PLUGINS_DIR`, else `$XDG_DATA_HOME/termocode/plugins`, else
  `~/.local/share/termocode/plugins`.

The rules for every bootstrap:

- **Idempotent** — an existing checkout means "already installed".
- **Silent** — no output on success.
- **Non-fatal** — no git, no network or a failed clone shows a warning toast;
  the editor runs without that feature. The clone goes to a temp folder and is
  renamed into place, so a partial clone never looks installed and the next
  launch retries.

## Consequences
- **+** Zero-config: features work after the first online launch.
- **+** Plugins are isolated from the user's own nvim setup.
- **−** First launch needs `git` and network; offline users lose those
  features until a later online launch.
- **−** We track upstream `HEAD`, not a pinned version. An upstream break can
  reach users without a termocode release. Pinning to a tag/commit is a
  possible follow-up.
- **−** No update path: plugins are never pulled again. Users can delete the
  plugins dir to refresh.
