# ADR 0021 — Workflows run in the UI process; files in the repo; state outside

Status: **Accepted** (2026-11-05)

## Context
Users repeat the same chain of work every day: read new items from a
system (issues over MCP, pull requests, a shell command, an HTTP API),
let the AI look at them, do the work per item in a worktree, then write
the result back (a comment, a status, a draft PR, a file, a message).
Today each step is done by hand, or with a pipeline (ADR 0015) that
knows nothing about outside items and never writes to outside systems.

Constraints:
- A run must go on when the SSH link drops or the laptop closes, and
  `termocode attach` must show it (ADR 0007).
- Every write to an outside system is a risk. The user must see it and
  allow it; a repository must never pre-approve its own writes.
- Items from outside systems are untrusted data, never instructions.
- No secret may sit in a file that travels with the repository.
- Config files never break startup; unknown keys are kept and warned
  (ADR 0005).

Alternatives on the table:
- **A separate workflow daemon.** It could run without a session, but
  it would need the gates, cards, worktrees, MCP connections, secrets
  and the audit log over IPC, and a second process to supervise.
- **Workflows as agent pipeline profiles.** A profile is a graph of AI
  steps; sources, filters, per-item fan-out and gated writes do not fit
  it, and a profile has no file hash a trust card could bind to.
- **YAML files.** Familiar, but a new parser and a second config
  dialect next to `profiles.json`, `mcp.json` and `config.json`.
- **JSONC files in the repo, run in the UI process, state outside the
  repo** (chosen).

## Decision
- **The file.** `.termocode/workflows/<name>.jsonc` in the repository
  (shared with the team, reviewed in PRs); private ones in
  `~/.config/termocode/workflows/`. JSONC through `internal/jsonc`, the
  same as our other config files. `"version": 1`; a higher version is
  refused with "made by a newer termocode"; unknown keys are warned,
  never fatal. Steps are `source`, `plan`, `filter`, `each`, run steps
  (`ai`, `agent`, `pipeline`, `tests`, `shell`) and `write`; any step
  may have `when` / `else`. Limits: 40 steps, 3 nested `each`, 500
  items, 32 KB prompt, 1 KB expression, 256 KB file.
- **The parser is pure.** `internal/workflow` parses and validates a
  file with no I/O beyond the bytes it is given. Errors name the line
  and the field in plain words, are at most 512 characters, and never
  echo a value that may be a secret.
- **Secrets.** A literal that looks like a key is refused. `${cmd:}` is
  refused anywhere. `${secret:NAME}` is allowed only in `headers`,
  `env` and `via`, and the adapter resolves it at run time. Templates
  (`{{…}}`) are refused in `argv`, so item text never becomes a command
  argument.
- **Where it runs.** In the UI process, behind the session server, like
  pipeline runs. A dropped link changes nothing. A later service unit
  starts the session at login for 24/7 schedules.
- **Trust and writes.** A file from the repository is untrusted until
  the user accepts it on a trust card; the hash is stored outside the
  repository, and a changed file asks again. Every write asks by
  default (Deny first; Enter and Esc deny). "Allow for this workflow"
  is stored outside the repository and bound to the file hash. Dry run
  never writes.
- **State outside the repo.** Run files, the seen ledger, the write
  ledger, the allow-list and schedule state live under
  `<state>/termocode/workflows/<workspace key>/`.
- **Items are data.** Each item reaches a prompt only as a fenced data
  block, with a system sentence that says so.
- **Kind numbers.** Workflows own confirm, picker and prompt kinds
  3500–3549 in `internal/app` (3540–3549: the write gate card).
- **Settings.** `workflows_enabled`, `workflows_parallel_max`,
  `workflows_day_budget_usd`, `workflows_missed_grace_min`,
  `workflows_history_days`, `workflows_wait_hours`,
  `workflows_agent_sandbox`. The AI may change the limits after a card;
  it never turns workflows on or off and never changes the agent
  sandbox.

## Consequences
- One more file format to keep stable: version 1 is read forever, and
  a schema change bumps the version.
- The runtime reuses the session server, gates, cards, worktrees, MCP
  manager, secret store and audit log; it adds no process and no
  network listener.
- A workflow cannot run while the session server is down; the service
  unit covers reboots.
- The parser, the expression language, the write ledger and the source
  adapters are separate packages under `internal/workflow`, each with
  table tests and fuzz tests, so they can be built and checked one by
  one.
