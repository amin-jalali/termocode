# Serve tasks from this machine (runner)

Make one machine (your desktop, a server, a VM) do agent tasks for you.
You queue a task from anywhere you can reach it, for example over SSH.
The runner takes it, runs Claude Code or Codex on it, and keeps the answer.

## Start the runner

On the machine that should do the work:

```sh
cd ~/proj
termocode runner serve
```

It serves the current folder and runs at most 2 tasks at once. Serve more
folders, or more tasks at once (at most 8):

```sh
termocode runner serve --folder ~/proj --folder ~/api --pool 4
```

Keep it running in a termocode terminal, in `tmux`, or as a service.
`Ctrl+C` stops it; tasks that still run are stopped and marked failed.
Only one runner serves the queue at a time.

## Queue a task

```sh
termocode runner add "fix the flaky login test"
termocode runner add --agent codex --read-only "explain the cache layer"
echo "add a --json flag to list" | termocode runner add -
```

From your laptop, through SSH:

```sh
ssh me@server 'cd ~/proj && termocode runner add "update the README"'
```

`add` prints the task id. Options:

| Option | What it does |
|---|---|
| `--folder DIR` | The folder the task works on (default: the current folder) |
| `--agent claude\|codex` | The agent CLI (default: `claude`) |
| `--model M` | The model the CLI is asked for |
| `--read-only` | The agent only reads; it runs in the folder itself |
| `--base BRANCH` | The branch the task's worktree starts from (default: HEAD) |

## Where the work goes

- A task that may change files gets **its own worktree** and branch. Your
  checkout is never touched. The worktree shows in the
  [WORKTREES view](worktrees.md), so you can review, merge or discard it.
- A `--read-only` task runs in the folder and changes nothing.
- The agent runs on **your own** Claude Code or Codex login. The prompt goes
  on stdin, and secrets are taken out of its environment.
- A task for a folder the runner does not serve fails at once.

## Follow and stop tasks

| Command | What it does |
|---|---|
| `termocode runner list [--json]` | Every task: id, state, agent, folder, prompt |
| `termocode runner show <id> [--json]` | One task: answer, changed files, worktree, log file |
| `termocode runner cancel <id>` | Drop a queued task, or stop a running one |

States: `queued`, `running`, `done`, `failed`, `canceled`. A task that was
running when the runner died is marked `failed` on the next start. It is
never run twice.

The queue is the folder `~/.config/termocode/runner/`: one `.json` file per
task, and a `.log` file with the agent's output.
