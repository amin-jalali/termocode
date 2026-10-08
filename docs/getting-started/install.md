# Install

termocode runs on **Linux** and **macOS**. Today you build it from source.
It takes about one minute.

## What you need

| Tool | Why | Needed? |
|---|---|---|
| Go 1.26 or newer | to build termocode | yes (to build) |
| `nvim` 0.10 or newer | the editor engine | yes |
| `git` | Source Control panel, and the first-run plugin download | yes |
| `rg` (ripgrep) | fast workspace search | no — a slower built-in search is used without it |
| A truecolor terminal | colors | yes — kitty, WezTerm, Ghostty, Alacritty, iTerm2, Windows Terminal, GNOME Terminal… |
| A Nerd Font | nicer icons | no — on Linux, `termocode setup` can install one |

On Linux, the clipboard library needs X11 headers to build:

```sh
sudo apt install libx11-dev     # Debian / Ubuntu
```

## Build from source

```sh
git clone https://github.com/amin-jalali/termocode.git
cd termocode
go install ./cmd/termocode
```

`go install` puts the `termocode` binary in `$(go env GOPATH)/bin`.
Make sure that folder is on your `PATH`.

If you prefer a local binary:

```sh
go build -o termocode ./cmd/termocode
./termocode
```

!!! note "`go install …@latest` does not work yet"
    The Go module is named `termocode`, not `github.com/amin-jalali/termocode`.
    So `go install github.com/amin-jalali/termocode/cmd/termocode@latest`
    fails. Clone the repository and run `go install ./cmd/termocode` inside it.

## Run it

```sh
termocode              # open the current folder
termocode ~/code/app   # open another folder
termocode setup        # check your system (and install the Nerd Font on Linux)
termocode -h           # short help
```

The first launch downloads three small Neovim plugins with `git clone`
(`nvim-dap`, `vim-visual-multi`, `rainbow-delimiters`). If you are offline,
termocode still starts. Those features stay off until the next online start.

## Package managers

| Method | Status |
|---|---|
| GitHub Releases tarballs (linux / darwin, amd64 / arm64) | **coming soon** — the release workflow is ready, but no version is tagged yet |
| Homebrew tap (`amin-jalali/termocode`) | **coming soon** — not published yet |
| AUR (`termocode-bin`) | **coming soon** — not published yet |

When a release is out, this page will show the exact commands.

## Next steps

- Check your setup: [Run Doctor](help-doctor.md).
- Learn the basics: [First 5 minutes](first-steps.md).
- Add language servers: [Install language servers and debuggers](../guides/language-servers.md).

## Troubleshooting

**`go: go.mod requires go >= 1.26`**
:   Your Go is too old. Install a newer Go from [go.dev/dl](https://go.dev/dl/).

**Build fails with `X11/Xlib.h: No such file or directory`**
:   Install the X11 headers: `sudo apt install libx11-dev`.

**`termocode: command not found`**
:   Add Go's bin folder to your `PATH`:
    `export PATH="$PATH:$(go env GOPATH)/bin"`.

**Colors look wrong or flat**
:   Use a truecolor terminal. Run `termocode setup --test-colors` to see a
    color gradient. It should look smooth.

**Icons show as boxes or `?`**
:   On Linux, run `termocode setup` to install the JetBrainsMono Nerd Font,
    then pick it in your terminal. On macOS, install any Nerd Font by hand. Or set `TERMOCODE_ICON_MODE=ascii`.
