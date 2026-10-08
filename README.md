# termocode

A terminal IDE that feels like VSCode — built on Bubble Tea, powered by a real Neovim process under the hood.

<!--
Screenshots are not committed yet. Generate them with
`vhs assets/screenshots/demo.tape` (see assets/screenshots/README.md), then uncomment:
![termocode demo](assets/screenshots/demo.gif)
-->

```sh
go build -o termocode ./cmd/termocode
./termocode [path]
```

Without `path`, opens in the current directory.

## What you get

- **Real Neovim** as the editor (buffers, motions, undo tree, LSP, DAP) wrapped in a familiar IDE chrome.
- **Multi-cursor**, **bracket-matched selection**, **clipboard history**, **snippets**, **format-on-save**.
- **Fuzzy file open**, **command palette**, **workspace ripgrep search**, **live find/replace**.
- **LSP** out of the box for Go, Python, TypeScript / JavaScript, Rust, C / C++, Lua (servers installed separately).
- **DAP** debugger with breakpoints, step over / into / out for Go (delve), Python (debugpy), Node.
- **Git** sidebar with stage, diff, commit, stash, log, blame, branch switch — plus inline gutter signs.
- **Tabs**, **splits**, **bookmarks**, **outline**, **markdown preview**, **integrated terminal**.
- **Themes** (VSCode Dark+, GitHub Dark, One Dark, Solarized Dark) + a full custom-theme editor.
- **Session persistence** — tabs, cursors, expanded folders, recent files, recent workspaces, undo history.

## Install

Once a release is published to the package managers:

```sh
brew install amin-jalali/termocode/termocode   # macOS / Linux (Homebrew tap)
yay -S termocode-bin                           # Arch Linux (AUR)
```

Or grab a tarball from the GitHub Releases page, or build from source.

Building requires Go ≥ 1.26, plus a handful of runtime dependencies:

- `nvim` ≥ 0.10 on `$PATH`
- `rg` (ripgrep) for fast workspace search (optional — a slower built-in search is used without it)
- `git` for the Source Control sidebar
- A truecolor terminal (kitty, wezterm, ghostty, alacritty, iTerm2, modern Windows Terminal)

Optional but recommended:

- Language servers — `gopls`, `pyright`, `ts_ls`, `rust-analyzer`, `clangd`, `lua-language-server`
- Debug adapters — `dlv`, `debugpy`, `node --inspect`

```sh
git clone https://github.com/amin-jalali/termocode.git
cd termocode
./scripts/dev.sh    # vet + test + build
```

For cross-compiled release tarballs (linux/darwin × amd64/arm64) in `dist/`:

```sh
./scripts/release.sh
```

## Documentation

- **[Feature guide](docs/features.md)** — every keybinding, palette command, and feature in detail.
- **[Architecture](docs/architecture.md)** — how the pieces fit: Bubble Tea, Neovim RPC, overlay rendering, persistence, themes.
- **[Contributing](CONTRIBUTING.md)** — build, test, code layout, style, commit messages, releases.
- **[Decisions](docs/adr/README.md)** and **[QC cases](docs/qc/README.md)**.

Something not working? Run `termocode setup` in a shell, or `Help: Run Doctor` from the command palette (`F1`), to check nvim, ripgrep, language servers and fonts.

## CI

Every push and PR runs the test matrix (Ubuntu + macOS) via `.github/workflows/test.yml`. Tagged releases (`v*`) trigger `.github/workflows/release.yml`, which cross-compiles and attaches binaries to a GitHub Release, then updates the Homebrew tap and the AUR package.
