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
[Itermoc](itermoc.md) lists all profiles, the AIs you connected and the
trust state of each profile in a sidebar view; the chip and its picker stay the fast way to switch.

| How | What it does |
|---|---|
| Click the chip | Opens the profile picker |
| `Alt+Shift+P` or `Alt+N` | Switches to the next profile (a toast says which) |
| **AI: Switch Profile…** | Opens the profile picker |
| **AI: Next Profile** | Same as `Alt+Shift+P` |

Each row of the picker shows the steps of the profile in order, with the
model of each step (the one you chose in [the model list](#choose-a-model)):

```text
default        one step (main model: claude-sonnet-4-5)
review         Reviewer (claude-sonnet-4-5) → Fixer (gpt-5-mini)
fast           Answer (gpt-5-mini)
plan           planner decides the steps (up to 12 agents, 6 waves)
```

A step whose model is not set says so, for example `Fixer (quick: not set)`. On a
narrow window the row gets shorter: first the model names (`sonnet-4-5`),
then the agent names, then only `2 steps`. A project profile keeps its
trust state (`project, asks first`) at the start of the row. For the full
picture of one profile press `→` (or click **→ details** in the footer) on
its row: it opens the details of the highlighted profile and does not
switch to it. The same card is **AI: Show Profile** for the active profile
(see [See your agents and what they cost](#see-your-agents-and-what-they-cost)).

After the profiles the picker has four more rows. None of them switches
the profile:

- **Show details of <active>…** opens the details of the active profile.
- **New profile from <active>…** opens the step form on a copy (same as
  **AI: New Profile from Current…**). See
  [Create and edit profiles](#create-and-edit-profiles).
- **Edit a profile…** lists the profiles and opens the step form on the one
  you pick (same as **AI: Edit Profile…**).
- **Edit profiles (JSON)…** opens `profiles.json` (same as **AI: Edit
  Profiles**).

The choice is kept per workspace. When it is not `default`, the `✦`
badge in the status bar shows its name too, for example
`review · claude-sonnet-4-5`. The chip turns to the AI accent colour
while a pipeline runs.

!!! tip
    Your terminal may not send `Alt+Shift+P`. Press `Alt+N` (the same
    command, no Shift), use the palette, or bind another key to
    `AINextProfile` in `keymap.json`.

## Create and edit profiles

You do not need to write JSON. A step form makes a profile or changes
one.

Open it from the profile picker (**New profile from <active>…** starts
from a copy of the active profile; **Edit a profile…** lets you pick one)
or from the palette: **AI: New Profile from Current…** and **AI: Edit
Profile…**. Editing one of your own profiles changes it in place. A
built-in or project profile cannot be changed, so the form opens on a
copy with a new name (`my-review`).

The form has these fields:

| Field | What it is |
| --- | --- |
| Name | Letters, digits, `-`, `_` and `.`. `default` and the other built-in names are taken. |
| Title | One line about the profile (optional). |
| Budget | Dollars per run; empty means no limit. |
| Quick → / Deep → | Which model **Automatic: quick** and **Automatic: deep** use in this profile. `default` keeps the standard mapping. |

Below them is the list of steps. Only the step you are on is open; the
others show one line (model and tools). Each step has:

- **Title** and **Id** (the id is how other steps refer to it).
- **Model**: the model the step runs on. Press `Enter` (or click it) to
  open the model list; see [Choose a model](#choose-a-model). `←` and `→`
  step through the list without opening it.
- **Fix** (only when the step's AI is not ready): the line under
  **Model** says why, for example `✗ needs OpenRouter` or
  `✗ Claude Code is not signed in`. **Fix** opens the **Add AI** card at
  that AI. When the card ends, you are back on the same step.
- **Tools**: `none`, `read-only` or `all`. A tool list written in JSON
  (`allow`, `deny`) stays as it is.
- **Prompt**: what the model is told, over several lines.
- **Waits for**: step numbers (`1, 2`), shown when `ai_parallel_max` is 2
  or more. See [Planned runs and waves](#planned-runs-and-waves).
- **Planner**: at most one step plans the run.

**Add step**, **Move up**, **Move down** and **Remove step** change the list.
**Add step** opens the model list of the new step at once (`Esc` keeps the
chat's model). **Remove step** asks first, with **Keep step** as the
default button.

### Choose a model

The model list shows every model of every AI you connected, in one
list you can search. Type to search: it matches a model id (`mini`) or
an AI's name (`ollama`). `↑` and `↓` move, `Enter` or a click chooses,
`Esc` closes the list and changes nothing.

The list has these parts, top to bottom:

- **Same model as the chat**, **Automatic: quick** (a fast, low-cost
  model) and **Automatic: deep** (the strongest model). The automatic
  ones follow **Quick →** and **Deep →** above.
- One group per connected AI, with a tag that says how you pay for it:

  | Tag | Meaning |
  | --- | --- |
  | `your plan` | A sign-in AI (Claude Code, Codex). It uses your plan; no cost per request. |
  | `API · pay per use` | An API key. Each request costs money. |
  | `local · free` | A model on your machine (Ollama, LM Studio, a local server). |

  A sign-in AI also has **Other model id…**: type the id in the search,
  then choose the row (`Use "…"`). A search that finds no model offers
  the same row.
- **From config.json**: your named models from `ai_models`, when you have
  some (or the step already uses one).
- **+ Add another AI (OpenAI, Llama…)** at the bottom: it opens the
  **Add AI** card (see [Set up AI](ai-setup.md#option-a-add-an-ai-in-the-app)).
  Its last button says **Back to the profile**: you come back to the same
  step with the model list open, and the new AI's models are in it. The
  form keeps what you changed.

The models of an AI come from the list termocode saved when you
connected it (refresh it in [Itermoc](itermoc.md)). A step you choose
saves the AI and the model (`"provider"` + `"model"`; a sign-in AI:
`"cli"` + `"model"`). An older step that names a model slot keeps it
until you choose another model.

Keys: `Tab` and `Shift+Tab` (or `↓` and `↑`) move between fields, `Enter`
edits a text field, opens the model list or changes a choice, `←` and `→` change a choice, `Esc`
stops editing (a second `Esc` cancels), `a` adds a step, `K` and `J` move
it, `x` removes it, and `?` shows the key card. Everything also works with
the mouse: click a field to edit it, a choice to change it, a button to
press it. The form scrolls, so it fits 80 columns by 24 rows.

The form checks your input with the same rules as `profiles.json`: a
duplicate step id, more than 12 steps, two planners, `waits for` without
`ai_parallel_max`, an unknown model name, a prompt over 8 KB. The problem
shows under the field. **Save…** stays inactive until the form is valid;
pressing it then jumps to the first problem.

**Save…** opens the preview card: each step, its model, tools, and the
first line of its prompt. Its **Save** writes the profile to
`~/.config/termocode/ai/profiles.json` and switches to it. The file is
read, changed and written back through a temporary file, so other
profiles, other keys and the keys of your profile that the form does not
know stay as they were. **Cancel** on the preview brings the form back.
**Cancel** or `Esc` on the form writes nothing; after changes it asks
**Keep editing** or **Discard**. When you edit one of your own profiles,
**Delete profile…** removes it from the file after a question (**Keep
profile** is the default button).

### Edit the JSON yourself

**AI: Edit Profiles** opens that JSON file in the editor (with an example
when the file is missing). Saving it reloads the profile list and shows
`Profiles updated: <names>`. If a profile's tools or prompts changed, a
preview card shows the steps and asks **Use now** or **Later**. A broken
file shows the problem and line; built-in profiles remain available.
Use it for fields the form does not show (`output`, `max_steps`,
`allow` and `deny` lists).

## Built-in profiles

| Profile | Steps | Use it for |
|---|---|---|
| `default` | **Assistant** on the `main` slot, all tools | The normal chat. Exactly the chat you know. |
| `review` | **Reviewer** (category `deep`, read-only tools), then **Fixer** (category `quick`, all tools) | "Review my last commit and fix what you find." |
| `fast` | **Answer** (category `quick`, read-only tools) | Quick questions on a cheaper model. |
| `local-only` | **Answer** on the `local` slot (Ollama) | Nothing leaves your machine. |
| `plan` | **Planner** (category `deep`, read-only tools), then the steps of its plan | Bigger tasks: the planner splits them into steps that run in waves. See [Planned runs](#planned-runs-and-waves). |
| `until-done` | **Planner**, the steps of its plan, **Verify** (runs tests, edits nothing), and **Fix** while verify fails | Tasks that must end with green tests. See [Work until done](#work-until-done). |

Your own profiles with the same name replace a built-in one, except
`default`, which you can not replace.

## Model slots

Most steps name an AI and a model: you pick them in
[the model list](#choose-a-model) and nothing else is needed. This
section is the expert path: a **slot** is a short name in `config.json`
for a provider and a model. A step can name a slot instead of a model,
and the built-in profiles use these three, which always exist:

| Slot | What it is |
|---|---|
| `main` | Your `ai_provider` and `ai_model`: the chat's own model. It can not be replaced. |
| `quick` | The main provider with `ai_inline_model` (the ghost-text model). |
| `local` | Ollama on `http://localhost:11434/v1` (or `OLLAMA_HOST`). |

Add your own slot with **AI: Add Model Slot…**. Your slots are rows in
the AI PROVIDERS section of [Itermoc](itermoc.md#named-models-from-configjson),
under `from config.json`. The form asks for a
name, a provider, a model and, when the provider needs one, a base URL.
**Edit…** and **Remove…** on a slot's row work the same way. After Save
the form offers **Set key** when the provider has no key yet.

The slots live in `~/.config/termocode/config.json` under `ai_models`.
To edit the file by hand (the expert path), run **AI: Edit Model Slots**:

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

No file in this guide holds a key. A step (or slot) uses the key of its
AI, in this order:

1. the environment variable (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
   `OPENROUTER_API_KEY`),
2. the key you stored with the **Add AI** card or **AI: Configure
   Provider…**,
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

The step form (see [Create and edit profiles](#create-and-edit-profiles))
covers most profiles. For the rest, run **AI: Edit Profiles**. It opens
`~/.config/termocode/ai/profiles.json` and creates it with a working
example the first time. Save the file: the picker and `Alt+Shift+P` read
it again.

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

**AI: New Profile from Current…** opens the step form on a copy of the
active profile; its Save writes the copy into the same file and switches
to it.

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
| `cli` | `claude` or `codex`: the step runs Claude Code or Codex with your own subscription login, not a model slot (see [Subscription steps](#subscription-steps-claude-code-and-codex)). Leave out `slot` and `category`. |
| `provider` | A provider kind (`anthropic`, `openai`, `openrouter`, `ollama`, `lmstudio`, `openai-compatible`): the step runs on that provider's `model`, with no slot in `config.json`. It uses the provider's key (environment variable, then the stored key) and the server URL from `ai/providers.json`. Leave out `slot` and `category`. |
| `model` | The model id. Needed with `provider` (for example `claude-sonnet-5-5`, `llama3.2:3b`). With `cli` it picks the CLI's model (for example `opus` or `gpt-5-codex`); without it the CLI's default runs. Letters, digits and `. _ : / @ + -`; it may not start with `-`. |

A step with `provider` and `model`:

```json
{"id": "plan", "provider": "anthropic", "model": "claude-sonnet-5-5"}
```

A provider you have not set up on this machine is not an error in the
file: profiles travel between machines. The step fails when it runs and
says which key it needs.

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

## Approve the plan

Before a run starts a step that **changes files** or **runs commands**,
the chat shows the plan and waits for you. A planned run shows it after
the planner (the planner only reads). A fixed profile, like `review`,
shows it before its first step.

```text
 ✦ Approve this plan?
 Nothing that changes files or runs commands starts before you answer.

 1. Map the parser · gpt-5-mini
    only reads
 2. Speed up lexer · gpt-5-mini
    ✎ changes files · ▶ runs commands
    Make the lexer faster; keep the tests green.
 If verify fails: Fix · gpt-5-mini
    ✎ changes files · ▶ runs commands

 Limits: up to 3 rounds of verify and fix · budget $2.00 (it pauses there)

        Deny      Approve      Edit
```

- **Deny** is first and has the focus, so `Enter` alone never starts a
  step. `Esc` is Deny too. The run stops and the chat says so.
- **Approve** runs the plan.
- **Edit** opens the plan as JSON in the editor. Change a task, title or
  `deps`, leave a step out to skip it, or add a step with a new `id` and
  a `prompt`. Save the file: the card comes back with the new plan. When
  the edit does not fit, the card says why and the plan does not change.

While the run waits, the chat shows **Review plan** and **Deny**, and the
[AGENTS panel](#the-agents-panel) says `waiting for you` (`a` opens the
card). **AI: Review Waiting Plan** opens it too.

**Later in the run.** The fix step of [Work until done](#work-until-done)
is part of the plan you approved: a new round goes on without a new
question, and the step says so. When a new plan adds steps that change
files or run commands, the card asks again for those steps.

**After a restart.** The approval is saved with the chat. **↻ Resume**
does not ask again for a plan you approved. A run that stopped before
your answer asks again.

**When it asks.** `ai_plan_approval` in `config.json`: `ask` (default)
asks for plans that change files or run commands, and a read-only plan
runs at once. `always` asks for every plan. `auto` never asks.
**AI: Toggle Plan Approval** turns the question off (or on) for the
current chat only. The AI can not approve its own plan or change this
setting.

## Let the AI start a run

The AI in the chat can start another run for you: a profile, or a few
steps it writes itself, on a task. It always asks first.

```text
 ✦ AI wants to: start an agent run
 Start an agent run with profile review (2 steps):
   1 · Reviewer · main → claude-sonnet-4-5 · tools read-only
   2 · Fixer · quick → gpt-5-mini · tools all

 Task:
   Review parser.go and fix the bug

 Worktrees: auto (steps that run side by side work in their own git worktree)

 Started by AI in the chat "fix the parser".
 It starts here when the current answer or run ends; plan approval still asks before steps that write.

        Deny      Start run
```

- **Deny** is first and has the focus. The card asks every time: no
  "Always allow" and no setting turns it off.
- **Start run** puts the run in a queue. When the AI's answer (or the
  run it works in) ends, the run starts in the same chat, as if you sent
  the task with that profile. [Approve the plan](#approve-the-plan),
  [own worktrees](#own-worktrees-for-parallel-steps), the budget card
  and the review cards work as usual.
- The task shows in the chat with **✦ started by AI**. The
  [AGENTS panel](#the-agents-panel) and [Runs](runs.md) show a
  **by AI** chip.
- The AI reads a run with `agent_run_status` and `agent_run_result`
  (state, steps, cost, final answer, changed files) in a later answer.
  It can not wait for the run inside the same answer.
- `agent_run_stop` drops a run that waits, or stops a running one. It
  asks first too. A step can not stop its own run.
- **Stop** (`Ctrl+C`) also drops the runs that wait.

**Limits.** At most 3 runs started by the AI wait or run at once. A run
the AI started can start one more run, not two levels more. A run
started from a run with a budget gets only what is left of that budget;
when nothing is left it does not start and the chat says why. Outside
agents (over MCP) never get these tools.

## The AGENTS panel

To watch every run in one place (all chats, live numbers, Stop, Pause and
Resume), use [Runs](runs.md): the green pulse icon in the activity bar.

The AGENTS panel shows the runs of the current chat as a graph. It uses
the same place on the right as the chat. **Alt+Shift+A** or **Alt+G** (palette
**AI: Toggle Agents Panel**) shows it, and pressed again goes back to the
chat. **Alt+A**, the `‹ chat` chip in the header, or `Esc` also bring the
chat back. Press `?` in AGENTS or in the usage tables (`u`) for the key
card; the footer says `? keys` first, so it stays visible in a narrow
panel.

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
| `a` | Open the plan card of a run that waits for your approval |
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

## Own worktrees for parallel steps

When two steps that may change files run at the same time, each one
works in its **own git worktree**. They never change the same file, and
your files stay as they are while they work.

- **When.** In a git repository with at least one commit, for a step
  that runs in the same wave as another writing step. Read-only steps and
  steps that run alone work in your tree, as before. Outside a git
  repository every step works in your tree.
- **Where.** The worktree is in the repository's git folder
  (`.git/termocode/worktrees/<id>`), on the branch
  `termocode/agent-<id>`. It starts from your files as they are now,
  with edits you did not commit yet. New files git does not track yet are
  not in it. Your branch, your staged files and your
  files do not change.
- **On the card.** The step card shows `⑂ termocode/agent-… · working`.
  When the step ends it shows `3 files changed` and three buttons:

```text
 ▸ 3 · Speed up lexer · gpt-5-mini · 14 s · 2 tools
     ⑂ termocode/agent-r1-lexer · 3 files changed
      Review   Merge   Discard
```

- **Review** opens the agent's changes as a diff (read-only).
- **Merge** writes the changes into your tree, then removes the worktree
  and its branch. If you changed one of the same files in the meantime,
  nothing is written. A card shows the files and asks: **Cancel**,
  **Merge with markers** (both versions with `<<<<<<<` markers, you fix
  them; the branch stays) or **Keep on branch** (the worktree goes, the
  work stays on the branch for later).
- **Discard** asks first (**Cancel** is first and selected). It removes
  the worktree and the branch. Your files do not change.
- A step that changed no file has its worktree removed at once.
- **If the worktree cannot be made** (a git error), the step does not
  start in your tree, because another step works at the same time. It
  stops with the reason; fix it and use **↻ Retry from step N**. A git lock
  that another git command holds for a moment is waited for first.
- Steps that come later do not see the changes until you merge. They are
  told so, with the worktree path.

In the AGENTS panel the card shows `⑂ 3 files changed`; `w` opens the
same Review / Merge / Discard card for the selected step.

**Left-over worktrees.** If termocode stops while a step works, its card
offers Review, Merge and Discard when you open the chat again.
**AI: Agent Worktrees…** lists every agent worktree of the repository,
also ones from other chats. Nothing is deleted without a card that asks.

**Turn it off.** **AI: Toggle Agent Worktrees**, or `"ai_worktrees": "off"`
in `config.json`. Then every step works in your tree.

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

## Work until done

A **work until done** run does not stop at the first answer. It plans,
does the work, and then a **Verify** step checks it: it runs the
project's tests and checks and reads the changes. When verify finds
problems, a **Fix** step repairs exactly those problems, and verify runs
again. This repeats until verify passes, or until the run has used all
its rounds.

### Turn it on

- **AI: Work Until Done (toggle)** in the palette switches the chat to
  the built-in `until-done` profile. The chat header chip then reads
  `✦ until-done ▾`, and a toast says the limits, for example
  `Work until done: on · up to 3 rounds · budget $2.00`. Run the command
  again to go back to the profile you had before.
- Start one message with `/done `, for example `/done fix the failing
  tests`. Only this message runs until done. Your profile does not
  change, and the chat keeps the message without `/done `.
- **AI: Switch Profile…** and `Alt+Shift+P` also pick `until-done`.

The AI can ask to switch to `until-done` too, but only with a confirm
card that you answer.

### What the chat shows

The run has the cards of a planned run: **Planner**, the steps of its
plan, and **Verify**. When verify fails, a **Fix** card appears. The
cards of the second round and later say which round they ran in:

```text
 ▸ 2 · Verify · claude-sonnet-4-5 · round 2/3 · 41 s · 12.0k tok · $0.044
 ▸ 4 · Fix · gpt-5-mini · round 2/3 · 18 s · 5.5k tok · $0.003
 ─ run · 1 m 20 s · 4 steps · 2 rounds · verify ✓ · 30.4k tok · $0.071 ─
```

- An open Verify card has one line per round, for example
  `✘ round 1/3: verify failed` and `✓ round 2/3: verify passed`.
- The answer of the run is the last verify report.
- When verify still fails after the last round, the run row says
  `verify ✘ (3 rounds)` and a red line says
  `✘ Not done after 3 rounds: verify still fails.` Read the last verify
  report above it, then send a new message or fix the rest yourself.
- The AGENTS panel header says the round, for example
  `run r1 · until-done · round 2/3`, and the Fix and Verify cards say
  their round.

Verify must end its answer with the line `VERDICT: PASS` or
`VERDICT: FAIL`. When the line is missing, verify is asked once more
(`↻ no VERDICT line, asked once more` on its card). When it is still
missing, the round counts as a fail, and the card says
`no VERDICT line, even after asking again: counted as FAIL`.

### Limits and where to change them

| Limit | Default | Change it |
|---|---|---|
| Rounds (how many times verify runs at most) | 3 | Settings → AI → `ai_until_done_rounds` (1 to 10), or `loop.max_rounds` in a profile |
| Budget | $2.00 | Settings → AI → `ai_until_done_budget_usd` (0 = no budget), or `budget_usd` in a profile |

- A profile's own `loop.max_rounds` and `budget_usd` win over the
  settings. A profile that leaves them out uses the settings.
- A value out of range is refused in Settings with an error. A bad value
  written by hand in `config.json` is not used: the default applies and
  a toast names the problem.
- The budget works like the [budget pause](#budget-pause): the cost of
  every round adds up, and the pause can come between two rounds.
  **Continue** runs the remaining rounds.

### Safety

- The plan card asks before the first step that changes files or runs
  commands (see [Approve the plan](#approve-the-plan)). The fix step is
  part of that plan.
- The Fix step may use all tools, so its file changes open the review
  card (or the confirm dialog) as in any chat turn. Nothing is written
  without your answer, unless you auto-approved it.
- Verify may run commands and tests (`run_command`, `tests_run`,
  `tasks_run`). They ask first as usual. To let verify run tests without
  asking, auto-approve `run` in **AI: Agent Auto-Approve…**.
- **AI: Undo Last Turn** undoes the file changes of every round.
- `Ctrl+C` stops the whole run.
- A run that termocode restarted in the middle shows
  **↻ Resume (N of M left)** and goes on in the round where it stopped.

### Your own loop profile

Add `loop` to a profile: `verify` is the id of one of its steps, `fix` is
a step that runs (after verify failed) before verify runs again.

```json
{
  "profiles": {
    "tdd": {
      "title": "Do, test, fix",
      "steps": [
        {"id": "do", "category": "quick", "tools": {"preset": "all"}},
        {"id": "check", "title": "Check", "category": "deep",
         "system": "Run go test ./... and report every failure."}
      ],
      "loop": {
        "verify": "check",
        "fix": {"title": "Fix", "category": "quick", "tools": {"preset": "all"}},
        "max_rounds": 4
      },
      "budget_usd": 1
    }
  }
}
```

- `fix` takes the fields of a step, except `deps` and `plan`. Its `id`
  is `fix` when you leave it out; it may not be a step id.
- `verify` may not be the planner step.
- termocode adds the verdict rule to the verify step's prompt, and the
  fix step gets the verify report under `### Verify report (round N)`.

## Subscription steps: Claude Code and Codex

A step can run **Claude Code** or **Codex** on your own subscription
login instead of an API key. For example: Claude Code does the work,
Codex reviews it, and no API tokens are billed.

**Before you start.** Connect the AI once with **AI: Add AI…** and
**Sign in with Claude Pro / Max** or **Sign in with ChatGPT Plus / Pro**
(see [Sign in with your plan](ai-setup.md#sign-in-with-your-plan)).
termocode installs the CLI into its own tools folder when it is missing
and runs the CLI's own sign-in. termocode never sees your login: each CLI
keeps its own.

**In the form.** In **Edit profile…** (or a step in Itermoc), open a
step's **Model** list and choose a model from the group tagged
`your plan` (for example `opus` under Anthropic, or a model under
OpenAI). The line under **Model** says if the CLI is ready:

- `✓ uses your Claude Code sign-in · no API key` — ready.
- `✗ Claude Code is not installed` or `✗ Codex is not signed in`, with
  **Fix**: the **Add AI** card opens at that AI's sign-in. When it ends,
  you are back on the step.

**In profiles.json.** Give the step `"cli": "claude"` or
`"cli": "codex"`. This profile lets Claude Code work and Codex review,
until the review passes:

```json
{
  "profiles": {
    "claude-codex": {
      "title": "Claude Code works, Codex reviews",
      "steps": [
        {"id": "work", "title": "Work", "cli": "claude", "tools": {"preset": "all"}},
        {"id": "review", "title": "Review", "cli": "codex",
         "system": "Review the change. Run nothing that edits files."}
      ],
      "loop": {
        "verify": "review",
        "fix": {"title": "Fix", "cli": "claude", "tools": {"preset": "all"}},
        "max_rounds": 3
      }
    }
  }
}
```

**What a subscription step may do.**

- Without `"tools": {"preset": "all"}` the step only **reads**: Claude
  Code gets the `Read`, `Grep` and `Glob` tools; Codex runs in its
  `read-only` sandbox. A reviewer should stay like this.
- With `"preset": "all"` the step may **edit files** in the project:
  Claude Code gets `Edit` and `Write` (edits are accepted, but it has no
  shell, so it runs no commands); Codex runs in its `workspace-write`
  sandbox.
- The CLI runs in the project folder. Your prompt goes in on standard
  input, never on the command line.
- The CLI gets your environment, minus termocode's own secrets and the
  vendors' API keys (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
  `CODEX_API_KEY` …). So it always uses your subscription login, never a
  billed key.
- A step that runs longer than 30 minutes is stopped. **Stop** (or
  `Ctrl+C`) ends the CLI and everything it started.

**The hand-off.** The next step gets your task, the earlier step's
answer and the work so far as a git diff (`git status --short` and
`git diff HEAD`, at most 24 KB) under `### Changes so far (git diff)`.
A verify step ends with `VERDICT: PASS` or `VERDICT: FAIL`, as in
[Work until done](#work-until-done).

**Cost.** A subscription step shows its time and the tokens the CLI
reports. Its cost says **subscription**, not dollars. It does not count
in `budget_usd` or the day budget.

**Usage limit.** When the CLI says its usage limit is reached, the run
**pauses** at that step. If the CLI says when the limit resets, the run
shows `usage limit · resumes at 16:00` and goes on by itself at that
time. Otherwise it shows `usage limit reached · Resume when it resets`.
**Resume** (in the chat or on the Runs page) runs the same step again;
the steps before it keep their work.

**Not logged in.** A step whose CLI has no login fails with
`… is not logged in: sign in with AI: Add AI… (or run claude auth login
in a terminal) …`. Sign in, then use **Retry from step N**. A step whose
CLI is missing fails with `… is not installed …`; the **Add AI** card
installs it and signs in.

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
| `ai_worktrees` | `auto` | `auto`: writing steps that run at the same time get [their own worktree](#own-worktrees-for-parallel-steps). `off`: every step works in your tree. |
| `ai_plan_approval` | `ask` | [Approve the plan](#approve-the-plan): `ask` before steps that change files or run commands, `always` for every plan, `auto` never. |
| `ai_prices` | `{}` | Price overrides, $ per million tokens. |
| `ai_context_windows` | `{}` | Context window overrides, in tokens. |
| `ai_max_steps` | `25` | Tool steps per step (a step's `max_steps` wins). |
| `ai_until_done_rounds` | `3` | [Work until done](#work-until-done): how many times verify runs at most (1 to 10). A profile's `loop.max_rounds` wins. |
| `ai_until_done_budget_usd` | `2` | Work until done: the budget of a run (0 = no budget). A profile's `budget_usd` wins. |

The AI can not change these settings. It may ask to switch the profile
(**AI: Switch Profile…**); you confirm it.
All files: [Config files](../reference/config-files.md). All settings:
[Settings](../reference/settings.md).

## Troubleshooting

**"Profile review cannot run"**
:   A step of the profile has no key or its AI is not connected. The
    toast names it. Connect the AI with **AI: Add AI…** (or press **Fix**
    on the step in the profile form), set the key with the environment
    variable, or, for a slot, change it in `ai_models`.

**My profile is not in the picker**
:   Its file has an error. Look for the toast "AI profiles: some were
    skipped", which names the file and line. Fix it and save.

**The run row has no `$`**
:   One step used a model with no known price. Add it to `ai_prices`.

**`Alt+Shift+P` does nothing**
:   Your terminal does not send the key. Press `Alt+N` instead, or use
    **AI: Next Profile**.

**`Alt+Shift+A` does nothing**
:   Your terminal does not send the key. Press `Alt+G` instead, use
    **AI: Toggle Agents Panel**,
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
