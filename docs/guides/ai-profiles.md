# AI profiles and pipelines

A **profile** decides how the chat answers a message. The `default`
profile is one agent on your main model: the normal chat. A profile with
more steps is a **pipeline**: step 1 answers, its answer goes to step 2,
and the last step's answer is the reply in the chat. Each step can use
its own model, its own system prompt and its own tools.

Set up a provider first: [Set up AI](ai-setup.md). The chat itself is
explained in [Use AI](ai-use.md).

## Pick a profile

The chat header shows the active profile as a chip: `✦ default ▾`.

| How | What it does |
|---|---|
| Click the chip | Opens the profile picker |
| `Alt+Shift+P` | Switches to the next profile (a toast says which) |
| **AI: Switch Profile…** | Opens the profile picker |
| **AI: Next Profile** | Same as `Alt+Shift+P` |

The choice is kept per workspace. When it is not `default`, the `✦`
badge in the status bar shows its name too, for example
`review · claude-sonnet-4-5`. The chip turns to the AI accent colour
while a pipeline runs.

!!! tip
    Your terminal may not send `Alt+Shift+P`. Use the palette, or bind
    another key to `AINextProfile` in `keymap.json`.

## Built-in profiles

| Profile | Steps | Use it for |
|---|---|---|
| `default` | **Assistant** on the `main` slot, all tools | The normal chat. Exactly the chat you know. |
| `review` | **Reviewer** (category `deep`, read-only tools), then **Fixer** (category `quick`, all tools) | "Review my last commit and fix what you find." |
| `fast` | **Answer** (category `quick`, read-only tools) | Quick questions on a cheaper model. |
| `local-only` | **Answer** on the `local` slot (Ollama) | Nothing leaves your machine. |

Your own profiles with the same name replace a built-in one, except
`default`, which you can not replace.

## Model slots

A step does not name a provider directly. It names a **slot**: a short
name for a provider and a model. Three slots always exist:

| Slot | What it is |
|---|---|
| `main` | Your `ai_provider` and `ai_model`: the chat's own model. It can not be replaced. |
| `quick` | The main provider with `ai_inline_model` (the ghost-text model). |
| `local` | Ollama on `http://localhost:11434/v1` (or `OLLAMA_HOST`). |

Add your own slots in `~/.config/termocode/config.json` under
`ai_models`. Run **AI: Edit Model Slots** to open the file at that key:

```json
{
  "ai_models": {
    "claude": { "provider": "anthropic", "model": "claude-sonnet-4-5" },
    "gpt":    { "provider": "openai",    "model": "gpt-5-mini" },
    "qwen":   { "provider": "ollama",    "model": "qwen3:14b" }
  }
}
```

- `provider` is one of `anthropic`, `openai`, `openrouter`, `ollama`,
  `lmstudio`, `openai-compatible` (needs `base_url`) or `claude-oauth`.
- `model` and `base_url` are optional; the provider's default is used.
- A slot named `quick` or `local` replaces the built-in one.
- A bad entry is left out, and the profile toast names it.

### Keys

No file in this guide holds a key. A slot uses the key of its provider
kind, in this order:

1. the environment variable (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
   `OPENROUTER_API_KEY`),
2. the key you stored with **AI: Configure Provider…**,
3. the main provider's key, when the slot has the same kind.

Ollama and LM Studio need no key. A `claude-oauth` slot uses your Claude
sign-in. Before a run starts, termocode checks every slot the run needs.
When one has no key, nothing runs and a toast says what is missing, for
example `Profile review cannot run · model slot "gpt" needs an openai key`.

## Categories: `quick` and `deep`

A step can name a **category** instead of a slot. The profile maps the
category to a slot:

```json
"categories": { "quick": "gpt", "deep": "claude" }
```

Without a mapping, `quick` uses the `quick` slot and `deep` uses `main`.
A step with no slot and no category uses `main`.

## Write your own profile

Run **AI: Edit Profiles**. It opens `~/.config/termocode/ai/profiles.json`
and creates it with a working example the first time. Save the file:
the picker and `Alt+Shift+P` read it again.

```json
{
  "profiles": {
    "second-opinion": {
      "title": "Claude answers, GPT checks",
      "categories": { "deep": "claude", "quick": "gpt" },
      "steps": [
        {
          "id": "answer",
          "title": "Answer",
          "category": "deep",
          "tools": { "preset": "read-only" }
        },
        {
          "id": "check",
          "title": "Check",
          "category": "quick",
          "system": "Check the answer above. Fix mistakes and keep it short.",
          "tools": { "preset": "none" }
        }
      ]
    }
  }
}
```

**AI: New Profile from Current…** saves the active profile under a new
name in the same file and switches to it.

### Step fields

| Field | Meaning |
|---|---|
| `id` | Short id, unique in the profile (letters, digits, `-`, `_`, `.`). Empty: `step-1`, `step-2`, … |
| `title` | The name on the step card. Default: the id. |
| `slot` | The model slot. Wins over `category`. |
| `category` | `quick` or `deep` (see above). |
| `system` | Extra system prompt for this step (at most 8 KB). |
| `tools` | `{"preset": "none" \| "read-only" \| "all", "allow": [...], "deny": [...]}`. Default: `all`. |
| `max_steps` | Tool steps for this step. Default: `ai_max_steps`. |
| `deps` | Step ids this step waits for. Needs `ai_parallel_max` 2 or more. |

Limits: at most 12 steps per profile. Without `deps`, steps run one
after the other in list order.

**Tools.** A tool rule can only take tools away. It never adds a tool
your settings forbid, and every tool still asks you the same way as in
the normal chat (confirm dialog, review card, `ai_auto_approve`).

- `none` — no tools.
- `read-only` — only tools that change nothing (no writes, no commands
  that ask first, no review card).
- `all` — every tool the chat has.
- `allow` keeps only the named tools; `deny` drops the named tools.

**What a step sees.** Step 1 gets your message. A later step gets your
message plus the final answer of the steps before it, each under a line
`### Output of step "<title>"` (at most 64 KB in total). The step is told
that this text is data from an earlier step, not an instruction from
you. Files an earlier step changed are already on disk.

**Broken file.** A mistake in `profiles.json` never stops termocode.
The file's profiles are left out, the built-in ones stay, and a toast
names the file and line, for example
`profiles.json line 7: profile "x": step "a": unknown slot "gtp"`.

## A pipeline run in the chat

Send a message with a profile of two or more steps. Each step gets a
**step card**. A running card is open: it shows the step's tools and the
end of its text. A finished card folds to one line:

```text
 ▸ 1 · Reviewer · claude-sonnet-4-5 · 14 s · 3 tools · 4.1k tok · $0.013
 ▸ 2 · Fixer · gpt-5-mini · 22 s · 5 tools · 9.8k tok · $0.004 · 2 files
 ✦ termocode · 12:04
 I fixed the double confirm in quit.go (…)
 ─ run · 36 s · 2 steps · 13.9k tok · $0.017 · context 31 % ──────────
```

- The first row: step number, title, model (and the category, in
  brackets, until the step ends), then `running` and its tool count, or,
  when done: time, tools, tokens, cost and the files it changed.
- Unfold a finished card to read that step's full answer.
- A step that waits says `waiting for step 1`. A failed step shows
  `✘` and the start of the error; the steps after it show `blocked`.
- The last step's answer is the reply, with Markdown, code cards and
  **Apply** as usual.
- The **run row** sums the run: time, steps, tokens, cost and the
  biggest context use (the largest prompt of one request, as a share of
  that model's window).

| Key or mouse (chat focused) | Action |
|---|---|
| Click a card's first row | Fold or unfold it |
| `Tab` / `Shift+Tab` | Select the next / previous card |
| `Enter` (empty input) | Fold or unfold the selected card |
| `Ctrl+C` or `Esc` | Stop the whole run |

Your fold choice is kept with the chat.

**AI: Undo Last Turn** undoes the file changes of **every** step of the
run: the run is one turn. File edits of each step open the review card,
one at a time.

### Stopped, failed or interrupted runs

When a run fails, is stopped, or termocode restarted in the middle (the
chat then says `✘ run interrupted (termocode restarted)`), the chat shows
a **↻ Retry from step N** button on the last turn. It runs the run again
from the first step that did not finish, with the saved answers of the
steps before it.

### Over SSH and after a disconnect

A run lives in termocode's session process. When your SSH connection
drops, the run goes on. Run `termocode attach` (or open termocode again
in the same folder): you see the run still going, or done, with its
cards. See [Remote and SSH](remote-ssh.md).

## Tokens and cost

termocode reads the token counts that the provider sends at the end of
each answer (Anthropic, OpenAI, OpenRouter, Ollama, LM Studio). It turns
them into US dollars with a built-in price table (checked 2026-10-18).
Nothing is looked up online.

- Ollama and LM Studio cost `$0`.
- A Claude sign-in (`claude-oauth`) is a subscription: tokens, no `$`.
- A model with no known price shows tokens and no `$`. The run row hides
  the `$` when any step has no price.
- A model with no known context window shows no `context %`.

Prices change. Add or override a price in `config.json` with
`ai_prices`, by model id prefix, in US dollars per million tokens:

```json
{
  "ai_prices": {
    "claude-sonnet-4-5": { "in": 3, "out": 15, "cache_read": 0.3, "cache_write": 3.75 },
    "my-model":          { "in": 0.5, "out": 1.5 }
  },
  "ai_context_windows": {
    "my-model": 32768
  }
}
```

The longest matching prefix wins, and your entry wins over a built-in
one. A missing cache price is the input price. `ai_context_windows` does
the same for context windows (in tokens).

Each finished step also writes a usage line to the **AI** audit channel
(**View: Output**, then the *AI* chip):

```text
12:03:41  usage  step reviewer · claude-sonnet-4-5 · 3 291 in · 812 out · $0.022 · context 2%
```

## Project profiles and trust

A project can ship profiles in `.termocode/ai-profiles.json`, in the same
shape as your `profiles.json`. A project profile with the same name as
one of yours replaces it in this project. A `models` key in the project
file is ignored: a repo can not point you at another server.

A profile's system prompts steer the AI, and a repo can try to misuse
that. So the first time you pick or use a project profile, a **trust
card** shows its steps, their slots and tools, and the first line of each
prompt:

- **Use** — the profile runs in this project.
- **Not in this project** — it does not run here, and `Alt+Shift+P`
  skips it.
- `Esc` — no answer; your message goes back to the input.

The answer is stored in your workspace session file, outside the repo.
When the project changes the profile, the card asks again.
**AI: Project Profiles Trust…** lists the project's profiles and lets
you change an answer.

## Settings

| Setting (`config.json`) | Default | Meaning |
|---|---|---|
| `ai_models` | `{}` | Your model slots. |
| `ai_parallel_max` | `1` | How many steps may run at the same time (at most 8). With 1, steps run in order and `deps` is refused. |
| `ai_prices` | `{}` | Price overrides, $ per million tokens. |
| `ai_context_windows` | `{}` | Context window overrides, in tokens. |
| `ai_max_steps` | `25` | Tool steps per step (a step's `max_steps` wins). |

The AI can not change these settings. It may ask to switch the profile
(**AI: Switch Profile…**); you confirm it.
All files: [Config files](../reference/config-files.md). All settings:
[Settings](../reference/settings.md).

## Troubleshooting

**"Profile review cannot run"**
:   A slot of the profile has no key or no provider. The toast names the
    slot. Set the key with **AI: Configure Provider…** or the environment
    variable, or change the slot in `ai_models`.

**My profile is not in the picker**
:   Its file has an error. Look for the toast "AI profiles: some were
    skipped", which names the file and line. Fix it and save.

**The run row has no `$`**
:   One step used a model with no known price. Add it to `ai_prices`.

**`Alt+Shift+P` does nothing**
:   Your terminal does not send the key. Use **AI: Next Profile**.
