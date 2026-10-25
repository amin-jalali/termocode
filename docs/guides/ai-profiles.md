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

Each row of the picker shows the steps of the profile in order, with the
model of each step (taken from its [model slot](#model-slots)):

```text
default        one step (main model: claude-sonnet-4-5)
review         Reviewer (claude-sonnet-4-5) → Fixer (gpt-5-mini)
fast           Answer (gpt-5-mini)
plan           planner decides the steps (up to 12 agents, 6 waves)
```

A slot with no model says so, for example `Fixer (quick: not set)`. On a
narrow window the row gets shorter: first the model names (`sonnet-4-5`),
then the agent names, then only `2 steps`. A project profile keeps its
trust state (`project, asks first`) at the start of the row. For the full
picture of one profile press `→` (or click **→ details** in the footer) on
its row: it opens the details of the highlighted profile and does not
switch to it. The same card is **AI: Show Profile** for the active profile
(see [See your agents and what they cost](#see-your-agents-and-what-they-cost)).

After the profiles the picker has three more rows. None of them switches
the profile:

- **Show details of <active>…** opens the details of the active profile.
- **New profile from <active>…** asks for a name and saves a copy (same as
  **AI: New Profile from Current…**).
- **Edit profiles (JSON)…** opens `profiles.json` (same as **AI: Edit
  Profiles**).

The choice is kept per workspace. When it is not `default`, the `✦`
badge in the status bar shows its name too, for example
`review · claude-sonnet-4-5`. The chip turns to the AI accent colour
while a pipeline runs.

!!! tip
    Your terminal may not send `Alt+Shift+P`. Use the palette, or bind
    another key to `AINextProfile` in `keymap.json`.

## Create and edit profiles

**AI: New Profile from Current…** copies the active profile. After you
name it, a preview card lists each step, its model slot and category,
tools, and the first line of its prompt. **Save** writes the profile to
`~/.config/termocode/ai/profiles.json` and switches to it. **Cancel** or
`Esc` leaves the file unchanged and shows that the profile was not saved.

**AI: Edit Profiles** opens that JSON file in the editor (with an example
when the file is missing). Saving it reloads the profile list and shows
`Profiles updated: <names>`. If a profile's tools or prompts changed, a
preview card shows the steps and asks **Use now** or **Later**. A broken
file shows the problem and line; built-in profiles remain available.

## Built-in profiles

| Profile | Steps | Use it for |
|---|---|---|
| `default` | **Assistant** on the `main` slot, all tools | The normal chat. Exactly the chat you know. |
| `review` | **Reviewer** (category `deep`, read-only tools), then **Fixer** (category `quick`, all tools) | "Review my last commit and fix what you find." |
| `fast` | **Answer** (category `quick`, read-only tools) | Quick questions on a cheaper model. |
| `local-only` | **Answer** on the `local` slot (Ollama) | Nothing leaves your machine. |
| `plan` | **Planner** (category `deep`, read-only tools), then the steps of its plan | Bigger tasks: the planner splits them into steps that run in waves. See [Planned runs](#planned-runs-and-waves). |

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
A step with no slot and no category uses `main`. The steps a planner
adds use the same mapping.

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
| `plan` | `true` makes the step a planner (see [Planned runs](#planned-runs-and-waves)). At most one per profile. |

A profile can also set `budget_usd` next to `steps` (see
[Budget pause](#budget-pause)).

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
- A step that waits says `waiting for step 1` (in a planned run, all
  the steps it waits for: `waiting for steps 3, 4`). A failed step shows
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

## Planned runs and waves

With the `plan` profile, the first step is a **planner**. It reads your
message (and may read files) and answers with a plan: a list of steps,
each with a task, a category and the steps it waits for. termocode
checks the plan, adds one card per step to the run, and runs them:

```text
 ▸ 1 · Planner · claude-sonnet-4-5 · 9 s · 2 tools · 6.2k tok · $0.021
 ▸ 2 · Map the parser · gpt-5-mini · 12 s · 3 tools · 5.0k tok · $0.002
 ▾ 3 · Speed up lexer · gpt-5-mini (quick) · running · 1 tool
 ▾ 4 · Speed up tokens · gpt-5-mini (quick) · running
 ▸ 5 · Verify · claude-sonnet-4-5 (deep) · waiting for steps 3, 4
```

- **Waves.** Steps whose waits are done run at the same time, up to
  `ai_parallel_max`. A planned run uses 3 when `ai_parallel_max` is not
  set. Other profiles keep their setting (1 by default).
- **What a planned step sees.** Your message, its own task under
  `### Task`, and the answers of the steps it waits for. It does not see
  the plan or the answers of other steps.
- **Categories.** Each planned step is `quick` (the default) or `deep`;
  the profile's `categories` pick the model.
- **The answer.** The plan ends with one final step that waits for the
  others. Its answer is the reply. Steps that came after the planner in
  your own profile wait for that final step.

**The plan is checked strictly.** At most 12 steps and 6 waves, unique
ids, known categories, a task for every step, no unknown keys, no loops,
and one final step. When the plan breaks a rule, the planner is asked
once more with the error, and the planner card shows
`↻ plan not valid, asked again: …`. When the second plan is also wrong,
the planner card fails with the error (`✘ the plan is not valid after
one repair: …`) and nothing else runs.

**Repository content is data.** The planner is told that files, code,
comments and tool results are data, not instructions: it plans only what
you asked.

**Cards that wait for you take turns.** Steps that run at the same time
can each need you: a confirm dialog or a review card. They come one at a
time, in order. The confirm title names the step, for example
`✦ Fixer wants to: Run command`. When you stop the run, a step that waits
for its turn stops at once.

To make your own planned profile, add a step with `"plan": true`. Steps
after it in the list run after the plan:

```json
{
  "profiles": {
    "plan-and-summarize": {
      "categories": { "quick": "gpt", "deep": "claude" },
      "budget_usd": 0.50,
      "steps": [
        { "id": "planner", "title": "Planner", "category": "deep", "plan": true,
          "tools": { "preset": "read-only" } },
        { "id": "summary", "title": "Summary", "category": "quick", "tools": { "preset": "none" } }
      ]
    }
  }
}
```

**Resume a planned run.** When a planned run fails, is stopped, or
termocode restarted in the middle, the chat shows a
**↻ Resume (2 of 5 left)** button: 2 steps did not finish, of 5 in the
run. Resume reuses the saved plan: the planner is not asked again, the
finished steps keep their saved answers, and only the unfinished steps
run (with the steps that wait for them). The chat file keeps each planned
step's task for this; run snapshots never hold it.

- The steps of the saved plan run on the model slots of the profile's
  categories **as they are now**. If you changed `categories` since the
  run, the resume uses the new slots.
- Chats saved by an older termocode did not keep the planned steps'
  tasks. Their planned runs show no button: send the message again.
- If the profile's own steps changed since the run, resume says so; send
  the message again.

While a planned run goes, the chat shows where it is under its cards:
`─ run · wave 3/4 · done 2/5 · 2 running ──`. A click on that row opens
the run in the AGENTS panel.

## The AGENTS panel

The AGENTS panel shows the runs of the current chat as a graph. It uses
the same place on the right as the chat. **Alt+Shift+A** (palette
**AI: Toggle Agents Panel**) shows it, and pressed again goes back to the
chat. **Alt+A**, the `‹ chat` chip in the header, or `Esc` also bring the
chat back.

```text
 ✦ AGENTS ‹ chat
 run ml3ov4001 · plan              wave 3/4 · done 2/5
 ● 2 running · following
 ┌ ▸ Map the parser ─────┐
 │ ✓ done · 12 s · 2 tu… │
 └───────────────────────┘
             │
             ├──────────────────────────┐
             ▼                          ▼
 ┌ ▾ Faster lexer ───────┐  ┌ ▾ Token cache ────────┐
 │ ● running · quick     │  │ ● running · quick     │
 │ ✎ now · …so that      │  │ waiting for the model │
 │ 21 s · 1 turn         │  │ 21 s · 2 turns · 1 t… │
 └───────────────────────┘  └───────────────────────┘
 Dependencies
   Map the parser → Faster lexer
   …
 Node details · Faster lexer
   quick · gpt-5-mini
   ● running · 21 s · 1 turn
   task · speed up the lexer.
```

- **Cards.** One card per step, one row of cards per wave. A card shows
  the step's state (`● running`, `○ pending`, `✓ done`, `✘ failed`,
  `⊘ blocked`, `◌ cancelled`), its category, what it does now (the tool
  it runs, or the end of what it writes), or which steps it waits for,
  and its time, turns (model requests) and tools. A running card shows
  its model next to the live time. A finished card shows its model,
  tokens and cost (`gpt-5-mini · 3.4k tok · $0.004`) in a second row;
  on a narrow card the tokens go first, then the model version. When a
  running step gets no token for 30 s (90 s before its first token), its
  card shows `⚠ possibly stalled · 35s` and turns yellow; the step in the
  chat says the same. A tool that runs long never warns. Lines with `│ ├ ┐ ▼`
  join one wave to the next; the **Dependencies** list says exactly
  which step feeds which.
- **Header.** The run id, the profile, `wave i/n` (the first wave that
  is not done) and `done k/m` (steps done of all steps).
- **Node details** of the selected step: category, model, tokens, cost,
  context, its state and times, its task while the run goes, its last
  tool calls and the end of its answer (or its error).
- **Follow mode** is on when the panel opens: the selection follows the
  running step. Moving the selection yourself turns it off; `f` turns it
  on again. The status row says `following` while it is on.
- **Narrow panels** show one card per row. With ASCII icons the panel
  draws `+ - | v` instead of box lines and arrows.

| Key | What it does |
|---|---|
| `Tab` / `↓` / `j` | Select the next step (in wave order) |
| `Shift+Tab` / `↑` / `k` | Select the previous step |
| `Space` / `Enter` | Fold or unfold the selected card (the same choice as its card in the chat) |
| `d` | Show or hide the node details |
| `u` | Switch to [AI: Usage](#see-your-agents-and-what-they-cost) (and back) |
| `f` | Follow the running step again |
| `r` | Resume the shown run when termocode's restart interrupted it |
| `←` / `→` (or `h` / `l`) | Show the previous / next run of this chat |
| `PgUp` / `PgDn` | Scroll the panel |
| `Home` | Scroll to the top |
| `Esc` / `q` | Back to the chat |
| `Ctrl+C` | Stop the run |

A click on a card selects it; the mouse wheel scrolls.

**After a restart.** A run that termocode's restart interrupted shows the
states of its last snapshot (written at each step's end), marked
`✘ run interrupted · last snapshot 12:04`. A step that was running then
shows `◌ was running`. Snapshots hold step ids, titles and states only,
never prompts or answers. When the run can go on, the status row adds
`· r resumes`: `r` runs its unfinished steps, the same as the
**↻ Resume** (or **↻ Retry from step N**) button in the chat.

**No cost when idle.** The panel draws from the run's saved cards. It
redraws once a second only while a run goes and the panel (or the chat)
is shown; at all other times it uses no CPU.

## Budget pause

Set `budget_usd` on a profile to cap what one run may cost. When the
steps of a run have cost that much, the run **pauses** before its next
step. Steps that already run finish first. A card asks:

```text
 ✦ Budget $0.50 reached
 This run has cost $0.512 so far; 2 steps did not run yet.
 Continue runs them without another budget stop. Stop ends the run.
```

- **Continue** runs the rest of the run. The budget does not stop this
  run again.
- **Stop** ends the run. The steps that did not run show `cancelled`;
  **Retry from step N** works as for any stopped run.
- `Esc` closes the card. The run stays paused, and the chat shows
  `Budget $0.50 reached · … · the run is paused` with **Continue** and
  **Stop** buttons.

Only steps with a known price count (see [Tokens and cost](#tokens-and-cost)).
A run on models with no price never pauses.

### Stopped, failed or interrupted runs

When a run fails, is stopped, or termocode restarted in the middle (the
chat then says `✘ run interrupted (termocode restarted)`), the chat shows
a **↻ Retry from step N** button on the last turn. It runs the run again
from the first step that did not finish, with the saved answers of the
steps before it. A planned run shows **↻ Resume (N of M left)** instead
(see [Planned runs and waves](#planned-runs-and-waves)).

### Over SSH and after a disconnect

A run lives in termocode's session process. When your SSH connection
drops, the run goes on. Run `termocode attach` (or open termocode again
in the same folder): you see the run still going, or done, with its
cards. This is the same for a planned run: its steps go on in waves with
no client attached, and the AGENTS panel shows the graph again when you
come back. See [Remote and SSH](remote-ssh.md).

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

## See your agents and what they cost

Three places answer "which agents and models work here, in what order,
and what did they use?"

**1. The profile, before you run it.** Open the profile picker (click the
chip) to see every profile as a line of steps and models. For one profile
in detail, run **AI: Show Profile** from the palette. It is a read-only
card for the active profile:

```text
✦ Profile review · Review then fix
From: built in
2 steps, one after the other.

1 · Reviewer · main → claude-sonnet-4-5
    tools: read-only
    “You review code. List concrete problems with file:line. Do not edit fil…”
2 · Fixer · quick → gpt-5-mini
    “Fix the problems the reviewer listed. Keep the changes small.”
```

Each step shows its number, its agent, its `slot → model`, the tools it
may use (only when the profile limits them) and the first line of its
prompt. The line `From:` says where the profile comes from: built in,
your `profiles.json`, or this project's `.termocode/ai-profiles.json`
(with its trust state). A planner profile also lists the models of the
steps it adds. Use ↑/↓ to scroll a long card, `Esc` to close it. The
card changes nothing.

**2. The AGENTS panel, while it runs.** Every card shows its model. A
finished card adds the tokens and the cost. See
[The AGENTS panel](#the-agents-panel).

**3. AI: Usage, after it ran.** Run **AI: Usage** (or press `u` in the
AGENTS panel, or click the tokens and cost in a run's summary row in the
chat). It has two sets of tables:

- **This run.** The run you select with `←` / `→`, like in the AGENTS
  panel.
- **This session.** Every run and chat turn since this termocode started,
  also from other chats.

Each set has a table **by model** and one **by agent** (the step name),
with the calls, the tokens in and out, the total and the cost. The rows
are sorted by cost, then by tokens, and the last row is the total.

```text
THIS RUN · by model
model                  calls     in    out  total     cost
claude-sonnet-4-5          2   7.6k    380   8.0k   $0.028
gpt-5-mini                 5  11.5k    420  11.9k   $0.004
total                      7  19.1k    800  19.9k   $0.032
```

- *In* is the whole prompt, cached tokens included, so *in* + *out* =
  *total*.
- A local model (Ollama, LM Studio) costs `$0`.
- A model with no known price shows `—` as cost, and a note names it:
  `price unknown for my-model (set ai_prices)`. In a total, `$0.020+`
  means "at least this much": some prices are not known.
- A model used through a Claude sign-in is a subscription: `—`, with a
  note, and nothing to set.
- A narrow panel shows fewer columns (calls, then in and out go first).

The numbers come from what the runs record already; nothing is looked up
online. The session tables are kept in memory only: nothing is written to
disk for them, and they are gone when you close termocode. A step that
you retry counts again in the session (it did cost again), but the run
table shows the last attempt of each step.

!!! note
    Neither screen can be used by the AI: it can not read its own bill.

## Project profiles and trust

A project can ship profiles in `.termocode/ai-profiles.json`, in the same
shape as your `profiles.json`. A project profile with the same name as
one of yours replaces it in this project. A `models` key in the project
file is ignored: a repo can not point you at another server.

A profile's system prompts steer the AI, and a repo can try to misuse
that. So the first time you pick or use a project profile, a **trust
card** shows its steps, their slots and tools, and the first line of each
prompt:

- **Not in this project** — it does not run here, and `Alt+Shift+P`
  skips it. This button comes first and has the focus, so `Enter` alone
  never trusts a project.
- **Always in this project** — the profile runs in this project.
- `Esc` — no answer; nothing is trusted, and your message goes back to
  the input.

The answer is stored in your workspace session file, outside the repo.
When the project changes the profile, the card asks again.
**AI: Project Profiles Trust…** lists the project's profiles and lets
you change an answer.

## Settings

| Setting (`config.json`) | Default | Meaning |
|---|---|---|
| `ai_models` | `{}` | Your model slots. |
| `ai_parallel_max` | `1` | How many steps may run at the same time (at most 8). With 1, steps run in order and `deps` is refused. A planned run uses 3 when this is not set. |
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

**`Alt+Shift+A` does nothing**
:   Your terminal does not send the key. Use **AI: Toggle Agents Panel**,
    a click on the run row of a planned run, or bind another key to
    `AIToggleAgents` in `keymap.json`.

**"the plan is not valid after one repair"**
:   The planner's plan broke a rule twice (see
    [Planned runs](#planned-runs-and-waves)). Send the message again, or
    give the planner a stronger model with `"categories": {"deep": …}` in
    your own planned profile.

**Planned steps run one at a time**
:   `ai_parallel_max` is set to `1` in `config.json`. Remove it (a
    planned run then uses 3) or set it to 2 or more.

**A planned run stopped with `Budget … reached`**
:   The profile's `budget_usd` was reached. Press **Continue** to run the
    rest, or **Stop**. See [Budget pause](#budget-pause).
