# Architecture Decision Records

Short, immutable records of the significant architectural decisions in termocode.
Format: Status · Context · Decision · Consequences. Start from
[`0000-template.md`](0000-template.md).

**Supersede, don't edit.** When a decision changes, write a new ADR, set the old
one's status to `Superseded by NNNN`, and link both ways. Typo fixes are fine.

Write an ADR when a choice is hard to undo, surprises a new contributor, or was
picked over a clear alternative. A normal feature does not need one.

| # | Title | Status |
|---|---|---|
| [0001](0001-embedded-neovim-editor-engine.md) | Embedded `nvim --embed` as the editor engine | Accepted |
| [0002](0002-lua-chunks-in-go-raw-strings.md) | Neovim-side logic as Lua chunks in Go raw strings | Accepted |
| [0003](0003-auto-bootstrapped-nvim-plugins.md) | Auto-bootstrap a few nvim plugins by `git clone` on first launch | Accepted |
| [0004](0004-pure-go-search-fallback.md) | Pure-Go search fallback when ripgrep is missing | Accepted |
| [0005](0005-config-never-breaks-startup.md) | Config files can never break startup | Accepted |
| [0006](0006-lua-extension-host.md) | Extensions run as Lua inside the embedded Neovim | Accepted |

> The folder was started after the project was already running, so 0001–0005
> backfill decisions that were made earlier. Dates are the best guess from git
> history and code comments.
