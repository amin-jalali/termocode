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
| `h` | history: the newest run |
| `R` | read the files again |
| `x` | stop the run (asks; Keep running is first) |
| `l` | jump to the live output |
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
```

A dry run from the command line skips plan steps (no AI is used). A real
run, and `stop`, need the IDE: start them in the Workflows view. Exit
codes: 0 ok, 1 failed, 2 usage, 3 needs the IDE.
