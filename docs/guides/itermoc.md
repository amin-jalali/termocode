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

Open it from the command palette: **Itermoc: Open**. The palette also has
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
- **SETTINGS**: the categories (`quick` and `deep` slots), the budget, where
  the profile comes from and the last run.
- **RECENT RUNS**: the last runs of this profile in the chat, with time
  taken and cost; a run that stopped says why, in red.

Three pills at the bottom run the common verbs: **Use** (`u`), **Edit**
(`e`) and **Runs** (`r`). `Enter` opens the actions menu (the list after
`⋯ more` says what is in it). A project profile that asks for your trust
has **Trust…** (`t`) first. A click on a box in the pipeline edits that
step. **Edit** opens `profiles.json` for now.

The pages of a model slot, a service, the outside agents and the group
headers (a summary with what needs you) show their key facts and their
main pills. A key is never on a page. `Esc` on a page goes back to the
list; `j`, `↓`, `↑` and the wheel scroll a page that is taller than the
window.

## Actions of a row

Press `Enter` on a row in the list (or `Enter` on its page). A small menu
lists what you can do with it. Safe actions come first; the ones that ask for confirmation end in `…`.

| Row | Actions |
|---|---|
| Profile | Use · Details · Duplicate… · Runs · Trust… (project profiles) · Edit JSON |
| Model slot | Set key (when the key is missing) · Edit… · Pick model… (the main slot) · Edit JSON · Remove… (your own slots) |
| Service | the service's own actions: Connect, Show tools, Sign in, Allow writes / Read only, Trust, Disable / Enable, Remove… and more (see [Connect MCP servers](mcp.md)) |
| Listening row | Show status… · Turn on / Start listening now · Start Claude Code · Start Codex · Copy config · Stop listening… |
| Outside agent | Show status… · Allowed tools… · Disconnect… |

Every action opens a screen that exists without Itermoc:

- **Use** switches the chat to the profile. A project profile you did not
  trust yet shows the trust card first; its first button is *Not in this
  project*.
- **Details** is [AI: Show Profile](ai-profiles.md) for that profile. It
  does not switch to it.
- **Duplicate…** asks for a name and shows the same preview card as **AI:
  New Profile from Current…**, for the profile you picked.
- **Runs** opens the AGENTS view.
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
| `u` `e` `r` `d` `D` `t` | on a profile: use, edit, runs, details, duplicate, trust |
| `n` / `J` | new profile from the active one / edit `profiles.json` |
| `a` / `E` | add a model slot / edit `ai_models` in `config.json` |
| `e` / `k` / `D` | on a model slot: edit it, set the key, remove it (asks first) |
| `i` / `c` / `l` | on a service: sign in; connect a service; show the log |
| `s` / `S` / `y` / `T` | start Claude Code, start Codex, copy the termocode server config, show the status screen |

On a page the same keys work (`u` `e` `r` `t`, `k` `i` `l` `s` `y`, `Enter` for
the menu, `Esc` back to the list).

The mouse works too: click a row for its page, a header to fold it, a
`+ …` row to add, a pill or a box on a page to run it; the wheel scrolls.

## Narrow windows

In a short window only PROFILES is open; the other groups are folded
and keep their number (`1 !`). Open one with `Space` or a click. On a
narrow sidebar a row first cuts its word at the right. Its mark and name
always stay. You can drag the sidebar wider. A page in a narrow pane
draws the pipeline as chips and scrolls.

## Ask the AI

The chat reads the same state as Itermoc. Ask "what does the review
profile do?", "which models do I have?", "what is connected?" or "who is
using termocode?". The AI answers with `profiles_list`, `models_list`,
`mcp_services_list` and `outside_agents_status`. These tools run at once
and change nothing. They never contain a key, token, header, env value
or a URL with a password; a model slot only says `set` or `missing`.

When the next step is yours (set a key, sign in, trust, revoke), the AI
opens this view at the row with `ai_view_open`. It does not move your
keyboard focus. Outside agents do not get these tools. See
[AI tools](../reference/ai-tools.md).

## Where the old commands went

Nothing was removed. **AI: Switch Profile…**, **AI: Show Profile**,
**AI: Edit Profiles**, **AI: New Profile from Current…**, **AI: Edit Model
Slots**, **AI: Connect a Service…**, **AI: MCP Servers…**,
**MCP: Server Status…**, **MCP: Copy termocode Server Config** and the
**Agent: Start …** commands keep their names and do what they did. The
rows of Itermoc open the same screens.
