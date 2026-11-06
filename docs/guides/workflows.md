# Workflows

A **workflow** is a small file that says: read some items from a place,
keep the ones that matter, let the AI work on each one, and write the
result back. termocode runs it for you and asks before every write.

This first version runs workflows by hand. Schedules, outside agents
and worktrees per item come in a later version.

## Where workflow files live

- In the repo: `.termocode/workflows/<name>.jsonc`. The team shares them
  and reviews them in pull requests.
- Your own: `~/.config/termocode/workflows/<name>.jsonc`.

On the same name, the repo file wins. The file name must match the
`name` in the file.

A small example, `.termocode/workflows/docs-report.jsonc`:

```jsonc
{
  "version": 1,
  "name": "docs-report",
  "title": "Docs report",
  "steps": [
    { "id": "docs", "type": "source", "from": "files", "glob": "docs/**/*.md" },
    { "id": "rep", "type": "write", "to": "file", "path": "reports/docs.md",
      "content": "{{len(steps.docs.items)}} docs" }
  ]
}
```

## Trust and writes

- A file from the repo is **not trusted** until you accept it. The first
  `r` or `d` shows the file on a card. Cancel is first. Trust is kept
  outside the repo, for these exact bytes. A changed file asks again.
- Every write asks on a card: **Deny** (first, and what Enter and Esc
  do), **Allow once**, or **Allow for this workflow**. The last one is
  kept outside the repo and only for this version of the file.
- A step with `"gate": "dry"` never writes; it only shows the write.
- No secrets in files: `${secret:NAME}` is allowed only in headers and
  env, and is read from the secret store when the step runs.

## Open Workflows

- Click the flow icon in the activity bar. It shows once the workspace
  has a `.termocode/workflows` folder.
- Run **Workflows: Open** from the command palette (`F1`).

The sidebar shows the list. Each row has a mark: `◉` running, `●` on,
`○` off, `◌` not trusted, `✗` the file has errors. A file with errors
also shows its problems in **Problems**, with the line.

The editor area shows one tab: the overview, one workflow's page, or one
run's page. In a window narrower than 120 columns the sidebar hides and
the overview lists the workflows.

## Dry run

A dry run (`d`) reads the sources for real (read-only) and runs plan and
filter steps. It does not run AI, shell or test steps, and it never
writes. The run page lists every write it would do, with its full text.

## Build by chat

Run **AI: New Workflow…** from the command palette, or press `n` in the
view. The chat opens with the start of a message. Say what the workflow
should do and press `Enter`. To change a workflow, press `c` on it: the
chat opens with its file attached.

The AI never saves a file by itself:

- It checks the file first. A file with problems never reaches you.
- A card shows the whole file (or the diff for a change) and every
  write step it will do. **Cancel** is first. **Create** writes the file
  and trusts this exact version.
- It asks you before it adds a write step.
- A dry run and a real run from the chat ask on a card too (**Deny** is
  first). In a real run, every write still asks on its own card.

## Fan out: worktrees, agents, tests, pull requests

- An `each` step runs its items **in parallel**: `"parallel": 2` (or
  `limits.parallel`), never more than the `workflows_parallel_max`
  setting.
- With `"worktree": true` every item gets its **own git worktree**, made
  from the last commit of `base` (your uncommitted edits stay out). The
  steps of the item work there. `"cleanup": "discard"` removes the tree
  of a good item; its branch stays. By default every tree is kept for
  review.
- An `agent` step runs **Codex** or **Claude Code** with your own login,
  in the item's worktree only. With `"sandbox": "worktree"` it may change
  files there; with `"read-only"` it only reads. It never gets push or
  pull request tools. The item goes to it as marked data, never as
  orders. An item that looks like an injection asks you first.
- A `pipeline` step runs a named AI profile. A `tests` step runs the
  project's test task (or its `argv`) in the item's worktree; when the
  tests fail, the step's `else` runs.
- Git writes: `pr` commits the item's worktree, pushes its branch and
  opens the pull request; `commit` only commits; `github.comment`
  comments on a PR. Each asks on its card first. Each carries a small
  run marker, so a resumed run never sends it twice. The run keeps how
  to undo it.
- `p` pauses a running workflow before its next step; `p` again goes on.

## Keys

| Key | What it does |
|---|---|
| `↑` / `↓` | move in the list, or scroll a page |
| `Enter` | open the workflow's page |
| `/` | filter by name (Esc clears) |
| `Esc` | back: run → workflow → overview |
| `r` | run now (asks; a repo file asks for trust first) |
| `d` | dry run: read for real, write nothing (asks) |
| `o` | schedule on or off (asks) |
| `e` | open the file (at the first problem) |
| `n` | new workflow by chat (**AI: New Workflow…**) |
| `c` | change this workflow by chat (the file is attached) |
| `h` | history: the newest run |
| `R` | read the files again |
| `x` | stop the run (asks; Keep running is first) |
| `p` | pause the run before its next step; `p` again continues |
| `l` | jump to the live output |
| `u` | undo a write of the run: it asks first, Cancel is first (a write with an undo recipe, up to 7 days) |
| `?` | this list |

## From the command line

These work without the IDE running:

```sh
termocode workflow list [--json]
termocode workflow show <name> [--json]
termocode workflow validate [<file>…]
termocode workflow run <name> --dry-run [--var k=v]
termocode workflow runs [<name>] [--json] [--limit N]
termocode workflow logs <run id>
termocode workflow again <name> <key>    # forget a seen item: the next run handles it again
```

A dry run from the command line skips plan steps (no AI is used). A real
run, and `stop`, need the IDE: start them in the Workflows view. Exit
codes: 0 ok, 1 failed, 2 usage, 3 needs the IDE.
