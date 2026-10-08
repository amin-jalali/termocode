# Reference

These pages list everything in one place. They are **generated from the source
code** (`go run ./cmd/docgen`), so they always match the version you build.

Use the guides to learn a task. Use the reference to look something up.

<div class="grid cards" markdown>

-   :material-keyboard: **[Keyboard shortcuts](keys.md)**

    ---

    Every default key and the action it runs. Also tells you which keys
    some terminals do not send.

-   :material-console-line: **[Commands](commands.md)**

    ---

    Every command in the palette (`F1`), with its shortcut if it has one.

-   :material-tune: **[Settings](settings.md)**

    ---

    Every key in `config.json`: type, default value and what it does.

-   :material-file-cog-outline: **[Config files](config-files.md)**

    ---

    Every file termocode reads or writes, where it lives, and what it holds.

</div>

## Quick facts

- Config folder: `~/.config/termocode/` (or `$XDG_CONFIG_HOME/termocode/`).
- Project files: `.termocode/tasks.json` and `.termocode/launch.json` in the
  workspace root.
- Managed tools: `~/.local/share/termocode/tools/`.
- Inside the app, **Help: Show Shortcuts** shows a cheat sheet.
