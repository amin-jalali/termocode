# Debug a program

termocode has a **Run and Debug** view, like VSCode. It uses the Debug
Adapter Protocol (DAP) through `nvim-dap`.

Supported out of the box:

| Language | Debug adapter |
|---|---|
| Go | `dlv` (Delve) |
| Python | `debugpy` |
| JavaScript / TypeScript | `js-debug` (needs `node`) |

## Before you start: install the adapter

Run **DAP: Install Adapter…** and pick yours. termocode installs it in its own
tools folder. See [Install language servers and debuggers](language-servers.md).

## Debug in five steps

1. Open the **Run and Debug** view: click the **Run** icon in the activity bar,
   or run **View: Run and Debug**.
2. Pick a configuration in the **CONFIGURATIONS** section. termocode finds some
   for you, for example *Go: Debug package* or *Python: Debug current file*.
3. Open your code and press `F9` on a line to add a breakpoint. A `●` appears
   in the gutter.
4. Press `Alt+F5` (or click **▶ Start**). The program runs and stops at the
   breakpoint. The status bar shows `● DEBUG`.
5. Step through the code and look at **VARIABLES** and **CALL STACK**.
   Press `Shift+Alt+F5` to stop.

## Step through code

| Key | Action |
|---|---|
| `Alt+F5` | Start, or continue when paused |
| `F10` | Step over |
| `F11` | Step into |
| `Shift+F11` | Step out |
| `Shift+Alt+F5` | Stop |

When the session is live, the toolbar shows buttons for continue / pause,
step over, step into, step out, restart and stop. You can click them.

Palette commands: **Debug: Pause**, **Debug: Restart**,
**Debug: Run Without Debugging**, **Debug: Select Configuration...**.

## Breakpoints

- `F9` toggles a breakpoint on the current line.
- **Debug: Add Conditional Breakpoint...** stops only when an expression is true.
- **Debug: Add Logpoint...** prints a message instead of stopping.
- **Debug: Remove All Breakpoints** clears them all.

Breakpoints are saved in `~/.config/termocode/breakpoints.json`. They come
back next time. If the file changed, termocode looks for the same line text
and moves the breakpoint with it.

## Watch expressions

Run **Debug: Add Watch Expression...**, or press `a` in the Run and Debug view.
The **WATCH** section shows the value each time the program stops.

## Debug Console

Run **View: Debug Console**. It is a tab in the bottom panel.

- It shows the program's output.
- Type an expression and press `Enter` to evaluate it in the paused frame.
- `↑` / `↓` go through your history. `Ctrl+L` clears the console.

## Your own configurations: `launch.json`

Run **Debug: Open launch.json** (or click the ⚙ in the toolbar). If the file
does not exist, termocode creates `.termocode/launch.json` with the detected
configurations as a start.

The format is the same as VSCode. Comments and trailing commas are allowed.

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Debug server",
      "type": "go",
      "request": "launch",
      "program": "${workspaceFolder}/cmd/server",
      "args": ["--port", "8080"]
    },
    {
      "name": "Run dev server",
      "type": "shell",
      "command": "npm run dev"
    }
  ]
}
```

- `type` is the debug adapter: `go` / `delve`, `python` / `debugpy`,
  `node` / `pwa-node`.
- `type: "shell"` is a plain command. It runs in a new terminal tab, with no
  debugger.
- Variables such as `${file}`, `${workspaceFolder}` and `${fileDirname}` work.

Your `launch.json` entries come first in the list. Auto-detected entries
with the same name are hidden.

## Auto-detected configurations

| Project file | Configurations |
|---|---|
| `go.mod` | Go: Debug package · Go: Debug current file · Go: Run package · Go: Test |
| `pyproject.toml`, `requirements.txt`, `setup.py`, `main.py` | Python: Debug current file |
| `package.json` | Node: Debug current file · npm start |
| `Cargo.toml` | cargo run |
| `pubspec.yaml` | Dart: run |
| `Makefile` | make |

## Keys at a glance

| Key | Action |
|---|---|
| `F9` | Toggle breakpoint |
| `Alt+F5` | Start / continue |
| `Shift+Alt+F5` | Stop |
| `F10` / `F11` / `Shift+F11` | Step over / into / out |

In the Run and Debug view:

| Key | Action |
|---|---|
| `j` / `k` | Move |
| `Enter` or `Space` | Run the item: pick a config, open a frame or a breakpoint, expand a variable |
| `←` / `→` | Collapse / expand |
| `a` | Add a watch expression |
| `x` | Remove the breakpoint or watch under the cursor |
| `r` | Run the configuration without debugging |
| `Esc` | Back to the editor |

## Troubleshooting

**"install dlv" (or debugpy) message**
:   The adapter is missing. Click the install hint in the view, or run
    **DAP: Install Adapter…**.

**`F11` makes my terminal full screen**
:   Your terminal uses `F11`. Use **Debug: Step Into** from the palette.

**`Shift+Alt+F5` does nothing**
:   Some terminals do not send it. Use **Debug: Stop** from the palette.

**The program does not stop at my breakpoint**
:   Check that the right configuration is selected. *Debug current file*
    debugs the file in the active tab, so open the right file first. Read the
    **Debug Console** for adapter errors.

**Breakpoint moved to another line**
:   The file changed outside termocode. termocode looked for the same line
    text. Check it and press `F9` again if needed.
