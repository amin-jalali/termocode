# ADR 0015 — Agent pipelines run as a DAG inside the UI process

Status: **Accepted** (2026-10-18)

## Context
One chat message went to one agent: one model, one system prompt, one
tool set. Users asked for chains: "step 1 a Claude agent reviews, its
answer goes to step 2 a GPT agent that fixes, then show the answer".
They also want to see who did what, and what each step cost.

A parallel graph of agents was put off on 2026-10-16 ("not now"), but
the data model should not block it later: a planner that splits a task
into steps that run in waves, and an agents panel that draws the graph.

Constraints:
- The UI process is the persistent session (ADR 0007). A dropped SSH
  connection must not stop a run, and `termocode attach` must show it.
- Every tool call must keep the same gates as the plain chat: validate,
  gate, confirm, review card, `ai_auto_approve` classes, audit line.
- Config files never break startup (ADR 0005).

Alternatives on the table:
- **Each agent as its own process**, talking through files or pipes.
  More isolation, but a second process to supervise, a file format as
  IPC, and the gates and review card would have to cross a process
  boundary.
- **A hard-coded linear chain** (a list of steps, no graph). Simplest
  today, but a parallel phase would need a second model and a rewrite.
- **A DAG in the UI process** (chosen).

## Decision
- **One pure data model**: `internal/agentdag` has `Run`, `Node`, `Edge`,
  `Validate` / `Normalize`, `Layers` (Kahn layering into waves), `Ready`
  and `Chain`. No I/O, no imports of `ai` or `app`. A linear pipeline is
  a run whose edges form one path. Limits: 12 nodes, 6 layers.
- **One scheduler**: `internal/agentrun` starts every ready node as its
  own `ai.Agent` (the same type as the plain chat) in its own goroutine,
  at most `ai_parallel_max` at once (default 1). A node's input is the
  user message plus the final text of the nodes it depends on, under a
  `### Output of step "<title>"` line, capped at 64 KB. A failed node
  blocks its dependents and stops the run; `Cancel` stops every node. A
  review locker lets one node at a time show the review card.
- **The app builds each agent** (`Options.Build` in
  `internal/app/ai_runs.go`): the provider of the node's model slot, the
  chat's tools narrowed by the node's tool rule (a rule only removes
  tools), and the system prompt. The plain chat's gates stay as they are.
- **Profiles and model slots are config**: `internal/ai/profiles.go`
  merges built-in profiles, `~/.config/termocode/ai/profiles.json` and
  the project's `.termocode/ai-profiles.json`. Slots come from
  `config.json` `ai_models`; keys come from the credentials by provider
  kind, never from a profile file. A project profile needs the user's OK
  (trust card) before it runs.
- **The chat is the v1 view**: a step card per node and a run row with
  time, tokens, cost and context use. The default profile has one step
  and takes the plain chat path, so its transcript does not change.
- **State at step boundaries only**: the chat file keeps the step cards
  (`ai.StoredStep`); a run snapshot with ids and states (no prompts, no
  outputs) is written to `<state>/termocode/agent-runs/` when a step
  ends. Nothing is written per token.
- **Usage parsing in the providers**: Anthropic and OpenAI-style streams
  report token counts on the last chunk; `internal/ai/pricing.go` turns
  them into dollars from a dated table plus `ai_prices`.

## Consequences
- **+** A run lives in the session process: detach, reconnect, and the
  run is still going or done, with its cards.
- **+** No second process and no file as IPC. Tools, confirms, the
  review card, restore points and the audit log work the same for every
  step.
- **+** Parallel waves and an agents panel are a later view on the same
  `agentdag.Run`, not a rewrite.
- **−** A crash or restart of the UI process ends a running run. The
  chat marks it `run interrupted` and offers "Retry from step N" with the
  saved outputs of the finished steps.
- **−** All steps share the UI process's memory and CPU. The caps (12
  nodes, `ai_parallel_max` at most 8, input cap, `max_steps`) keep this
  small.
- **−** New config to keep stable: `ai/profiles.json`,
  `.termocode/ai-profiles.json`, `ai_models`, `ai_parallel_max`,
  `ai_prices`, `ai_context_windows`. The built-in price table needs
  updates; a model with no price shows tokens and no dollars.
- Follow-ups: a planner node with `quick` / `deep` categories, a budget
  pause, and the agents panel.
