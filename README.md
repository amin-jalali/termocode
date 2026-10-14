# termocode

[![Docs](https://img.shields.io/badge/docs-amin--jalali.github.io%2Ftermocode-4051b5)](https://amin-jalali.github.io/termocode/)
[![test](https://github.com/amin-jalali/termocode/actions/workflows/test.yml/badge.svg)](https://github.com/amin-jalali/termocode/actions/workflows/test.yml)

A terminal IDE that feels like VSCode — built on Bubble Tea, powered by a real Neovim process under the hood.

**Documentation: <https://amin-jalali.github.io/termocode/>**

![termocode: explorer, tabs, editor and status bar](assets/screenshots/editor.png)

| | |
|---|---|
| ![Start page](assets/screenshots/dashboard.png) | ![Command palette](assets/screenshots/palette.png) |
| ![AI chat with a Markdown answer](assets/screenshots/ai-chat-answer.png) | ![Catppuccin Latte](assets/screenshots/theme-catppuccin-latte-dashboard.png) |

## Highlights

- **Real Neovim** as the editor — your motions, registers and `init.lua` — inside tabs, an explorer and a command palette (`F1`).
- **AI assistant** — ghost-text completion, a chat panel with an agent, and Explain / Fix / Edit actions you review as a diff. Anthropic, OpenAI, OpenRouter or local Ollama.
- **Run and Debug** — breakpoints, watch, call stack and a Debug Console for Go, Python and Node, with `launch.json`.
- **Test explorer** for Go, pytest, Rust, Jest and Vitest, plus **tasks** and a **Problems** panel.
- **Git** — stage hunks, commit, sync, stash, blame, a commit graph, a merge-conflict resolver and clone.
- **Language servers** for Go, Python, JS/TS, Rust, C/C++, Lua and more — with a built-in installer.
- **Fast search** across every workspace folder with ripgrep: case / word / regex toggles and globs.
- **Make it yours** — themes (VSCode Dark+, Catppuccin Mocha / Latte, Tokyo Night, Rosé Pine / Dawn, …), a custom theme editor, Nerd Font / Unicode / ASCII icons, key rebinding, snippets, settings export, and **Lua extensions**.
- **Looks good everywhere** — rounded floating cards, status pills, smooth animations, inline images (kitty / sixel / half-blocks), and a clean fallback on 16-colour, no-Nerd-Font and SSH sessions.

## Install

termocode runs on Linux and macOS (amd64 and arm64). It needs `nvim` ≥ 0.10.
One line, no sudo — it installs to `~/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/amin-jalali/termocode/main/scripts/install.sh | sh
termocode            # opens the current folder
```

Run the same line again to upgrade. Uninstall with `... | sh -s -- --uninstall`.
You can also download a tarball from [Releases](https://github.com/amin-jalali/termocode/releases),
or build with Go ≥ 1.26: `go install github.com/amin-jalali/termocode/cmd/termocode@latest`
(on Linux, install the X11 headers first: `sudo apt install libx11-dev`).
See the [install guide](https://amin-jalali.github.io/termocode/getting-started/install/).

Something not working? Run `termocode setup`, or **Help: Run Doctor** from the palette (`F1`).

## Learn more

- [First 5 minutes](https://amin-jalali.github.io/termocode/getting-started/first-steps/)
- [Guides](https://amin-jalali.github.io/termocode/features/) · [Reference](https://amin-jalali.github.io/termocode/reference/)
- [Architecture](docs/architecture.md) · [Decisions](docs/adr/README.md) · [Contributing](CONTRIBUTING.md)

## CI

Every push and PR runs the test matrix (Ubuntu + macOS) via `.github/workflows/test.yml`.
Tagged releases (`v*`) trigger `.github/workflows/release.yml`.
Pushes to `main` that touch the docs rebuild the site via `.github/workflows/docs.yml`.
