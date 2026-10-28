# ADR 0019 — Connected AIs are providers; a step picks a provider and a model

Status: **Accepted** (2026-10-28)

## Context
Before this, a profile step ran on a *slot*: a name in `config.json`
(`ai_models`) that holds a provider kind, a model id and a base URL. To
use a new model the user had to add a slot, often by hand in JSON, and
to use Claude Code or Codex with their own plan they had to install and
log in to the CLI in a terminal tab, then click a toast to go back to
the form.

The goals:
- Connect an AI in one card, without a terminal, JSON or the word
  "slot".
- Every model of every connected AI shows up in every step's model list.
- Keys stay in the secret store (ADR 0013); sign-ins stay inside the
  vendor CLI; termocode never reads a CLI's credential files.
- Old `profiles.json` and `ai_models` keep working.

Alternatives on the table:
- **Auto-named slots** for each picked model: a slot name cannot hold
  `/` or `:` (so `ollama/llama3.2:3b` does not fit), and it would fill
  `config.json` with rows the user never asked for.
- **Keep the terminal tab for install and login**: it leaves the card,
  needs a "go back" toast, and the user must type commands.
- **A provider list plus a model per step** (chosen).

## Decision
- **Connected AIs** live in `~/.config/termocode/ai/providers.json`
  (no secrets): one entry per kind with a method `key`, `cli`, `local`
  or `url`. Without the file the list is derived once from what is
  already set up (a stored key, a ready CLI, a local `ai_provider`).
  A key that comes only from an environment variable shows as "via
  environment".
- **A step names `provider` + `model`.** At run time it gets the
  synthetic slot `<provider>/<model>`, which can never clash with a
  file slot name. A sign-in step is the existing `cli` step plus an
  optional `model` (passed as `--model` / `-m`). `slot` stays valid; a
  step may not set both `slot` and `provider`.
- **Models load by themselves.** Each kind's list is fetched from its
  own API and cached in `$XDG_CACHE_HOME/termocode/ai/models.json`
  (no secrets). Itermoc refreshes an entry older than one hour when it
  opens; **Refresh** always fetches. The two CLIs have no list command,
  so they use built-in lists plus "Other model id…".
- **One Add AI card** (`internal/addai`, a pure state machine; the app
  runs its requests). An API key is checked by listing models before it
  is saved; a local server is looked for; a sign-in installs the CLI
  into termocode's own tools folder (`internal/lspinstall`, no `-g`, no
  sudo) and runs the CLI's own login under a pty (`internal/clilogin`),
  showing the link, the code prompt or the device code in the card.
- **One method per provider.** Adding the other method replaces the
  first after a confirm. **Disconnect** removes the provider, its key
  and its cached models, and never runs the CLI's logout.
- **Itermoc's AI PROVIDERS** section lists the connected AIs; the
  profile form's model picker lists all their models. A step on an AI
  that is not ready shows **Fix**, and Itermoc's "needs you" rows show
  **Add** / **Sign in**; all of them open the Add AI card at that AI.
- The old Install / Log in pills, the terminal tab and the toast that
  parked the form are removed. Slots stay as the expert path
  (**AI: Add Model Slot…**, `ai_models` in `config.json`).
- The chat's own model (`ai_provider` / `ai_model`) is not changed by
  this decision.

## Consequences
- **+** A new user connects an AI and picks a model without a terminal
  or a config file.
- **+** A new model of a connected AI is in the list after a refresh,
  with no config change.
- **+** Profiles stay portable: a provider that is not connected on this
  machine is a "needs you" row, not a file error.
- **−** CLI login output is not a stable interface; the card reads only
  the first `https://` link, a code prompt and the CLI's own status
  command, and falls back to "Try again" / "Use an API key".
- **−** The CLI model lists are built in and can fall behind; "Other
  model id…" covers the gap.
- **−** Two places name a model now: a step's `provider` + `model`, and
  the expert slots in `config.json`.
