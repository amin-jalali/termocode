# Move in from another agent tool

Another agent tool is closing, or you leave it? Most of them let you
export your data. `termocode import` reads that export and turns it into
termocode's own files, so your scheduled tasks, setup lines, agent notes
and open tasks come with you.

```
cd ~/proj
termocode import ~/Downloads/export        # a folder or one file
```

## What it brings over

| In the export | In termocode |
|---|---|
| A scheduled task (a prompt and a schedule) | `.termocode/workflows/<name>.jsonc`: a workflow with one agent step |
| Setup lines of a repo | `.termocode/worktrees.json` `"setup"`: run in each new worktree |
| Notes or memory for the agent | `AGENTS.md` (made, or the new notes added at the end) |
| Tasks of a board (title, text, status) | `.termocode/imported/tasks.md`: a checklist, done tasks ticked |

It reads JSON, JSON Lines (`.jsonl`), Markdown notes (`AGENTS.md`, or any
`.md` in a `memory` or `notes` folder) and `setup*.sh` scripts. It finds
things by the shape of the data, so it works with most exports. A file it
does not know is listed under **Not used**.

## It asks first

1. It lists every file it would write, with a short preview.
2. It asks `Write these 4 file(s)? [y/N]`. **No** is the default.
3. Only then it writes.

- It never overwrites a file. A taken workflow name gets `-2`. An
  existing `worktrees.json` is left alone; it shows the setup lines to
  add by hand. `AGENTS.md` only gets the notes it does not have yet.
- `--yes` skips the question (for scripts).
- `--dir d` imports into another folder.

## After the import

- **Workflows** are repo files, so they are **not trusted** until you
  accept them in **Workflows: Open**. A schedule stays **off** until you
  press `o` on it.
- The agent step only **reads** (`"sandbox": "read-only"`). To let it
  change files, put the step in an `each` step with `"worktree": true`
  (see [Workflows](workflows.md)).
- `--agent claude-code` uses Claude Code instead of Codex.
- A schedule it does not understand is left out; the workflow then runs
  by hand, and the preview says why.
- **Setup lines** run only after you trust them on a card.
- Text from the export stays text: a `{{` in a prompt is written as `{ {`,
  so it never turns into a workflow template.
