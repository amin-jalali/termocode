# Install

termocode runs on **Linux** and **macOS**, on amd64 (x86-64) and arm64
(Apple Silicon, ARM servers). It takes about one minute. On Android it
runs inside Termux: see [Android (Termux)](#android-termux).

## What you need

| Tool | Why | Needed? |
|---|---|---|
| `nvim` 0.10 or newer | the editor engine | yes |
| `git` | Source Control panel, and the first-run plugin download | yes |
| `rg` (ripgrep) | fast workspace search | no — a slower built-in search is used without it |
| A truecolor terminal | colors | yes — kitty, WezTerm, Ghostty, Alacritty, iTerm2, Windows Terminal, GNOME Terminal… |
| A Nerd Font | nicer icons | no — on Linux, `termocode setup` can install one |
| Go 1.26 or newer | only to build from source | no |

## One-line install (recommended)

```sh
curl -fsSL https://raw.githubusercontent.com/amin-jalali/termocode/main/scripts/install.sh | sh
```

The script:

- finds your OS and CPU, and downloads the newest release from GitHub;
- checks the download against the release's `checksums.txt` (sha256);
- puts `termocode` in `~/.local/bin` — no `sudo`;
- tells you if that folder is not on your `PATH`, and how to add it;
- checks your `nvim` version and tells you how to install or upgrade it.
  It never installs system packages itself.

Run the same line again to **upgrade**. If you already have the newest
version, it does nothing.

Options (after `sh -s --`):

```sh
curl -fsSL https://raw.githubusercontent.com/amin-jalali/termocode/main/scripts/install.sh | sh -s -- --version v0.1.0
curl -fsSL https://raw.githubusercontent.com/amin-jalali/termocode/main/scripts/install.sh | sh -s -- --uninstall
curl -fsSL https://raw.githubusercontent.com/amin-jalali/termocode/main/scripts/install.sh | PREFIX=/opt/termocode sh
```

| Option | What it does |
|---|---|
| `--version vX.Y.Z` (or `TERMOCODE_VERSION=vX.Y.Z`) | install this version, not the newest |
| `--force` | install again, even if this version is already there |
| `--uninstall` | remove the binary (your settings in `~/.config/termocode` stay) |
| `PREFIX=/some/dir` | install to `/some/dir/bin` instead of `~/.local/bin` |

Want to read the script first? Download it, read it, then run it:

```sh
curl -fsSLO https://raw.githubusercontent.com/amin-jalali/termocode/main/scripts/install.sh
less install.sh
sh install.sh
```

Check that it worked:

```sh
termocode --version
```

## Download a release by hand

Each [GitHub release](https://github.com/amin-jalali/termocode/releases)
has four tarballs and a `checksums.txt`:

| File | For |
|---|---|
| `termocode-vX.Y.Z-linux-amd64.tar.gz` | Linux on x86-64 |
| `termocode-vX.Y.Z-linux-arm64.tar.gz` | Linux on ARM (64-bit Raspberry Pi OS, ARM servers) |
| `termocode-vX.Y.Z-darwin-amd64.tar.gz` | macOS on Intel |
| `termocode-vX.Y.Z-darwin-arm64.tar.gz` | macOS on Apple Silicon (M1 and newer) |

```sh
v=v0.1.0; f=termocode-$v-linux-amd64          # pick yours
curl -fsSLO https://github.com/amin-jalali/termocode/releases/download/$v/$f.tar.gz
curl -fsSLO https://github.com/amin-jalali/termocode/releases/download/$v/checksums.txt
sha256sum --ignore-missing -c checksums.txt   # macOS: shasum -a 256 --ignore-missing -c checksums.txt
tar -xzf $f.tar.gz
mkdir -p ~/.local/bin && mv $f/termocode ~/.local/bin/
```

The Linux binaries need glibc 2.35 or newer (Ubuntu 22.04, Debian 12,
Fedora 36 and later). On musl systems such as Alpine, build from source.

On macOS, a file downloaded with a browser may be blocked by Gatekeeper.
The install script and `curl` do not have this problem. If you used a
browser, run `xattr -d com.apple.quarantine ~/.local/bin/termocode`.

## Install with Go

Needs Go 1.26 or newer. On Linux, the clipboard library needs the X11
headers to build:

```sh
sudo apt install libx11-dev     # Debian / Ubuntu
go install github.com/amin-jalali/termocode/cmd/termocode@latest
```

`go install` puts the `termocode` binary in `$(go env GOPATH)/bin`.
Make sure that folder is on your `PATH`. Run the same command again to
update.

## Build from source

To hack on termocode, or to build a local binary:

```sh
git clone https://github.com/amin-jalali/termocode.git
cd termocode
go install ./cmd/termocode       # or: go build -o termocode ./cmd/termocode
```

## Android (Termux)

termocode runs on an Android phone or tablet (arm64) inside the
[Termux](https://termux.dev) app.

1. Install Termux **and the Termux:API app** from the same place
   (F-Droid, GitHub or the Play Store). Do not mix builds from different
   places. termocode works with both the F-Droid and the Play Store
   Termux.
2. In Termux, install the tools:

    ```sh
    pkg install neovim git termux-api
    pkg install ripgrep    # optional: fast search
    ```

3. Install termocode with the same script as on Linux:

    ```sh
    curl -fsSL https://raw.githubusercontent.com/amin-jalali/termocode/main/scripts/install.sh | sh
    termocode
    ```

    The script sees Termux and gets the Android build. It puts
    `termocode` in Termux's own `bin` folder, which is already on your
    `PATH`.

**Build it yourself** (also for x86_64 Termux in an emulator, which has
no release build): `pkg install golang`, then
`go install github.com/amin-jalali/termocode/cmd/termocode@latest` and
add `$HOME/go/bin` to your `PATH`. This needs Go 1.26 or newer. No X11
headers are needed: the Android build leaves the X11 clipboard library
out.

**Language servers.** Most install from the language server manager as
on Linux (`gopls` needs `pkg install golang`, the npm ones need
`pkg install nodejs`, the pip ones need `pkg install python`). A few
cannot: their downloads are built for normal Linux, and `rustup` does
not exist in Termux. For these the manager shows the Termux package:

| Server | In Termux |
|---|---|
| rust-analyzer | `pkg install rust-analyzer` |
| clangd | `pkg install clang` |
| lua-language-server | `pkg install lua-language-server` |

Language servers use a lot of memory (`gopls` 300–700 MB, rust-analyzer
1 GB or more). On a phone with little RAM, Android may close Termux in
the background. termocode works without them; you only lose completion,
errors as you type and go to definition.

**Android 12 and newer** can stop child processes of apps it thinks run
too many ("phantom process killer"). If language servers or the
terminal die without a reason, turn it off: Developer options →
"Disable child process restrictions", or from a computer with adb:
`adb shell settings put global settings_enable_monitor_phantom_procs false`.

**Clipboard.** termocode uses `termux-clipboard-set` and
`termux-clipboard-get` (package `termux-api`) for the Android clipboard,
so copy and paste work with other apps. Without them, copies still reach
Android through the terminal (OSC 52, Termux supports it), but pasting
from other apps does not work. termocode then says: install the
Termux:API app and run `pkg install termux-api`. Neovim's own `+`
register also uses these commands.

To save battery, termocode does not watch the Android clipboard. The
clipboard history (`Ctrl+Shift+V`) shows copies made in the editor, not
copies made in other apps.

**Icons.** `termocode setup` does not install fonts on Android. To get
Nerd Font icons, copy a Nerd Font `.ttf` to `~/.termux/font.ttf` and run
`termux-reload-settings`. Or set `TERMOCODE_ICON_MODE=ascii`.

## Run it

```sh
termocode              # open the current folder
termocode ~/code/app   # open another folder
termocode setup        # check your system (and install the Nerd Font on Linux)
termocode --version    # print the version
termocode -h           # short help
```

The first launch downloads three small Neovim plugins with `git clone`
(`nvim-dap`, `vim-visual-multi`, `rainbow-delimiters`). The download runs in
the background: you can work at once, and each feature turns on when its
plugin arrives. If you are offline, a message says the download failed.
Those features stay off until the next online start.

## Package managers

| Method | Status |
|---|---|
| Homebrew tap (`amin-jalali/termocode`) | **coming soon** — not published yet |
| AUR (`termocode-bin`) | **coming soon** — not published yet |

## Next steps

- Check your setup: [Run Doctor](help-doctor.md).
- Learn the basics: [First 5 minutes](first-steps.md).
- Add language servers: [Install language servers and debuggers](../guides/language-servers.md).

## Troubleshooting

**`termocode: command not found`**
:   The install folder is not on your `PATH`. For the install script, add
    `export PATH="$HOME/.local/bin:$PATH"` to `~/.bashrc` or `~/.zshrc`.
    For `go install`, add `export PATH="$PATH:$(go env GOPATH)/bin"`.
    Then open a new terminal.

**The install script says "checksum mismatch"**
:   The download was broken or changed. Nothing was installed. Run the
    script again. If it still fails, please open an issue.

**The install script says "could not find the latest release"**
:   GitHub could not be reached, or no release is published yet. Check
    your network, or use [Install with Go](#install-with-go).

**`go: go.mod requires go >= 1.26`**
:   Your Go is too old. Install a newer Go from [go.dev/dl](https://go.dev/dl/).

**Build fails with `X11/Xlib.h: No such file or directory`**
:   Install the X11 headers: `sudo apt install libx11-dev`.

**Colors look wrong or flat**
:   Use a truecolor terminal. Run `termocode setup --test-colors` to see a
    color gradient. It should look smooth.

**Icons show as boxes or `?`**
:   On Linux, run `termocode setup` to install the JetBrainsMono Nerd Font,
    then pick it in your terminal. `termocode setup` prints your terminal's
    font and the exact steps (it never changes terminal settings). On macOS,
    install any Nerd Font by hand. Or set `TERMOCODE_ICON_MODE=ascii`.

**On macOS, Alt shortcuts type `å`, `ƒ`, `Ω` instead**
:   macOS sends a letter for Option by default. Make Option act as Alt:
    in Terminal, Settings → Profiles → Keyboard → **Use Option as Meta
    key**; in iTerm2, Settings → Profiles → Keys → Left / Right Option key
    → **Esc+**. **Help: Run Doctor** shows this hint in both terminals.

**F1 opens the palette of VS Code (or JetBrains), not termocode's**
:   The IDE keeps F1 for itself. Press `Alt+X` to open termocode's command
    palette.

**Persian or Arabic text crosses the pane borders in Konsole**
:   Konsole reorders right-to-left rows itself. Turn off Profile →
    Advanced → **Bi-Directional text rendering**, then set `"rtl":
    "termocode"` in `config.json`.

**On the Linux console (no desktop)**
:   termocode works with 16 colours, ASCII icons and no mouse. It clears
    the screen when it quits, because the console has no alternate screen.
