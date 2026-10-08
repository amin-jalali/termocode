# termocode

[![Docs](https://img.shields.io/badge/docs-amin--jalali.github.io%2Ftermocode-4051b5)](https://amin-jalali.github.io/termocode/)
[![test](https://github.com/amin-jalali/termocode/actions/workflows/test.yml/badge.svg)](https://github.com/amin-jalali/termocode/actions/workflows/test.yml)

A terminal IDE that feels like VSCode — built on Bubble Tea, powered by a real Neovim process under the hood.

**Documentation: <https://amin-jalali.github.io/termocode/>**

<!--
Screenshots are not committed yet. Generate them with
`vhs assets/screenshots/demo.tape` (see assets/screenshots/README.md), then uncomment:
![termocode demo](assets/screenshots/demo.gif)
-->

## Highlights

- **Real Neovim** as the editor — your motions, registers and `init.lua` — inside tabs, an explorer and a command palette (`F1`).
- **AI assistant** — ghost-text completion, a chat panel with an agent, and Explain / Fix / Edit actions you review as a diff. Anthropic, OpenAI, OpenRouter or local Ollama.
- **Run and Debug** — breakpoints, watch, call stack and a Debug Console for Go, Python and Node, with `launch.json`.
- **Test explorer** for Go, pytest, Rust, Jest and Vitest, plus **tasks** and a **Problems** panel.
- **Git** — stage hunks, commit, sync, stash, blame, a commit graph, a merge-conflict resolver and clone.
- **Language servers** for Go, Python, JS/TS, Rust, C/C++, Lua and more — with a built-in installer.
- **Fast search** across every workspace folder with ripgrep: case / word / regex toggles and globs.
- **Make it yours** — themes, a custom theme editor, key rebinding, snippets, settings export, and **Lua extensions**.

## Install

termocode runs on Linux and macOS. Build it from source (needs Go ≥ 1.26 and `nvim` ≥ 0.10):

```sh
git clone https://github.com/amin-jalali/termocode.git
cd termocode
go install ./cmd/termocode
termocode            # opens the current folder
```

On Linux, install the X11 headers first (`sudo apt install libx11-dev`).
Homebrew, AUR and release tarballs are **coming soon** — they are not published yet.
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
