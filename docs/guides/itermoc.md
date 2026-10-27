# Itermoc: profiles, models, services and outside agents

Itermoc is one sidebar view for everything that shapes the AI chat. It
lists your agent profiles, the model slots they run on, the services the
chat can reach and the outside agents that use termocode's tools. The list
is calm: one row per thing, one word at the right. Pick a row and its
**page** opens in the editor area, drawn as a picture (see [The page](#the-page)).
Every verb opens the same screens you already know. Model slots are the one
thing you add, edit and remove right here, with a small form (see
[Models](#models)).

Set up a provider first: [Set up AI](ai-setup.md).

## The view

Open it with **Alt+Shift+I** (or **Alt+O**, if your terminal swallows the
first key), or from the command palette: **Itermoc: Open**. Press the key
again to go back to the view you had before. The palette also has
one command per section: **Itermoc: Profiles**, **Itermoc: Models**,
**Itermoc: Services** and **Itermoc: Outside Agents**. **AI: Agents &
Connections** opens the same view.

Two clicks open it at a section:

- The `⇄` in the status bar (an outside agent is connected) opens
  **OUTSIDE AGENTS**.
- The services chip in the chat header (`⌁ 2 MCP`) opens **SERVICES**.

The profile chip in the chat header (`✦ review ▾`) still opens the quick
profile picker. That stays the fast way to switch.

```text
 ITERMOC                                       ? keys  / filter
 ──────────────────────────────────────────────────────────────
 ✦ Overview                                                3 !

 ✦ PROFILES                                                1 !
   ● review                                              active
   ○ default
   ⚠ house-review                                       project
   + new profile…

 ϟ MODELS                                                  1 !
   ● main                                            sonnet-4-5
   ! gpt                                                 no key
   + add slot…

 ⌁ SERVICES                                                1 !
   ● github                                             28 tools
   ! linear                                              sign in
   + connect…

 ⇄ OUTSIDE AGENTS                                            1
   ● listening                                              auto
   ● claude-code                                        14 calls
   + start one…
 Enter actions · o page · ? keys
```

The first row, **Overview**, opens one page with all four groups and
everything that needs you (see [The overview](#the-overview)). Its number
(`3 !`, amber) counts those things.

Each group has an icon, its name and a number at the right. The number is
how many rows it has, or `1 !` (amber) when one of them needs you. A folded
group keeps its number.

| Group | What it lists |
|---|---|
| PROFILES | the agent profiles; `●` marks the active one (`active`), `⚠` a project profile you were not asked about yet (`project`) |
| MODELS | the model slots the steps run on, with the short model name, or `no key` in amber. A key is never shown |
| SERVICES | the services in `mcp.json`: `28 tools`, `off`, `sign in`, `failed` |
| OUTSIDE AGENTS | whether termocode listens for agents like Claude Code (`listening` and its mode), who is connected and their calls |

There are no buttons in the list. A row that needs you turns amber and
says why (`sign in`, `no key`, `project`); open its page for the button.
The dim row at the end of a group (`+ new profile…`, `+ add slot…`,
`+ connect…`, `+ start one…`) adds a new one. Press `Enter` or click it.
## The page

Click a row (or press `o` on it) and its page opens in the editor area, in
one tab named after it (`✦ review`). Pick another row and the same tab
shows that one: it never piles up tabs. With the page open, `↑` `↓` in the
list change it at once, like a file tree. Close the tab like any tab.

A **profile** page draws the pipeline as boxes with arrows (`you` →
`1 Reviewer` → `2 Fixer` → `answer`); every box has its model. When the
steps do not fit the width the pipeline wraps onto more rows. In a narrow
window it is one line of chips. Under it:

- **STEPS**: number, name, the model badge, what the step may use, what it
  waits for, and the first line of its prompt.
- **SETTINGS**: the categories (`quick` and `deep` slots), the budget and
  where the profile comes from.

The last line says `Last run 12 min ago · See runs →` (or `Not run yet`).
A click on `See runs →` opens [Runs](runs.md) filtered to this profile,
like **Runs** (`r`). Runs and
what they cost are not shown here: this page is setup only.

Three pills at the bottom run the common verbs: **Use** (`u`), **Edit**
(`e`) and **Runs** (`r`). `Enter` opens the actions menu (the list after
`⋯ more` says what is in it). A project profile that asks for your trust
has **Trust…** (`t`) first. **Edit** opens the profile step form (the
same form as **AI: Edit Profile…**): your own profile is changed in place,
a built-in or project profile becomes a copy under a new name. A click on a
box in the pipeline opens the form at that step. **Edit JSON** in the
actions menu still opens `profiles.json`.

A **service** page says what the chat gets from it: its state, where it
runs (the host of a remote service, the command of a local one), how many
tools it has and their names, its access (read only or writes allowed),
its sign-in and whether you trust it. When it needs you, an amber card at
the top says what is wrong and what it costs ("The chat lost 7 tools"),
with the button that fixes it: **Sign in** (`i`) for a sign-in that ended,
**Log** (`l`) for a service that does not start. The pills are **Show
tools** (`t`), **Disable…** (`d`) and **Log** (`l`). **Disable…** asks
first (*Keep on* is the first button): the entry and its sign-in stay, the
chat gets none of its tools. On an off service `d` is **Enable**, which
runs at once. **Remove…** (`D`) asks on its own card, *Keep* first. The
rest (Connect / Refresh, Trust, Allow writes, Sign out, Edit in mcp.json)
is in the actions menu (`Enter`). A token is never on the page.

An **outside agent** page draws the connection: `termocode ↔ claude-code`,
with how many calls it made and how often it asked you. **ALWAYS ALLOWED**
lists the tools it may run without asking (you said *Always allow* on its
card); **RECENT CALLS** are its own last calls. The pills are
**Revoke…** (`x`), **Copy config** (`y`) and **Status** (`T`).
**Revoke…** asks first (*Keep* is the first button), then forgets this
agent's whole *Always allow* list: each call that changes something asks
you again. The agent stays connected; **Disconnect…** and **Allowed
tools…** (revoke one tool) are in the actions menu.

The **listening** row's page draws termocode and the first connected
agent, lists the agents that are not running yet, and says the mode. Its
pills start an agent in a terminal tab: **Start Claude Code** (`s`),
**Start Codex** (`S`), and **Copy config** (`y`) copies the config other
agent tools need.

The page of a model slot shows its key facts and its main pills. A key is
never on a page. `Esc` on a page goes back to the list; `j`, `↓`, `↑` and
the wheel scroll a page that is taller than the window.

## The overview

The **Overview** row (and a group header) opens the overview page, in the
same tab (`✦ AI`):

```text
 ✦ AI  agents & connections                          listening auto
 ───────────────────────────────────────────────────────────────────
 ╭ ✦ PROFILES ── 1 ╮ ╭ ϟ MODELS ─── 2 ╮ ╭ ⌁ SERVICES ── 3 ╮ ╭ ⇄ OUTSIDE AGENTS ─ 4 ╮
 │ review          │ │ 4 slots        │ │ 1 of 3 ready    │ │ 1 connected          │
 │ ! house-review… │ │ ! gpt has no … │ │ ! linear needs… │ │ ● claude-code · 14 … │
 ╰─────────────────╯ ╰────────────────╯ ╰─────────────────╯ ╰──────────────────────╯

 NEEDS YOU  3 things
 ▸ ! linear        sign-in expired · the chat lost 7 tools    ▐ Sign in (i) ▌
   ! gpt           no key · this model cannot answer yet      ▐ Set key (k) ▌
   ⚠ house-review  a profile from this project · not trusted  ▐ Trust… (t) ▌

 ACTIVE FLOW  ✦ review · Review then fix
 you ──▶ 1 Reviewer ──▶ 2 Fixer ──▶ answer
 ───────────────────────────────────────────────────────────────────
 New profile (n)  Connect a service (c)  Start Claude Code (s)
```

- The four **tiles** say each group in a few words. A tile with something
  that needs you has an amber frame and line. Click a tile, or press `1`
  to `4`, to open that group: the active profile, the first model or
  service that needs you (else the first one), the first outside agent.
- **NEEDS YOU** lists every row that waits for you, the most urgent first:
  services that lost their sign-in or do not start, models without a key,
  project profiles you did not trust yet. A click on a line, or `Tab` to
  it and `Enter`, opens its page. Its pill fixes it right here (**Sign
  in**, **Set key**, **Trust…**, **Log**). When nothing needs you the page
  says `nothing right now`.
- **ACTIVE FLOW** draws the active profile's pipeline. A click on a step
  opens the step form of that profile.

In a short or narrow window the tiles become one line each.

## Actions of a row

Press `Enter` on a row in the list (or `Enter` on its page). A small menu
lists what you can do with it. Safe actions come first; the ones that ask for confirmation end in `…`.

| Row | Actions |
|---|---|
| Profile | Use · Details · Edit… · Duplicate… · Runs · Trust… (project profiles) · Edit JSON |
| Model slot | Set key (when the key is missing) · Edit… · Pick model… (the main slot) · Edit JSON · Remove… (your own slots) |
| Service | the service's own actions: Connect, Show tools, Sign in, Allow writes / Read only, Trust, Disable / Enable, Remove… and more (see [Connect MCP servers](mcp.md)) |
| Listening row | Show status… · Turn on / Start listening now · Start Claude Code · Start Codex · Copy config · Stop listening… |
| Outside agent | Show status… · Allowed tools… · Revoke always allow… · Disconnect… |

Every action opens a screen that exists without Itermoc:

- **Use** switches the chat to the profile. A project profile you did not
  trust yet shows the trust card first; its first button is *Not in this
  project*.
- **Details** is [AI: Show Profile](ai-profiles.md) for that profile. It
  does not switch to it.
- **Edit…** on a profile opens the profile step form. A built-in or
  project profile becomes a copy; **Save…** shows the preview card first.
- **Duplicate…** opens the same form on a copy of the profile you picked,
  under a free name (`my-review`). Nothing is written until **Save…**.
- **Runs** opens [Runs](runs.md) filtered to this profile's runs.
- **Set key** opens the key prompt of the slot's provider. The key goes to
  the secret store; Itermoc never shows it.
- **Edit…** on a model slot opens the slot form (see [Models](#models)).
  On `main` it opens **AI: Configure Provider…**.
- **Remove…**, **Disconnect…** and **Stop listening…** ask on a card where
  the safe button comes first and has the focus.

## Models

**Add slot** (`a`, the `+ add slot…` row, or **AI: Add Model Slot…** in the palette)
opens a small form:

```text
 Add model slot
 ──────────────────────────────────────────────
 Name       work
 Provider  ◦ anthropic
           ◉ openai
           ◦ openrouter
           ◦ ollama
           ◦ lmstudio
           ◦ openai-compatible
           ◦ claude-oauth
 Model      gpt-4.1-mini
 suggested: gpt-4.1 · gpt-4.1-mini · gpt-4o-mini
 No key here: Set key stores it in the secret store.

                 Cancel      Save
```

- `Tab` moves between the fields; `←` / `→` pick the provider; `Enter`
  saves. A click works on every field.
- An empty model means the provider's default (the field shows it).
- **Base URL** appears for `openai-compatible` (needed), `ollama` and
  `lmstudio` (empty = the usual local address). It must start with
  `http://` or `https://` and can not hold a user or password.
- A wrong field shows a red line in the form, and nothing is saved.
- **Save** writes `ai_models.<name>` in `config.json`. Every other setting
  and slot stays as it was. **Cancel** or `Esc` writes nothing.
- When the provider has no key yet, the next card offers **Set key**. The
  key goes to the secret store, never into `config.json`. **Later** closes
  the card; the slot's page keeps its **Set key** pill.

**Edit…** (`e` on the row) opens the same form with the slot's values. The
name is fixed; other fields you wrote by hand in the slot stay. `main` is
your provider: **Edit…** opens **AI: Configure Provider…**. On `quick` or
`local` the form saves your own slot under that name; **Remove…** brings
the built-in one back.

**Remove…** (`D` on the row) asks first, with **Keep** first and focused.
When a profile uses the slot, the card names it: that profile will not run
until you change it. The provider's key stays in the secret store.

The key state is all Itermoc shows: `key ✓`, `key ✗` or `no key needed` on
the page, `no key` in the list.
Key values, environment values and passwords in a base URL never appear.
**Edit JSON** (`E`) still opens `config.json` at `ai_models` for the
expert path.

A profile file that cannot be read (a broken `profiles.json`, a bad line in
the project's `ai-profiles.json`) is a row in PROFILES, with the file name,
the line and a **Fix** pill. `Enter`, `f` or a click on the pill opens the
file at that line; the row is gone once the file loads again. The toast
stays as before.

## Keys

Press `?` in the view for the key card.

| Key | What it does |
|---|---|
| `j` / `k`, arrows | move between rows |
| `Enter` | the actions of the row (on a group header: fold it; on a `+ …` row: add) |
| `o` | open the page of the row and move the keys to it |
| `Space` | fold or open the section |
| `Tab` / `Shift+Tab` | next / previous section |
| `/` | filter by name; `Enter` keeps it, `Esc` clears it |
| `R` | read everything again |
| `Esc` | back to the editor |
| `Alt+Shift+I` / `Alt+O` | leave Itermoc and go back to the view you had (the same keys open it) |
| `u` `e` `r` `d` `D` `t` | on a profile: use, edit, runs, details, duplicate, trust |
| `n` / `J` | new profile from the active one / edit `profiles.json` |
| `f` | on a `profiles` row that says it cannot be read: open the file at the broken line |
| `a` / `E` | add a model slot / edit `ai_models` in `config.json` |
| `e` / `k` / `D` | on a model slot: edit it, set the key, remove it (asks first) |
| `i` / `c` / `l` | on a service: sign in; connect a service; show the log |
| `x` | on an outside agent: revoke its always allow (asks first) |
| `t` / `d` / `D` | on a service: show its tools; disable it (asks first) or enable it; remove it (asks first) |
| `s` / `S` / `y` / `T` | start Claude Code, start Codex, copy the termocode server config, show the status screen |

On a page the same keys work (`u` `e` `r` `t`, `k` `i` `l` `s` `y`, `Enter` for
the menu, `Esc` back to the list). On the overview, `1` to `4` open a
group, `Tab` / `Shift+Tab` pick a line of NEEDS YOU and `Enter` opens its
page.

The mouse works too: click a row for its page, a header to fold it, a
`+ …` row to add, a pill or a box on a page to run it; the wheel scrolls.

## Narrow windows

In a short window only PROFILES is open; the other groups are folded
and keep their number (`1 !`). Open one with `Space` or a click. On a
narrow sidebar a row first cuts its word at the right. Its mark and name
always stay. You can drag the sidebar wider. A page in a narrow pane
draws the pipeline as chips and scrolls.

A window under 120 columns (80×24, say) has no room for the list next to
a page, so the list and the page take turns, like [Runs](runs.md). Itermoc
opens as one full-width tab (`✦ Itermoc`) that shows the list. Each row
gets a middle column (the steps of a profile, the provider and model of a
slot, what a service waits for) and, when it needs you, its fix pill at
the right:

```text
 ITERMOC                                          ? keys  / filter
 ──────────────────────────────────────────────────────────────────
 ✦ Overview                                                    3 !

 ✦ PROFILES                                                    1 !
  ● review        Reviewer ▶ Fixer                          active
  ⚠ house-review  2 steps · not trusted yet          ▐ Trust… (t) ▌
 Enter open · u use · Space fold · / filter · ? keys
```

`Enter` (or a click) opens the row's page full-width; `Esc` goes back to
the list on the same row. The row keys (`u`, `i`, `k`, …) work in the list
too. Under 60 columns the right column goes first, then (under 44) the
middle one; the mark and the name always stay.

## Ask the AI

The chat reads the same state as Itermoc. Ask "what does the review
profile do?", "which models do I have?", "what is connected?" or "who is
using termocode?". The AI answers with `profiles_list`, `models_list`,
`mcp_services_list` and `outside_agents_status`. These tools run at once
and change nothing. They never contain a key, token, header, env value
or a URL with a password; a model slot only says `set` or `missing`.

When the next step is yours (set a key, sign in, trust, revoke), the AI
opens this view at the row with `ai_view_open`. It does not move your
keyboard focus.

The AI can also propose a change. You always see a card first, with
**Deny** first and focused. Deny changes nothing. These cards have no
**Always allow**: every call asks, even if you allowed other tools
always.

| You say | Tool | After you allow |
| --- | --- | --- |
| "use the fast profile" | `profile_switch` | The profile switches for your next message. A project profile you have not trusted asks for trust first. |
| "make me a profile: Claude reviews, GPT fixes" | `profile_draft` | The profile form opens with the draft. Nothing is saved until you press **Save** there. The card shows the draft; for one of your own profiles it shows `-` and `+` lines. |
| "add a slot gpt on openai gpt-5-mini" | `model_slot_draft` | The slot form opens with these values. Nothing is saved until you press **Save**. You set the key yourself after that. |
| "connect GitHub" | `service_connect` | The Connect wizard opens. You sign in; the AI gets no token. |
| "turn on writes for GitHub" | `service_set_access` | Writes are on (each write still asks), or the service is read only again. |
| "start Codex with my IDE tools" | `outside_agent_start` | The agent starts in a new terminal tab, as **Agent: Start …** does. |

No tool takes a key, token or password. If the AI puts one in a call,
termocode refuses the call before any card. **Remove**, **Sign out**,
**Revoke** and **Trust** have no AI tool: they stay your decision. A
profile step with read-only tools does not get these tools, and outside
agents never do. See [AI tools](../reference/ai-tools.md).

## Where the old commands went

Nothing was removed. **AI: Switch Profile…**, **AI: Show Profile**,
**AI: Edit Profiles**, **AI: New Profile from Current…**, **AI: Edit Model
Slots**, **AI: Connect a Service…**, **AI: MCP Servers…**,
**MCP: Server Status…**, **MCP: Copy termocode Server Config** and the
**Agent: Start …** commands keep their names and do what they did. The
rows of Itermoc open the same screens.
