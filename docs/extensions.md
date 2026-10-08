# Extensions

termocode extensions are small Lua programs that run inside the embedded
Neovim. They can add palette commands, sidebar panels and status-bar items.
Why Lua-in-nvim: see [ADR 0006](adr/0006-lua-extension-host.md).

## Quick start

1. Command palette → **Extensions: New Extension...** → type a name.
   termocode creates `~/.config/termocode/extensions/<name>/init.lua`,
   opens it, and loads it.
2. Edit the file, then run **Extensions: Reload**.
3. Your command shows up in the palette as **Ext: <Title>**.

Or try the samples: **Extensions: Install Sample Extensions** copies
`todo-tree`, `word-count`, `open-in-github` and `rest-client` into the
folder (existing folders are never overwritten).

| Palette command | What it does |
|---|---|
| Extensions: Reload | Drop everything and load the folder again |
| Extensions: Open Folder | Open the extensions folder in the file manager |
| Extensions: Show Log | Load results + every extension log / error line |
| Extensions: New Extension... | Scaffold a commented `init.lua` + `extension.json` |
| Extensions: Install Sample Extensions | Copy the bundled samples |

## Layout

```text
~/.config/termocode/extensions/      ($XDG_CONFIG_HOME is honoured)
  my-ext/
    init.lua           required — runs once at startup / reload
    extension.json     optional — {"name", "version", "description"}
    lua/my_ext/util.lua  optional — require('my_ext.util')
  old-thing.disabled/  skipped (rename to disable)
```

Folder names may use letters, digits, `_`, `.` and `-`.

## Example

```lua
local tc = termocode
tc.register_command({ id = 'hello', title = 'Say Hello', run = function()
  tc.prompt({ title = 'Hello', label = 'Name:' }, function(name)
    if name then tc.notify('Hello, ' .. name .. '!') end
  end)
end })
tc.register_status_item({ id = 'lines', command = 'hello', text = function()
  return '{{muted}}' .. vim.api.nvim_buf_line_count(0) .. ' lines{{/}}'
end })
```

## API — the global `termocode`

| Call | Notes |
|---|---|
| `register_command{ id, title, run, hint? }` | Palette row `Ext: <title>`. An id without a dot gets the extension name as prefix (`hello` → `my-ext.hello`). Returns the full id. |
| `register_panel{ id, title, icon?, render(w, h), on_select?(row, line) }` | Adds an activity-bar item and a sidebar panel. `render` returns a list of markup lines (or one string with `\n`). `icon` is one 1-cell glyph; otherwise the first letter of `title`. `on_select` gets the 1-based row and its plain text (click, Enter or Space). |
| `register_status_item{ id, text, command? }` | `text` is a string or a function returning markup. A click runs `command`. Empty text hides the item. |
| `on(event, fn)` | `ready` (all extensions loaded), `save`, `open`, `changed`, `cursor`, `shutdown`, or any Neovim autocmd name (`'BufWritePost'`). `fn` gets `{ event, buf, path, match }`. |
| `notify(msg, level?, detail?)` | Toast. `level`: `info` (default), `warn`, `error`. |
| `prompt({ title, label, default }, cb)` | Text input; `cb(value)` or `cb(nil)` when cancelled. |
| `pick({ title, items }, cb)` | Picker. Items are strings or tables `{ title, hint, … }`; `cb` gets the item table back (extra fields kept) or `nil`. |
| `preview(title, body)` | Read-only scrollable overlay. |
| `open(path, line?, col?)` | Open a file in the editor and move the cursor. |
| `refresh(panel_id?)` | Re-render panels now (otherwise every second). |
| `run(command_id)` | Run a registered command. |
| `escape(text)` | Make text safe inside markup (`{{` → `{{{{`). |
| `wrap(fn)` | Guard a callback you hand to Neovim yourself (jobs, timers, `vim.api` autocmds). |
| `log(msg)` | Line in **Extensions: Show Log**. |

Everything else in Neovim (`vim.api`, `vim.fn`, `vim.lsp`, jobs, …) works as
usual.

## Markup

Panels and status items never print ANSI escapes. Use tags:

```text
{{accent bold}}TODO{{/}} main.go:12  {{muted}}fix this{{/}}
```

- `{{names}}` opens a style (space-separated); `{{/}}` closes the last one.
- A tag with an unknown name is printed as text, so `{{host}}` stays as is.
- `{{{{` prints `{{` — `termocode.escape()` does this for you.

Tokens: `primary secondary muted dim white` · `accent blue green amber
yellow red magenta lavender` · `error warning info hint success` ·
`keyword function string number type comment constant variable` · `added
modified deleted untracked` · `bold italic underline faint`. Colours follow
the active theme.

## Keys

Bind any command in `~/.config/termocode/keymap.json`:

```json
{ "ctrl+alt+r": "ext:rest-client.send" }
```

The key is matched against the raw key name (no default binding needed)
and wins over a built-in binding of the same key. The palette shows the key
as the hint.

## Safety and limits

- A broken extension never blocks startup. Syntax errors, runtime errors
  and slow code are reported with a toast and in the Error Log
  (`~/.config/termocode/errors.log`); the other extensions still load.
- Each call has a time budget: 2 s for `init.lua`, 250 ms for commands,
  renders, events and callbacks. Longer work must run in a job
  (`vim.fn.jobstart`) — see `todo-tree` and `rest-client`.
- A panel render or status item that fails 3 times is switched off until
  **Extensions: Reload**.
- Extension code runs with the LuaJIT compiler off (so the budget can
  stop endless loops) and shares Neovim's Lua state with termocode. Only
  install extensions you trust.

## Samples (repo `extensions/`)

| Extension | Shows |
|---|---|
| `todo-tree` | Panel with every TODO / FIXME / HACK of the workspace (ripgrep job), click to open |
| `word-count` | Status item with the word count; click for details |
| `open-in-github` | Commands "Open in GitHub" / "Copy GitHub Link" (GitHub, GitLab, Bitbucket, Codeberg) |
| `rest-client` | `.http` / `.rest` files: panel of requests, send with curl, response preview. Port of mobocode's `http_file_parser.dart`; supports `@var = value` and `{{var}}` |
