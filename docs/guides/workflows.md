# Workflows

A **workflow** is a small file that says: read some items from a place,
keep the ones that matter, let the AI work on each one, and write the
result back. termocode runs it for you and asks before every write.

Run a workflow by hand, or turn its schedule on and let it run by
itself. You can follow a run and answer its write cards on the laptop or
on your phone.

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
  pull request tools. With the `agent_os_sandbox` setting on, the agent
  program itself also runs inside an OS sandbox (bwrap / sandbox-exec):
  the rest of the disk is read-only. The item goes to it as marked data, never as
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

## Schedules

Add a `schedule` to the file and press `o` to turn it on. The card says
it will run by itself; Cancel is first.

```jsonc
"schedule": { "preset": "weekdays", "time": "09:00", "tz": "Europe/Berlin",
              "precheck": ["gh", "auth", "status"] }
```

- `preset` is `hourly`, `daily`, `weekdays`, `weekly` or `monthly`, or
  use `"cron": "0 9 * * 1-5"`. `tz` is a zone name; empty means this
  machine's zone.
- `precheck` runs first. If it fails, the run is skipped and the page
  says `skipped (precheck)`.
- A run that is still going when the next one is due: the next one is
  skipped. At most `workflows_parallel_max` (2) runs go at the same
  time, across all workflows.
- termocode was not running at the time: a run due in the last
  `workflows_missed_grace_min` (30) minutes still runs once; older ones
  are only recorded as missed.
- The workflow's page shows the next run. The state (on, last run, next
  run) is kept outside the repo.
- Only one termocode window owns a folder's schedules. A second window
  on the same folder runs workflows by hand only.
- To keep schedules going after a reboot, run
  `termocode service install` in the folder. It shows the service file
  (a systemd user unit on Linux, a launchd agent on macOS) and asks
  before it writes it. `termocode service status` and
  `termocode service uninstall` check and remove it. On Linux it starts
  at login; when it should also start at boot without a login, it tells
  you to run `loginctl enable-linger $USER` once. Termux on Android has
  no user services: there the command says so and writes nothing (use
  `termux-wake-lock` to keep the session alive).
- Every write still asks on its card, unless you allowed it for this
  version of the file.

## Triggers

Add `on` to the file to start the workflow when something happens
outside. Like a schedule, triggers work only while the workflow is
trusted and on (`o`).

```jsonc
"on": [
  { "github": "issue_labeled", "labels": ["agent"], "worktree": true, "base": "main" },
  { "github": "ci_failed", "branch": "main" },
  { "github": "mention", "text": "@termocode" },
  { "webhook": "deploy", "from": "slack" }
]
```

- `github` is `issue_opened`, `issue_labeled`, `pr_opened`, `mention`
  or `ci_failed`. termocode asks `gh` (your own login) every
  `workflows_trigger_poll_min` (5) minutes. `labels` keeps only items
  with one of them; `repo` ("owner/name") reads another repo; `branch`
  keeps only CI runs on that branch. A `mention` looks for its `text`
  in the text and comments of open issues and pull requests.
- The first look only notes what is already there. Old issues never
  start runs when you turn a trigger on.
- `webhook` takes `POST /hooks/<name>` on
  `127.0.0.1:workflows_webhook_port`. The port is 0 (off) until you set
  it. Put a tunnel or a reverse proxy in front to reach it from
  outside. `from` is `github`, `slack`, `linear`, `jira` or `generic`
  (any JSON with `title`, `body`, `url`, `key`, `labels`).
- Every webhook must be signed. Store the signing secret in the secret
  store as `workflow-hook.<name>` (**AI: Secret Store…**); the workflow file
  never holds it. GitHub, Slack and Linear use their own signature
  headers. Jira and other senders use `X-Termocode-Signature:
  sha256=<hmac of the body>`, or `Authorization: Bearer <secret>` when
  they cannot sign. A request without a good signature is refused; a
  hook takes at most 30 requests a minute; a sent-again event runs once.
- Each event shows a card first: what came in, from whom, and where the
  run works. Cancel is first. `"ask": false` starts the run without a
  card, like a schedule does (an admin policy that denies schedules
  makes it ask again).
- The run gets the event as vars: `trigger_kind`, `trigger_title`,
  `trigger_body`, `trigger_url`, `trigger_number`, `trigger_author`,
  `trigger_labels`, `trigger_branch`, `trigger_repo`, `trigger_key`.
  Use them in prompts as `{{vars.trigger_title}}`. This text comes from
  outside: it is never a command.
- `"worktree": true` runs the whole run in a new agent worktree from
  `base` (empty: the current commit). The worktree list shows it, and
  `trigger_worktree` and `trigger_worktree_branch` name it.
- The workflow's page lists its triggers. **Workflows: Trigger Events…**
  in the palette shows what came in and what became of it, and answers
  events that wait while another card was open.
- Every write still asks on its card, unless you allowed it for this
  version of the file.

## After a crash or restart

A run that was going when termocode ended is marked `interrupted`. When
termocode starts again it goes on from the step that was running:
finished steps stay finished, and a write that may have been sent is
looked up by its marker before it is sent again. The run page shows
`↻ Resumed`. Set `"resume_on_start": false` to keep it stopped. A
changed file is never resumed.

Items a run handled are remembered (`seen(key)` in a filter). To handle
one again: `termocode workflow again <name> <key>`.

## Notifications and history

- When a scheduled run ends you get a short note on the desktop (when
  the terminal is in the background) and on a connected phone. Set
  `"notify": ["os"]` or `["web"]` to choose; a run you start by hand
  notifies only when the file asks.
- `"notify": ["message:sum"]` sends the same summary through the
  message step `sum` (`"via": "os"`, `"web"`, `"${secret:NAME}"`, a
  secret that holds an https webhook URL, or an MCP server such as
  `"slack"` with a `"channel"`). A webhook or a server is a write to an
  outside system: the summary goes there only after you chose **Allow
  for this workflow** on that step's card.
- A message via an MCP server calls the step's own `"tool"`, or else
  the server's send tool, found in its tool list (a name with post or
  send and message, such as `slack_post_message`; its arguments name
  the channel and the text). When none is found it tries
  `slack_post_message` with the channel id; if that fails, the error
  lists the server's tools, so you can set `"tool"`.
  Connect the server with writes allowed. Text that goes to a webhook
  or a server has secrets and keys hidden first.
- Runs are kept for `workflows_history_days` (30) days and deleted when
  termocode starts. `h` opens the newest run;
  `termocode workflow runs --json` lists them.

## Undo, budgets and the audit file

- The run page lists the writes that can be taken back (an MCP write
  with an `undo` tool, a pull request, a commit). `u` undoes the first
  one that is not undone yet. The card shows what it will do; Cancel is
  first. Undo works for 7 days.
- `workflows_day_budget_usd` caps what all workflows spend on AI in one
  day (0 = no cap). A run that reaches it waits before its next step and
  asks: **Stop** (first) or **Continue**.
- In the file, `"limits": { "budget_usd": 5, "item_budget_usd": 1.5 }`
  caps one run and one item the same way. Each budget asks once; after
  **Continue** it does not ask again in that run.
- Each run keeps an `audit.jsonl` next to its run file: one line per
  step event and per answer on a write card.

## On your phone

With web access on, **More → Workflows** lists the workflows and their
runs, the step that waits for you and the last lines of a running run.
A write card shows up like the other cards: **Deny** (first) or **Allow
once**. "Allow for this workflow", turning a schedule on and editing a
file stay on the laptop. You can stop a run from the phone; it asks
first.

In a browser on a wide screen, **Workflows** in the activity bar lists
the same in the side bar. A run opens as an editor tab with its counts,
its last lines and the write card that waits for it (**Deny** first).
**Stop** asks first, **Cancel** first.

## Admin policy

An admin can limit workflows on a machine with
`/etc/termocode/workflows-policy.json` (macOS:
`/Library/Application Support/termocode/workflows-policy.json`). You can
keep your own in `~/.config/termocode/workflows-policy.json`.

```json
{ "deny": [ { "step": "agent" }, { "write": "http" }, { "schedule": true },
            { "workflow": "nightly-*" } ] }
```

A denied workflow does not run (a dry run still works) and a denied
schedule stays off. A policy file that cannot be read blocks every
workflow.

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
