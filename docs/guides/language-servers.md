# Install language servers and debuggers

Language servers give you go-to-definition, completion, rename, errors and
formatting. Debug adapters let you debug. termocode can install both for you.

## Which tools are supported

**Language servers**

| Language | Server | Installed with |
|---|---|---|
| Go | `gopls` | `go install` |
| Python | `pyright` (or `pylsp`) | npm (pip for `pylsp`) |
| JavaScript / TypeScript | `ts_ls` (typescript-language-server) | npm |
| Rust | `rust_analyzer` | rustup |
| C / C++ | `clangd` | GitHub release download |
| Lua | `lua_ls` | GitHub release download |
| Shell | `bashls` | npm |
| TOML | `taplo` | cargo |

**Debug adapters**

| Language | Adapter | Installed with |
|---|---|---|
| Go | `dlv` | `go install` |
| Python | `debugpy` | pip (in its own virtual env) |
| JavaScript / TypeScript | `js-debug` | GitHub release download (needs `node`) |

The installer needs the matching toolchain. For example, `pyright` needs
`npm`, and `gopls` needs `go`.

## Install a language server

1. Press `F1` and run **LSP: Manage Language Servers...**.
2. Each row shows a status:
    - `✓ on PATH` — you already have it.
    - `✓ managed` — termocode installed it.
    - `✗ missing` — not found. The hint shows how it will be installed.
3. Pick a missing server and choose **Install**.
4. Progress shows in toasts. When it is done, reopen the file (or run
   **Developer: Reload Window**).

For an installed server you can choose **Reinstall** or **Uninstall**.

The first time you open a file type with no server, termocode shows a
one-time hint that names the server to install.

The status bar shows the attached server, for example `{} gopls`.
`{} none` means no server is running for this file.

## Install a debug adapter

Run **DAP: Install Adapter...** and pick one. It works the same way.

## Where the tools go

termocode puts its tools in its own folder, not in your system:

```text
~/.local/share/termocode/tools/
  bin/          ← added to Neovim's PATH
  gopls/
  pyright/
  ...
```

`$XDG_DATA_HOME` is used when it is set. Tools already on your `PATH` are used
as they are; termocode does not replace them.

## Install from a shell

```sh
termocode setup                  # report every server and adapter
termocode setup --install gopls  # install one by name
```

## Keys at a glance

| Command | Action |
|---|---|
| **LSP: Manage Language Servers...** | Install, reinstall or remove servers |
| **DAP: Install Adapter...** | Install debug adapters |
| **Help: Run Doctor** | See what is installed |
| `Ctrl+Alt+I` | Buffer info — shows the attached server |

## Troubleshooting

**"missing npm" (or go, pip, cargo, rustup)**
:   The installer needs that toolchain first. Install it with your package
    manager, then try again.

**`clangd` cannot be installed on Linux ARM**
:   There is no official build for linux/arm64. Install it with your package
    manager, e.g. `sudo apt install clangd`.

**The install fails with a network error**
:   You are offline or behind a proxy. Install the tool by hand and put it on
    your `PATH`. termocode uses it from there.

**The server is installed but nothing happens**
:   termocode starts a server only inside a project root. For example,
    `gopls` needs a `go.mod`, `go.work` or `.git` folder above the file.
    Run **Developer: Reload Window** after an install.
