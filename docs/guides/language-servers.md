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
| Dockerfile | `dockerls` (docker-langserver) | npm |
| Docker Compose | `docker_compose_language_service` (only on `compose.yaml`, `docker-compose.yml` and `compose.*.yaml` files) | npm |
| YAML / Kubernetes | `yamlls` (yaml-language-server; Kubernetes schema for files in `k8s/`, `kubernetes/`, `manifests/`, `deploy/` and `*.k8s.yaml`, other files from the schema store) | npm |

**Dockerfile lint.** When `hadolint` is on your PATH (or in termocode's tools folder), its findings show as diagnostics when you open or save a Dockerfile. termocode does not install `hadolint`; without it nothing runs.

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

## Tools your dev container needs

If your project has a `.devcontainer/devcontainer.json`, termocode reads it
once when you open the project. It looks at the `image`, the `features` and
the editor extensions listed there. If they point to tools you do not have
(for example `gopls` and `dlv` for a Go container), a quiet toast says:
"This project's dev container uses tools you don't have".

Click the toast to see just those tools. Pick one to install it, as above.
termocode never installs anything until you confirm. It asks once per
project; if you ignore the toast it does not come back. It only reads the
file: no network, no Docker.

## Folders opened in a container

When you open a folder inside a container (**Workspace: Open Folder in
Container…**), termocode never installs anything in it. It picks a language
server like this:

1. **In the container.** If the image already has the server (on `PATH`,
   in `node_modules/.bin`, `.venv/bin`, `~/go/bin` or `~/.cargo/bin`),
   termocode runs it there with `docker exec -i`. It sees the same files,
   packages and toolchain as your code.
2. **Local.** Otherwise a server on your machine is used through
   `termocode lsp-proxy`, which rewrites the file paths between the
   container (`/app`) and your machine. It reads the bind-mounted folder,
   or, when the folder is not mounted, a read-only local copy of the code
   (the *mirror*, in `~/.cache/termocode/mirror/`, private to you).
3. **None** when neither has it.

The status bar chip says where each server runs: `{} gopls · in api-1` or
`{} ts_ls · local`.

A local server cannot see packages that exist only in the container, so
its errors are often wrong there. That is why a local server shows **no
diagnostics** in a container workspace. Completion, hover and go to
definition still work. To see its diagnostics anyway, run **LSP: Toggle
Local Diagnostics in Container Workspace**.

| Command | What it does |
| --- | --- |
| **LSP: Choose Where the Language Server Runs…** | Auto (container first, then local), in the container only, local only, or off. |
| **LSP: Toggle Local Diagnostics in Container Workspace** | Show or mute a local server's errors and warnings. Muted by default. |
| **LSP: Refresh Container Mirror** | Copy the code into the mirror again (after changes made outside termocode). |
| **LSP: Clear Container Mirror…** | Delete the local copy. Asks first; nothing changes in the container. |

The mirror leaves out `node_modules`, `.venv`, `vendor`, `target`,
`__pycache__` and `.git/objects`, holds at most 512 MB per folder and
2 GB in all (the oldest go first). Your saves from termocode update it at
once. In `config.json` the settings are `container_lsp`
(`auto`, `image`, `local`, `off`) and `container_local_lsp_diagnostics`
(`on`, `off`).

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
