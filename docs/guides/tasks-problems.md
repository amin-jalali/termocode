# Tasks and problems

A **task** is a command you run often: build, test, lint, start a server.
termocode runs each task in its own terminal tab. When a task prints
errors like `main.go:12:5: undefined: x`, they show up in the **Problems**
panel and in the editor gutter.

## Run a task

1. Press `Alt+R` (or run **Tasks: Run Task...**).
2. Pick a task. termocode lists tasks from three places:
    - your `.termocode/tasks.json`,
    - your `commands.json` user commands (shown as `User: <title>`),
    - tasks it detects from project files.
3. The task runs in a new terminal tab. The tab's dot turns green on success
   and red on failure.

Press `Alt+Shift+R` to run the last task again.

Other commands: **Tasks: Run Build Task**, **Tasks: Run Test Task**,
**Tasks: Terminate Task**, **Tasks: Clear Task Problems**.

## Detected tasks

| Project file | Tasks |
|---|---|
| `go.mod` | `go: build`, `go: test`, `go: vet`, `go: run` |
| `package.json` | one task per script, e.g. `npm: dev` (uses npm, pnpm, yarn or bun, from the lock file) |
| `Makefile` | `make`, plus one per target |
| `Cargo.toml` | `cargo: build`, `cargo: test`, `cargo: run`, `cargo: clippy` |
| pytest config | `pytest: run tests` |
| `justfile` | one per recipe |

## Write your own tasks

1. Run **Tasks: Configure Tasks**. termocode creates
   `.termocode/tasks.json` with the detected tasks as a start, and opens it.
2. Edit it. The format is close to VSCode:

```json
{
  "version": 1,
  "tasks": [
    {
      "label": "build",
      "command": "go build ./...",
      "group": { "kind": "build", "isDefault": true },
      "problemMatcher": "$go"
    },
    {
      "label": "lint",
      "command": "golangci-lint",
      "args": ["run"],
      "cwd": "backend",
      "env": { "GOFLAGS": "-mod=mod" },
      "problemMatcher": "$generic"
    }
  ]
}
```

| Field | Meaning |
|---|---|
| `label` | Name in the list |
| `command` | Shell command line |
| `args` | Extra arguments (quoted for you) |
| `cwd` | Folder to run in (relative to the workspace root) |
| `env` | Extra environment variables |
| `group` | `"build"` or `"test"`, or `{ "kind": "build", "isDefault": true }` |
| `problemMatcher` | `$go`, `$gcc`, `$tsc`, `$rustc`, `$pytest`, `$eslint-compact`, `$generic`, or `$none` |

The default task of the `build` group runs with **Tasks: Run Build Task**.
The same goes for `test`.

## The Problems panel

The Problems panel lists every diagnostic: from language servers and from
task output. It is grouped by file.

Open it with `Alt+M` or `Ctrl+Shift+M`, with **View: Problems**, or by
clicking the error / warning counts in the status bar.

| Key (Problems focused) | Action |
|---|---|
| `j` / `k` | Move |
| `Enter` or `o` | Open the problem in the editor |
| `Space` | Fold / unfold the file |
| `h` / `l` | Collapse / expand |
| `c` | Collapse or expand all |
| `e` / `w` / `i` | Hide or show errors / warnings / info |
| `r` | Refresh |

In the editor, `F5` and `Shift+F5` jump to the next and previous problem.

## Open file links in a terminal

Any `file:line` or `file:line:col` text printed in a terminal tab is a link.
Click it to open that spot. Or run **Terminal: Open File Link...** to pick
from the links in the active tab.

## Keys at a glance

| Key | Action |
|---|---|
| `Alt+R` | Run a task |
| `Alt+Shift+R` | Rerun the last task |
| `Alt+M` or `Ctrl+Shift+M` | Show Problems |
| `F5` / `Shift+F5` | Next / previous problem |

## Troubleshooting

**My task is not in the list**
:   Check the `label` in `.termocode/tasks.json`. If the JSON is broken,
    termocode shows an error toast and skips the file.

**Errors do not show up in Problems**
:   Add a `problemMatcher` to the task. Without one, output is not scanned.
    The matcher reads `path:line:col: message` lines.

**`Alt+R` does nothing**
:   Your terminal may not send `Alt`. Use **Tasks: Run Task...**.

**What happened to `commands.json`?**
:   It still works. Each entry shows up as a `User: <title>` task.
