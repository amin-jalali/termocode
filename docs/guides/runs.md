# Watch AI runs (Runs)

**Runs** answers three questions about the AI's work: which profile runs
on which task, where is it now, and how much does it use. It is the
monitoring home of pipeline runs (a profile with more than one step, see
[AI profiles and pipelines](ai-profiles.md)). It only watches and acts on
runs; profiles and models are set up in [Itermoc](itermoc.md).

## Open Runs

- Click the green pulse icon in the activity bar.
- Run **AI: Runs** from the command palette (`F1`).
- On a profile in [Itermoc](itermoc.md), press **Runs** (`r`) or click
  `See runs →`. Runs opens filtered to that profile; `/` clears it.

The sidebar shows the list. The editor area shows one tab, **◉ Runs**: the
overview, or one run's page. With no runs yet, the page has two pills:
**Open the chat** (`c`) and **Profiles in Itermoc** (`i`).

In a window narrower than 120 columns, Runs hides the sidebar and shows
the list on the page instead, one line per run.

## The list

Runs are grouped by time:

| Group | What is in it |
|---|---|
| **NOW** | The run that goes now, and runs that stopped today with steps left. |
| **TODAY** | The other runs that started today. |
| **older…** | Runs of the last 7 days. `Enter` on the row opens them. |

Each run has two lines. The first line is the task (your message) and
one word on the right: the cost so far, or a state word. The second line
is the profile, a small pipeline (`●─◉─○`) and where the run is now.
A run the AI started has a **by AI** chip after the profile; its page and
its log name the chat that asked (see
[Let the AI start a run](ai-profiles.md#let-the-ai-start-a-run)). Type
`by AI` in the filter to see only those.

Colour is state only:

| Colour | Means |
|---|---|
| Green `◉` | The run goes. The green step is where it is right now. |
| Amber | It needs you: stalled (`⚠`), paused, or stopped with steps left. |
| Red `✗` | It failed (a step failed, or verify still fails). |
| Dim `✓` | It is done. |

A step is **stalled** when it sends nothing for too long: 30 seconds while
it writes, or 90 seconds without a first answer. A tool may run for
minutes and never counts as stalled. These are the same rules as the
chat's activity line and the AGENTS panel.

The top line, **today**, is what the AI cost today: the runs that started
today and this session's plain chat turns. Prices come from `ai_prices`
(see [Tokens and cost](ai-profiles.md#tokens-and-cost)). A model without
a known price adds nothing.

## The overview

- **SPEND TODAY**: one bar split by model, the cost of each model, and
  this session's total.
- **NOW**: each run as a lane: the task, the profile, the time, the
  tokens and the cost, then the pipeline as chips. A stalled run has a
  **Stop (x)** pill; a paused or stopped one a **Resume (r)** pill.
- **TODAY**: one line per run: the start time, the task, the profile,
  the time, the tokens, the cost and, for a failed run, why.

## One run's page

`Enter` on a run opens its page:

- The pipeline as boxes: each step's model slot, model, tokens and cost.
  The line under the step that works now says what it does, for example
  `now · edit internal/toast/toast.go · 3 tool calls · last output 2 s ago`.
- **TIMELINE**: each step as one part of a line, as long as it took.
- **STEPS**: each step's state, time, tokens and cost. The bar is the
  step's share of the run's cost. The right column says what the step may
  do (`read-only`, `edit files`) and which steps it waits for.
- **BUDGET**: the run against its profile's `budget_usd`, and today
  against your day budget (below).
- **WHAT HAPPENED**: when each step started and ended.

The numbers move once a second while a run goes and Runs is on screen.
Nothing ticks when no run goes.

## Act on a run

| Pill / key | What it does |
|---|---|
| **Stop** (`x`) | Stops the run. A card asks first; **Keep running** is the first answer, so a stray `Enter` stops nothing. The step that works now ends without its answer. Finished steps keep their work. |
| **Pause** (`p`) | The step that works now finishes, then the run waits before its next step. The chat shows **Paused by you** with **Continue** and **Stop**. |
| **Resume** (`r`) | Goes on with a paused run. For a run that stopped with steps left (you stopped it, or termocode closed while it ran), it runs the steps that are left, with the saved answers of the finished steps. A run in another chat opens that chat first. This is the chat card's **↻ Resume** button. |
| **Open in chat** (`c`) | Shows the run's chat. Another chat opens only when the AI is not busy. |

`Enter` on a run's page opens **⋯ more**: Profile in Itermoc (`i`, the profile's
page in [Itermoc](itermoc.md)), Copy run id
(`y`), Show in AGENTS (`a`), Usage by model (`u`). Mouse clicks work on
rows and pills.

A run can only resume when it is the last turn of its chat and its
profile still has the same steps. Otherwise send the message again.

## Keys

| Key | Action |
|---|---|
| `↑` `↓` | Pick a run |
| `Enter` | Open the run (on its page: more) |
| `Esc` | Back: the page to the overview, the filter off, then the list |
| `x` `p` `r` `c` | Stop, Pause, Resume, Open in chat |
| `a` `u` `i` `y` | AGENTS, usage by model, the profile in Itermoc, copy the run id |
| `/` | Filter by task, profile or chat title |
| `?` | The keys card |

## Day budget

Set `ai_day_budget_usd` in `config.json` to draw today's spend against a
daily line:

```json
{ "ai_day_budget_usd": 2.0 }
```

It only draws the line. It stops nothing. A run's own limit is its
profile's `budget_usd`: the run pauses there and asks (see
[Budget pause](ai-profiles.md#budget-pause)).

Steps that run Claude Code or Codex on your subscription
([Subscription steps](ai-profiles.md#subscription-steps-claude-code-and-codex))
show their tokens, and **subscription** where the cost goes. They count
in no budget. When such a CLI reaches its usage limit, the run shows
**paused** with `usage limit · resumes at 16:00` and goes on by itself
then; **Resume** (`r`) goes on at once.

## What Runs does not show

- A plain chat turn (a one-step profile such as `default`) is not a run.
  Its cost counts in **today** while termocode runs.
- Runs reads the saved chats (the newest 50). A deleted chat's runs are
  gone from the list.

## Other workspaces

When termocode runs in more than one window (another project), the Runs
list ends with **OTHER WORKSPACES**: each other workspace whose agents
work now or wait for you, for example `shop · 1 waiting for you ·
2 working`. A workspace with an agent that waits is shown in amber.
It counts AI runs and chats, and coding CLIs in terminal tabs (Claude
Code, Codex …). Each window writes a small file in
`~/.local/state/termocode/agents/` only when its agents' states change,
and removes it when it quits; the list is read when Runs opens.
