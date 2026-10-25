# Itermoc: profiles, models, services and outside agents

Itermoc is one sidebar view for everything that shapes the AI chat. It
lists your agent profiles, the model slots they run on, the services the
chat can reach and the outside agents that use termocode's tools. Each row
opens the same screens you already know. Itermoc itself changes nothing.

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
 ───────────────────────────────────────────────────────────────
 ▾ PROFILES                                      active: review
   ● review      Reviewer (sonnet-4-5) → Fixer (gpt-5-mini)
     default     one step (main model: sonnet-4-5)
   ⚠ house       project · not asked yet · 2 steps [Trust… (t)]
   [New profile (n)]  [Edit JSON (J)]
 ▾ MODELS                                            1 key ✗
   ● main        anthropic · claude-sonnet-4-5       key ✓
   ! gpt         openai · gpt-5-mini                 key ✗ [Set key (k)]
   [Edit slots (e)]
 ▾ SERVICES                                      1 needs you
   ● GitHub      amin-jalali · read only · 28 tools
   ! Linear      sign-in expired                  [Sign in (i)]
   [Connect a service (c)]  [Log (l)]
 ▾ OUTSIDE AGENTS                          auto · 1 agent ⇄
   ● listening   mode auto · 1 agent connected
   ⇄ claude-code since 09:41 · 14 calls, 2 asked
     09:42 claude-code · tasks_run · asked, you allowed
   [Start Claude Code (s)]  [Start Codex (S)]  [Copy config (y)]
 Enter actions · Space fold · ? keys
```

| Section | What it lists |
|---|---|
| PROFILES | the agent profiles; `●` marks the active one, `⚠` a project profile you were not asked about yet |
| MODELS | the model slots the steps run on, with `key ✓` or `key ✗`. A key is never shown |
| SERVICES | the services in `mcp.json`: ready, off, needs a sign-in, failed |
| OUTSIDE AGENTS | whether termocode listens for agents like Claude Code, who is connected, and their last calls |

A row that needs you has one button at its end: **Trust…**, **Set key**
or **Sign in**. Click it, or press its key on the row.

## Actions of a row

Press `Enter` (or click) on a row. A small menu lists what you can do with
it. Safe actions come first; the ones that ask for confirmation end in `…`.

| Row | Actions |
|---|---|
| Profile | Use · Details · Duplicate… · Runs · Trust… (project profiles) · Edit JSON |
| Model slot | Set key (when the key is missing) · Pick model… (the main slot) · Edit slots |
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
- **Remove…**, **Disconnect…** and **Stop listening…** ask on a card where
  the safe button comes first and has the focus.

## Keys

Press `?` in the view for the key card.

| Key | What it does |
|---|---|
| `j` / `k`, arrows | move between rows |
| `Enter` | the actions of the row (on a section header: fold it) |
| `Space` | fold or open the section |
| `Tab` / `Shift+Tab` | next / previous section |
| `/` | filter by name; `Enter` keeps it, `Esc` clears it |
| `R` | read everything again |
| `Esc` | back to the editor |
| `u` `d` `D` `t` | on a profile: use, details, duplicate, trust |
| `n` / `J` | new profile from the active one / edit `profiles.json` |
| `k` / `e` | on a model slot: set the key / edit the slots |
| `i` / `c` / `l` | on a service: sign in; connect a service; show the log |
| `s` / `S` / `y` / `T` | start Claude Code, start Codex, copy the termocode server config, show the status screen |

The mouse works too: click a row for its actions, a header to fold it, a
button to run it; the wheel scrolls.

## Narrow windows

In a short window only PROFILES is open; the other sections are folded
and keep one number on their header (`1 missing key`, `1 needs you`,
`auto · 1 agent`). Open one with `Space` or a click. On a narrow sidebar a
row first drops its description, then its state words. Its mark and name
always stay. You can drag the sidebar wider.

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
