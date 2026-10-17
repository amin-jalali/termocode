# Extensions

termocode extensions are small Lua programs that run inside the embedded
Neovim. They can add palette commands, sidebar panels and status-bar items.
Why Lua-in-nvim: see [ADR 0006](adr/0006-lua-extension-host.md).

An extension can also be a separate program in **any language** that talks
JSON-RPC with termocode: see [Process extensions (Tier 2)](#process-extensions-tier-2).

## Quick start

1. Command palette → **Extensions: New Extension...** → type a name.
   termocode creates `~/.config/termocode/extensions/<name>/init.lua`,
   opens it, and loads it.
2. Edit the file, then run **Extensions: Reload**.
3. Your command shows up in the palette as **Ext: <Title>**.

Or try the samples: **Extensions: Install Sample Extensions** copies
`todo-tree`, `word-count`, `open-in-github` and `word-frequency` (a
Python process extension) into the folder (existing folders are never
overwritten).

| Palette command | What it does |
|---|---|
| Extensions: Reload | Drop everything and load the folder again |
| Extensions: Open Folder | Open the extensions folder in the file manager |
| Extensions: Show Log | Load results + every extension log / error line |
| Extensions: New Extension... | Scaffold a commented `init.lua` + `extension.json` |
| Extensions: New Process Extension (Python)... | Scaffold a Python process extension (`main.py`, `termocode_ext.py`, `extension.json`) |
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
{ "alt+w": "ext:word-count.details" }
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
  (`vim.fn.jobstart`) — see `todo-tree`.
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
| `word-frequency` | **Process extension (Python)**: panel with the most used words of the file you open or save, status item with its word count |

The `rest-client` sample is retired: termocode has a built-in HTTP client
for `.http` / `.rest` files (no curl needed; see the
[HTTP client guide](guides/http-client.md)). If you installed the sample,
delete `~/.config/termocode/extensions/rest-client`; termocode never
deletes your files.


## Process extensions (Tier 2)

A process extension is a program in any language (Python, Node, Go, Rust,
a shell script…). termocode starts it and talks **JSON-RPC 2.0** with it
over **stdin / stdout**. It gets the same things as a Lua extension:
palette commands, a sidebar panel with the same markup, status items,
toasts, prompts and pickers. Design: [ADR 0009](adr/0009-process-extensions.md).

Quick start: **Extensions: New Process Extension (Python)...** makes
`~/.config/termocode/extensions/<name>/` with `extension.json`, `main.py`
and the helper `termocode_ext.py` (standard library only). The sample
`word-frequency` uses the same helper.

### extension.json

```json
{
  "name": "word-frequency",
  "version": "0.1.0",
  "run": { "command": "python3", "args": ["-B", "main.py"], "env": { "MY_VAR": "1" } },
  "runtime": "python",
  "activationEvents": ["onStartup"],
  "contributes": { "commands": [ { "id": "refresh", "title": "Word Frequency: Refresh" } ] }
}
```

- `run` makes the folder a process extension (an `init.lua` next to it is
  ignored). `command` is looked up on `PATH`; a command with a `/` is
  relative to the extension folder. The working folder is the extension
  folder. `runtime` is only a hint for people.
- The process gets `TERMOCODE_EXTENSION`, `TERMOCODE_WORKSPACE` and
  `TERMOCODE_VERSION` in its environment.
- **When it starts:** with `"onStartup"` (or `"*"`) at termocode start.
  With `"onSave"` / `"onOpen"` on the first such event. Commands listed in
  `contributes.commands` are in the palette at once, and the first run
  starts the process (lazy start). With no `activationEvents` and no
  `contributes`, it starts at startup.
- One process per extension. **Extensions: Reload** stops every process
  and starts them again.

### Protocol

Each message is framed like LSP:

```text
Content-Length: 52\r\n
\r\n
{"jsonrpc":"2.0","method":"notify","params":{...}}
```

Requests have an `id` and get an answer; notifications do not. Ids
without a dot are prefixed with the extension name (`refresh` →
`word-frequency.refresh`), as in the Lua API. Never write anything else
to stdout; stderr goes to **Extensions: Show Log**.

termocode → extension:

| Method | Kind | Params | Notes |
|---|---|---|---|
| `initialize` | request | `protocolVersion` (1), `name`, `extensionDir`, `workspaceRoot`, `termocodeVersion`, `capabilities` | Answer within 10 s; register things after it |
| `command/execute` | request | `id` (full id), `args` | Answer within 30 s |
| `event/notify` | notification | `event` (`open`, `save`), `path` | Sent to every running extension |
| `panel/select` | notification | `panel`, `row` (1-based), `text` | Enter / click on a panel row |
| `shutdown` | request | — | Then `exit`; then stdin closes |
| `exit` | notification | — | Exit now |

Extension → termocode:

| Method | Params | Lua API |
|---|---|---|
| `registerCommand` | `id`, `title`, `hint` | `register_command` |
| `registerStatusItem` | `id`, `text`, `command` | `register_status_item` |
| `statusItem/update` | `id`, `text` | (text function) |
| `registerPanel` | `id`, `title`, `icon` | `register_panel` |
| `panel/setContent` | `id`, `lines` (markup) | (render function) |
| `notify` | `message`, `detail`, `level` (`info`/`warn`/`error`) | `notify` |
| `preview` | `title`, `body` | `preview` |
| `openFile` | `path`, `line`, `col` | `open` |
| `prompt` *(request)* | `title`, `label`, `value` → the text, or `null` when cancelled | `prompt` |
| `pick` *(request)* | `title`, `items` `[{title, hint}]` → 1-based index or `null` | `pick` |
| `readFile` *(request)* | `path` → `{ "text": … }` (max 4 MB) | — |
| `log` | `message` | `log` |

Panels and status texts use the same [markup](#markup) as Lua extensions.
Any of these may also be sent as a request; termocode answers `null`.

### Life cycle and safety

- **Never blocks the UI.** All RPC runs on background goroutines with
  time limits; a slow or stuck extension only makes its own command late.
- **Crash:** its commands, panels and status items go away, a toast says
  it restarts, and it starts again (after 0.5 s, then 1 s, then 2 s). If
  it crashes a 4th time (a run of a minute or more resets the count), it
  stays off until **Extensions: Reload** (toast + Error Log).
- **Stop:** on quit (and on reload) termocode sends `shutdown` and `exit`,
  closes stdin, waits 2 s, then kills the extension's whole **process
  group** (so children it started go too). On Linux the process also gets
  SIGTERM if termocode dies hard; everywhere, **exit when stdin closes**
  (the helpers do).
- **Files:** `readFile` and `openFile` work only inside the workspace
  (symlinks that point out are refused). This is not a sandbox: the process
  runs with your user rights and can do anything you can. Only install
  extensions you trust.
- **AI:** the AI can run extension commands only after you confirm (class
  *run*), the same as Lua extension commands.
