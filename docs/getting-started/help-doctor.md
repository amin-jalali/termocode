# Check your setup (Doctor)

termocode needs a few outside tools. **Doctor** checks them for you and
tells you what is missing.

## Run Doctor inside termocode

1. Press `F1`.
2. Run **Help: Run Doctor**.
3. Read the report. Press `Esc` to close it.

Doctor is read-only. It installs nothing.

It checks:

- the `nvim` version,
- `rg` (ripgrep) and `git`,
- language servers and debug adapters,
- truecolor support,
- the Nerd Font.

## Run the same checks in a shell

```sh
termocode setup
```

`termocode setup` prints the same report. On Linux it also installs the
JetBrainsMono Nerd Font if it is missing. You can also install one tool:

```sh
termocode setup --install gopls
```

See [Install language servers and debuggers](../guides/language-servers.md)
for the list of tool names.

## Read the error log

termocode keeps a log of errors and warnings.

- In the app: run **Help: Show Error Log**.
- In a shell: `tail -f ~/.config/termocode/errors.log`

Secrets such as API keys are removed from AI errors before they reach the log.

## Keys at a glance

| Key / command | Action |
|---|---|
| **Help: Run Doctor** | Check tools |
| **Help: Show Error Log** | Show recent errors |
| **Help: Show Shortcuts** | Show the shortcut cheat sheet |
| `Ctrl+Alt+I` | Show info about the current buffer (file type, indent, LSP, EditorConfig) |

## Troubleshooting

**Doctor says `nvim` is too old**
:   termocode needs Neovim 0.10 or newer. Install it from your package manager
    or from [neovim.io](https://neovim.io/).

**Doctor says a language server is missing**
:   Run **LSP: Manage Language Servers...** and install it. See
    [the guide](../guides/language-servers.md).

**I changed a config file but nothing happened**
:   Run **Developer: Reload Window**. It restarts termocode in the same folder
    and keeps your tabs.
