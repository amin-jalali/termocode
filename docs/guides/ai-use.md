# Use AI

Set up a provider first: [Set up AI](ai-setup.md). Then you have four tools:

1. **Ghost text** — inline suggestions while you type.
2. **Chat panel** — ask questions about your code.
3. **AI code actions** — explain, fix, edit or document code, then review the diff.
4. **Agent** — the chat can read files, search, run commands and edit, with your OK.

To send one message through several agents (for example one model reviews,
another fixes), pick a profile: see [AI profiles and pipelines](ai-profiles.md).

## Ghost text (inline completion)

1. Type in Insert mode and pause. After a short moment, a grey suggestion
   appears after the cursor.
2. Press `Tab` to accept it.
3. Keep typing, move the cursor or press `Esc` to ignore it.

| Key | Action |
|---|---|
| `Tab` | Accept the suggestion |
| `Alt+\` | Ask for a suggestion now |
| `Alt+|` | Turn automatic suggestions on or off |

`Tab` still does its normal jobs when there is no suggestion: expand a
snippet, accept a completion, jump to the next placeholder, or insert a tab.

## Next edit suggestions

After you change code, the AI looks for the next place that must change
too. For example: you rename a parameter, and its uses below still have the
old name.

1. Make an edit and pause. A marker `⇥ next edit` appears at the end of the
   line that should change. The status bar says **Tab: go to next edit**.
2. Press `Tab`. The cursor goes to that line. Below it you see the change:
   the old text struck through, the new text in grey.
3. Press `Tab` again to apply it. One `u` (undo) takes it back.
4. Press `Esc` to dismiss it, or just keep typing.

If ghost text shows at the cursor, `Tab` accepts the ghost text first.

- It is on when ghost text is on. Turn it on or off with the palette:
  **AI: Toggle Next Edit Suggestions**, or the `ai_next_edit` setting.
- It uses the ghost-text model (`ai_inline_model`).
- It sends only the last few edits and about 60 lines around the cursor,
  never the whole file.
- It never sends files that may hold secrets (`.env`, keys, certificates,
  names with *secret*, *token*, *password* …) or very big files.
- It only suggests a change in the same file, near the cursor, and never on
  the line you just edited.

## Chat panel

1. Press `Alt+A` (or run **AI: Open Chat**). The chat opens on the right.
2. Type your question. Press `Enter` to send.
3. The answer streams in. Press `Ctrl+C` or `Esc` to stop it.
   One quiet line under the last message says what the model does:
   `◷ waiting for the model · 4 s`, `✎ writing`, `⚙ read_file · 2 s`
   (a tool runs) or `↻ retry 2 of 3` (the server was busy; termocode
   tries again up to 3 times). When no text comes for 90 s (or 30 s in
   the middle of an answer) it turns yellow: `no answer for 35 s — Esc
   to stop`. A running tool never shows this warning.
4. Press `Esc` to go back to the editor. `F6` also moves focus.

The header shows the profile chip, then `⌁ N MCP` (the services the chat
can use) and `usage` when there is room. In a narrow window a chip that
does not fit becomes `⋯`: a click opens a small menu with Profile…,
Services, Usage and Agents, so nothing is only in the palette. Hover a chip
for its label and key.

To give the AI context, open a file or select code, then run
**AI: Add File / Selection to Chat**.

To copy from the chat, drag over the text: it is copied when you let go.
Double-click copies a word, triple-click a whole paragraph. **Copy** on an
answer copies the whole answer (Markdown); right-click a message for
Copy Message / Copy Selection / Copy Code Block / Fork Chat from Here.

To put code from the answer into your file, run
**AI: Apply Last Code Block to File**, or click **Apply** on the code
card. You see the change in the review card first (see below).

| Key (chat focused) | Action |
|---|---|
| `Enter` | Send |
| `Shift+Enter`, `Alt+Enter` or `Ctrl+J` | New line |
| `↑` / `↓`, `PgUp` / `PgDn` | Scroll the chat |
| `Ctrl+C` | Copy the selection; with none, stop generating |
| `Ctrl+N` | New chat |
| `Alt+K` | Fork the chat from a message |
| `Ctrl+U` | Clear the input |
| `Esc` | Clear the selection, then stop a running answer, then back to the editor |

### Persian, Arabic and other right-to-left text

You can write and read Persian or Arabic in the chat. Most terminals do
not do this themselves: they show the letters backwards and not joined.
So termocode does it in the chat and in the Markdown preview:

- Letters are joined in their initial, medial and final forms; the
  lam-alef ligature and Persian-specific letters are supported. A ZWNJ
  keeps word parts apart as typed.
- Each line is shown in reading order, right to left. English words,
  numbers and `inline code` keep their own order.
- A paragraph that starts with a Persian or Arabic letter is right
  aligned. Your messages, answers, headings and list items too (the
  list marker goes on the right).
- Code blocks keep their left-to-right order. Only a Persian comment or
  string inside them reads right to left.
- Copy always gives the text as written, not as shown.

Limits: in the input box the current line is shown joined and in reading
order, but the arrow keys move in typing order (logical), not on screen.
Tables keep their columns left to right. termocode does not handle
explicit direction marks (LRE, RLO, …).

The `rtl` setting in `config.json` chooses who does this. termocode asks
the terminal not to reorder the text again (the BDSM escape `CSI 8 l`;
GNOME Terminal and other VTE terminals and WezTerm follow it):

| `rtl` | Meaning |
|---|---|
| `auto` (default) | `terminal` on Konsole and mlterm (they always reorder), else `termocode` |
| `termocode` | termocode joins and reorders; the terminal is asked not to |
| `terminal` | text is sent as written; the terminal must do it |
| `off` | text is sent as written; nothing else changes |

The setting is read when termocode starts.

Chats are saved. Run **AI: Chat History…** to open an old one, and
**AI: New Chat** to start fresh. The last 50 chats are kept.

### Fork a chat

A fork is a new chat that starts with the history of the old one, up to
one message. Use it to try another question or another way, and keep the
old chat as it is.

- Right-click a message and pick **Fork Chat from Here**.
- Or press `Alt+K` in the chat, or run **AI: Fork Chat from Message…**.
  A list of your questions and the answers opens, newest first. Pick one.

What you get:

- The fork opens in the panel. Its title is the old title with
  "(fork)". The first line says "Forked from <title> at message N".
- Fork from an **answer**: the fork has the history up to that answer.
  The input box is empty.
- Fork from **your question**: the fork has the history before it, and
  the question is in the input box. Change it, then press `Enter`.
- The old chat does not change. Both chats are saved and stay after a
  restart. Find the old one in **AI: Chat History…**.
- If the AI is still answering, termocode stops the answer first (like
  `Ctrl+C`) and keeps it in the old chat. The fork starts at the last
  finished message.
- The fork uses the same profile and model as before.

### Files in and out of the chat

Add files to your next message in three ways:

- **Drag and drop.** Drop files from your file manager onto the terminal
  while the chat input has focus. Each file becomes a chip above the input.
  Pasting a file path works the same way.
- **Attach button.** Click the paperclip at the right end of the input
  box and pick a file.
- **`@path`.** Write `@src/main.go` (or `@file src/main.go`) in your
  message. The file is attached when you send.

Text files are sent as text (up to 60 KB). Images (PNG, JPEG, GIF, WebP)
are sent as images to Anthropic, OpenAI and OpenRouter. Other providers
get a short note instead. Other binary files are refused.
Click `×` on a chip to remove it, or press `Backspace` in the empty input.

To get files out:

- **Save as…** on a code block writes it to a file in the workspace. The
  name comes from the answer (for example `cmd/main.go`) or from the
  language (`snippet.go`). It asks before it replaces a file, and opens
  the new file.
- **AI: Export Chat as Markdown** writes the chat to
  `ai-chat-<date>.md` in the workspace and opens it.

## AI code actions (review, then apply)

1. Put the cursor on a line, or select some lines.
2. Press `Ctrl+.` (or `Alt+Enter`). The **AI** group shows:
    - **Explain with AI** — the answer goes to the chat.
    - **Fix with AI** — only on a line with an error or warning.
    - **Edit with AI…** — type an instruction, e.g. *add error handling*.
    - **Generate Doc Comment with AI**
3. For Fix, Edit and Doc, the [review card](#review-ai-changes) opens
   with the proposal.
4. Accept or reject each change, or the whole file.

What you accept is one edit in the open tab (not saved yet, as if you
typed it). Press `u` once to undo it. If the file changed while the AI was
working, termocode does not apply the edit.

Shortcut: `Alt+I` runs **Edit with AI** on the selection or the current line.

### Fix from the Problems panel

In the **Problems** panel (`Alt+M`), select a problem and press `a`. Or
click **✦ Fix with AI** at the end of the selected row, or right-click the
row and pick **Fix with AI**. termocode jumps to the problem and runs the
same Fix flow: the review card, then the accepted change as one undo step.

## Review AI changes

Every change the AI suggests opens in the **review card** first: a diff
over the editor, like in mobocode.

- **Split / Unified** (top right, or `t`): old and new side by side, or one
  column. Wide windows start split.
- Changed words are brighter than the changed line, so you see the real
  edit at a glance.
- Long runs of unchanged lines fold into a band ("14 unchanged lines").
  Click it, or press `z`, to unfold.
- Each change has a header, **Change 2 of 5**, with **Accept** and
  **Reject**. `n` / `p` (or `‹` / `›`) jump between changes; `a` / `r`
  accept or reject the selected one. Click again (**Undo**) to change your
  mind. When every change is decided, the file is done.
- The footer has **Accept file** (`A`), **Reject file** (`R`), **Apply
  selected** (`Enter`: only what you accepted) and **Accept & edit** (`e`:
  write it, then open the file so you can change it by hand).
- When one answer changes several files, a list on the left shows them.
  `Tab` goes to the next file. A dot is waiting, ✓ accepted, ◐ partly,
  ✗ rejected.
- `Esc` hides the card. The chat shows "**N files to review**" with a
  **Review** button; **AI: Review Pending Changes** opens it again.

### The agent waits for you

With the default setting, the agent's file changes are not written at
once. `write_file`, `create_file`, `format`, `lsp_rename` and applying a
code action (`lsp_code_actions`) open the review card, and the agent waits
until you decide (at most 30 minutes; **Stop** ends the wait at once).
There is no extra "AI wants to" question for them: the review is the
question.

The agent is told exactly what happened, for example *"The user accepted
1 of 2 changes to main.go; only those were written. Rejected: change 2
…"*. When you reject everything, nothing is written and the agent is told
not to try the same change again.

What you accept is written to disk. An open tab shows it as **one undo
step** (`u`), and the explorer, git panel and open files follow, as
before. A file whose tab has unsaved edits is refused: the agent asks you
to save first. A code action that runs a language-server command can not
be shown as a diff; it asks you with the usual dialog instead. Delete,
rename of files and folders, git and run tools keep the usual dialog.

To turn the review off, run **AI: Toggle Edit Review (diff / auto)**, or
set `"ai_edit_review": "auto"` in `config.json`. Then file changes ask with
the dialog as before. Auto-approving *fileWrite* (**AI: Agent
Auto-Approve…**) also skips the review: changes are written at once.

**File: Compare Two Files…** uses the same card, read-only.

### Undo a whole AI turn

Before an agent turn first changes a file, termocode keeps the file's old
text (a *restore point*). This covers `write_file`, `create_file`,
`delete`, `rename` and every change you accept in the review card.

- Under the turn, the chat shows **↶ Undo AI changes (N files)**.
- Or run **AI: Undo Last Turn** from the palette.

Undo puts every file back: changed files get their old text (an open tab
shows it as one undo step), files the turn created are deleted. If a file
changed again after the turn, a dialog lists it and asks first; Cancel has
the focus, so Enter keeps everything. A file whose tab has unsaved edits
is left as it is, and the dialog says so: save or discard the edits first.
A change the AI could not make (for example a file you had not saved) is
not part of the turn, so there is nothing to undo for it. The chat gets a
line saying what was restored, so the agent knows.

Restore points are saved with the chat (in
`~/.local/state/termocode/ai-restore/`), so the button still works after a
restart. The last 30 turns per chat are kept; files over 2 MB are not.
Shell commands (`run_command`, the terminal) and LSP edits made with
`ai_edit_review` = `auto` are not part of a restore point: use git for
those.

### Audit log

Every tool call of the agent is one line in the **Output** panel, channel
**AI** (**View: Output**, then the *AI* chip):

```text
14:03:22  write_file  content=<10 lines, 49 B> path=a.txt  gate=review  by=user (review)  → The user accepted 1 of 2 changes to a.txt; …
14:03:25  git_commit  message="fix typo"  gate=confirm  by=user denied  → declined by user
```

- time, tool, the arguments (long text is shown as its size),
- **gate**: *auto* (runs at once), *confirm* (the dialog), *review* (the
  diff card), *invalid* (refused before any question),
- **by**: who decided: *auto*, *user allowed*, *user denied*,
  *auto-approved (class)*, *user (review)*,
- then the first line of the result.

**AI: Undo Last Turn** adds a line too. The log is kept while termocode
runs (the last 5000 lines).

## Agent: let the chat use tools

The chat is an agent. It can call tools to answer you:

| Kind | Tools |
|---|---|
| Read | read a file, list a folder, search, git status / diff / branches, go to definition, diagnostics |
| Code (language server) | hover info (`lsp_hover`), file outline or workspace symbol search (`lsp_symbols`), go to definition / type definition / implementation / references (`lsp_nav`), rename a symbol in every file (`lsp_rename`), list and apply code actions (`lsp_code_actions`) |
| Write files | write, create, delete, rename files and folders, format |
| Git | stage, commit, checkout, push, pull |
| Run | run a shell command in the workspace (build, test, …); you see only its result |
| Terminal | type a command in your terminal panel (`terminal_send`), read its last lines (`terminal_read`), list / open / show / close terminal tabs (`terminal_tabs`) |

| Read | read a file, list a folder, search, git status / diff / branches / log (`git_log`) / blame (`git_blame`), go to definition, diagnostics |
| Write files | write, create, delete, rename files and folders, format |
| Git | stage, unstage (`git_unstage`), commit, checkout, push, pull, fetch (`git_fetch`) |
| Git hunks | stage, unstage or discard one diff hunk (`git_hunk`) |
| Git stash | list, push, apply, pop, drop (`git_stash`) |
| Run | run a shell command in the workspace (build, test, …) |

| Tasks | list tasks with group, command and last run (`tasks_list`), run one in its terminal tab (`tasks_run`), read its exit code and output (`tasks_status`) |
| Tests | run tests: all, a file, the test at the cursor, the failed ones or by name (`tests_run`); read the results with each failure's file:line and message (`tests_results`) |
| Debug | list, set (with a condition or log message), remove and clear breakpoints (`debug_breakpoint`); start a launch configuration or debug a file (`debug_start`); read where it stopped, the call stack, variables and watches (`debug_state`); continue, pause, step over / into / out, stop, restart (`debug_control`); evaluate an expression (`debug_evaluate`) |
| Editor | open a file (at a line), read the cursor and selection, show a panel, reveal in the explorer, show a toast |
| IDE commands | find a command palette command (`list_actions`) and run it (`execute_action`): fold, split, show a panel, save, run tests, … |
| Settings | list settings with their values and allowed values (`settings_list`), change one (`settings_set`): theme, tab size, icons, word wrap, … |
| Layout | sidebar width, panel height, show / hide the sidebar and the bottom panel, zen mode, show the AI panel (`layout_set`) |
| IDE state | read focus, panels, terminal tabs, layout, theme, git branch, language servers, problems, debug and running tasks / tests (`get_ide_state`) |
| Setup | list, install and remove language servers (`lsp_servers`) and debug adapters (`dap_adapters`); list, run and reload extensions, read their log (`extensions`); add or remove explorer folders (`workspace_folders`) |
| Containers | list every container on the host with its ports (`containers_list`), inspect one with env values masked (`containers_inspect`), read logs (`containers_logs`) and stats (`containers_stats`), list images, volumes, networks, contexts and registries; start / stop / restart / pause / unpause a container (`containers_control`), pull an image (`images_pull`), start a compose project (`compose_up`) |

Read and editor tools run freely. So "open it" opens the file in a tab.
For questions like "how do I … in termocode?", the chat can use
`help_read` to list built-in guide pages, show a page's headings and
anchors, return a section, or search for matching sections. Its results
are data from the built-in guide, not instructions.
**Before any write, git or run tool, termocode asks you.** File changes
open in the [review card](#review-ai-changes); other tools show a dialog
with what the tool will do. You can always say no.

Each palette command has a rule for the agent:

- **auto** — safe, view-only commands (fold, split, toggle the explorer,
  next problem) run at once.
- **confirm** — commands that change files, git or run code ask you first.
  Risky ones (discard changes, push, revert a file) always ask: there is
  no "Always allow" for them.
- **never** — the agent can not quit, reload or detach the session, open
  a shell or browser outside the IDE, import / export settings, or touch
  AI keys, sign-in or auto-approve. Commands that open a picker only you
  can answer are left out too.

Settings and layout have rules too:

- **Settings** — `settings_set` always asks you first. A theme change
  repaints at once and every change is saved to `config.json`, like the
  Settings screen does. The agent can not see or change the AI provider,
  model, base URL, inline model, auto-approve, step limit, the terminal key
  list or any key or credential. A wrong value is refused with the list of
  good values.
- **Layout** — `layout_set` runs at once (it only changes the view). Sizes
  are kept in the same limits as a mouse drag, and the sidebar width and
  panel height are saved like a drag. The agent can not hide the AI panel:
  your chat is in it. Zen mode hides the chat too until you leave zen.

The terminal tools use your real terminal panel, so you see the command run.
The agent uses them for dev servers, watchers and tests you want to watch;
for a quick command it uses the hidden **run** tool.

- `terminal_send` asks first (kind *run*). It opens the panel and a tab
  when needed. Your keys stay where they were.
- `terminal_read` runs at once. It can wait (up to 30 s) until the
  command is done, then it refreshes the explorer, git and open files.
- `terminal_tabs`: list, new and show run at once. Close asks (kind
  *ide*). Closing a tab that still runs a command always asks: there is
  no "Always allow" for it.

Language server tools have rules too:

- **Read** — hover, symbols, go to definition and listing code actions run
  at once.
- **Change** — `lsp_rename` and applying a code action open the review
  card with every file they change. Accepted files are written; open tabs
  show the change as one undo step. A file with unsaved edits is refused.
  No new tab opens for the other files. An action that does not exist is
  refused before you are asked. With `ai_edit_review` = `auto` they ask
  first (kind *fileWrite*) and change the files through the editor; a
  file with unsaved edits then keeps them unsaved.
- With no language server for the file, the agent is told so, and which
  server **LSP: Manage Language Servers...** can install for it.

Git tools have rules too:

- `git_log`, `git_blame` and `git_stash` list run at once (read only).
- `git_unstage`, `git_fetch`, hunk stage / unstage and stash push / apply /
  pop ask first. *git* auto-approve covers them.
- Hunk **discard** and stash **drop** can not be undone, so they always ask,
  with no "Always allow".
- Hunk discard and stash push / apply / pop are refused while a file has
  unsaved edits in the editor; the agent asks you to save first.

Tasks and tests use the same places as the palette, so you see them run.

- `tasks_run` asks first (kind *run*). It runs a task from
  `.termocode/tasks.json` (or a detected one) by label, or the default
  build / test task, in its own terminal tab.
- `tests_run` asks first (kind *run*). It uses the Testing view's runner
  and shows the Testing view.
- `tasks_list`, `tasks_status` and `tests_results` run at once.
  `tasks_status` and `tests_results` can wait (up to 2 min) until the run
  ends. Stop ends the wait.

The debug tools use the same session as **Run & Debug**, so you see it all.

- `debug_breakpoint` list, set and remove run at once, like F9.
  Clear asks first (kind *ide*). A breakpoint shows in the gutter and in
  the Run & Debug view, and it is saved like yours. A file that is not
  open gets the breakpoint without a new tab. Breakpoints move with a
  renamed file or folder, and go away with a deleted one.
- `debug_state` runs at once. It shows where the program stopped, the top
  frames, the variables of the top frame (a few levels deep, capped) and
  your watches. With no session it shows the configurations.
- `debug_start` asks first (kind *run*): it runs your program. It starts a
  configuration by name, or debugs a file (Go: its package). Then it waits
  for the first stop.
- `debug_control` asks first. Continue, steps and restart are kind *run*;
  pause and stop are kind *ide*. It waits (up to 2 min, Stop ends the
  wait) for the next stop and returns the new state.
- `debug_evaluate` asks first (kind *run*): an expression can call code in
  your program. The line and its result also show in the Debug Console.
- At each stop the editor shows the stopped line, like when you debug.
  Your keys stay in the chat.

Setup tools change the IDE itself:

- `lsp_servers` / `dap_adapters`: list runs at once. It shows the servers
  for the languages in your workspace and their state. Install asks
  first (kind *run*: it downloads and runs code). It runs in the
  background, with the same toasts as **LSP: Manage Language Servers...**;
  the agent waits up to a minute, then checks again with list. Uninstall
  always asks, with no "Always allow".
- `extensions`: list and log run at once. Run a command and reload ask
  first (kind *run*: they run extension code).
- `workspace_folders`: list runs at once. Add and remove ask first (kind
  *ide*). The new folder shows in the explorer. The agent can not open
  another workspace: that restarts termocode and would end the chat.

Container tools work on the whole host, not only this project:

- The list, inspect, logs, stats and the other list tools run at once.
  Each result says the engine and the context it used (the session's
  context, when you picked one in the Containers view).
- `containers_control`, `images_pull` and `compose_up` ask first. The card
  has **Deny** (first, with the focus), **Allow** and **Always allow containers**. "Always" lasts
  until you quit termocode; it is never saved. There is no "Always in
  project": a host action can touch another project's containers.
- The agent can not remove, prune, push, tag, run an image, sign in or
  out of a registry, switch contexts or read disk usage. There is no tool
  for these, and their palette commands are *never*.
- Env values never reach the model: inspect keeps the names and masks the
  values (`POSTGRES_PASSWORD=[REDACTED]`). See
  [Containers](containers.md#the-ai-and-host-containers).

The [AI tools reference](../reference/ai-tools.md) lists every tool, its
arguments and its rule.

To skip the question for one kind, run **AI: Agent Auto-Approve…** and turn
on *fileWrite*, *git*, *run*, *ide* or *settings*. Turn on only what you trust.

The IDE follows what the agent does: a new file shows in the explorer at once,
a renamed open file keeps its tab under the new name, a deleted one closes its
tab, and a written one shows the new text (undo with `u`). Git and shell tools
refresh the git panel, the branch and open files. The agent never overwrites,
deletes or saves a file whose tab has unsaved edits; it asks you to save first.

The agent stops after 25 tool steps per message and says so. Send "continue"
to go on. Change the limit with `ai_max_steps` in `config.json`.

## Project rules file

A project can tell the AI how to work in it: put the rules in `AGENTS.md`
or `.termocode/rules.md` in the workspace root. termocode reads them at the
start of every chat turn and puts them in the AI's system prompt. When both
files exist, both are used (AGENTS.md first).

The text steers the AI, so it is used only when you say so:

- The first message in a workspace with a rules file opens a card **Use this
  project's rules file?**. It shows the file, its size and the first 8 lines.
- **Not in this project** (the first button, and the one Enter picks) sends
  your message without the rules.
- **Always in this project** sends it with the rules.
- **Esc** sends nothing and puts your message back in the input.

The answer is kept per workspace for that exact text. When the file
changes (you edit it, a pull brings a new version, or the AI writes it),
the next message asks again.

Under your message, the chat shows a dim line such as
`rules: AGENTS.md (4 KB)` or `rules: AGENTS.md (not used)`. Click it to open
the file. Run **AI: Project Rules…** to change your answer.

Limits:

- At most 16 KB go into the prompt. A longer text is cut at a line end and
  ends with `(cut at 16 KB)`.
- Terminal escape codes, control characters and bidi marks are removed.
- Known secrets and key-looking strings are replaced by `[REDACTED]`.
- A rules file that is a symlink to a place outside the workspace is
  ignored.
- The rules are only prompt text. They give the AI no tools and never
  answer a confirm card for you. Pipeline runs ([AI profiles](ai-profiles.md))
  get them in every step.

## Memory

The AI can keep short lessons from your chats in a project, for example
"run go test with -race; CI does". Later chats in the same project get the
lessons that fit. Memory is off until you turn it on, per workspace.

**Turn it on.** The first time a chat turn could give lessons, a quiet card
shows in the chat under the answer: **Save lessons for later chats?** with
**Turn on** and **Not now** (click one). It is not a dialog: it takes no
focus and you can keep typing or open the palette. Nothing is saved while
you have not decided. **Not now** is kept for the workspace and the card does
not come back. **Turn on** turns memory on and saves the lessons of that
turn. You can also decide in the palette with **AI: Memory: Turn On/Off**,
and the card goes. The card only comes when a quick model slot is set (see
below).

**Where lessons go.** Into `.termocode/memory/lessons.md` in the workspace,
one section per lesson. When you turn memory on, termocode adds
`.termocode/memory/` to the workspace's `.gitignore` (it makes the file
when there is none, adds one line at the end, and changes no other line).
The toast says what it did. The lessons stay on your machine unless you
remove that line.

**When lessons are written.** After a turn that used a tool or ended with
an error, the **quick** model slot (AI: Edit Model Slots) reads the turn
and writes at most 3 lessons. Plain question-and-answer turns are skipped.
This runs beside the chat: you can send the next message at once. It stops
after 30 seconds. When lessons are saved, a dim line
`memory: 2 lessons saved · view` shows under the answer. Each run costs a
small request; **AI: Usage** lists it as the agent `memory`. Without a
quick slot, one toast says lessons are not saved.

**When lessons are used.** With memory on, each message sends up to 10
lessons (about 2 KB) in the system prompt, after the project rules:
lessons about the open or attached file first, then lessons that share
words with your message, then the newest ones. The line under your message
says `memory: 3 lessons`; click it to see them.

**View, edit, forget.** **AI: Memory…** lists the lessons, newest first.
Pick one to see it, then **Open in editor** (the file opens at that
lesson) or **Forget…**. **Forget this lesson?** has **Keep** first; only
**Forget** removes it. The last row, **Forget all…**, asks the same way.
You can also edit `lessons.md` by hand.

**Secrets.** The turn is passed through the secret filter before it goes
to the quick model, and the lessons again before they are saved and before
they go into a prompt. A lesson that still holds a secret is dropped.
Lesson text never goes to the logs or the AI audit channel; the audit line
only says `memory · 2 lessons saved`.

The AI has no memory tool: it can not turn memory on or off or forget a
lesson. It can change `lessons.md` only like any file, through the review
card.

## Skills

Knowledge files the AI loads only when a task needs them: see
[AI skills](ai-skills.md).

## Hooks after AI tool calls

A hook is a command that runs after an AI tool call worked, for example a
formatter after the AI writes a file:

```json
{"post_tool": [
  {"tools": ["write_file", "create_file"], "run": "gofmt -w \"$TERMOCODE_TOOL_PATH\""},
  {"tools": ["*"], "run": "make lint", "timeout_s": 60}
]}
```

- **Your hooks**: `~/.config/termocode/ai/hooks.json`. They always run.
- **Project hooks**: `.termocode/hooks.json` in the workspace. They come
  from the repository, so the first message opens a card **Run this
  project's hooks?** that lists the commands. **Not in this project** is
  first and is what Enter picks. The answer is kept for that exact file;
  a changed file asks again. **AI: Hooks…** opens the card again.
- `tools` names the AI tools (`*` or no list: every tool). The hook runs
  in the workspace with `sh -c` (`cmd /C` on Windows) and gets
  `TERMOCODE_TOOL`, `TERMOCODE_TOOL_PATH` (the call's file, as a full
  path) and `TERMOCODE_WORKSPACE`. It is stopped after `timeout_s`
  (default 30, at most 300).
- Every run is one line in **View: Output** → **AI** (the audit log):
  `hook after=write_file run=… by=user → exit 0 · 40ms`.
- When a hook fails (exit code not 0), its output (at most 4 KB, secrets
  blanked) is added to the tool result, so the AI can fix a lint error.
- Hooks run only after built-in tools, not after MCP tools.

## Web tools

The AI has no web access until you turn it on. Run **AI: Web Tools…**:

- **Web fetch: on/off** gives the AI `web_fetch`. It reads one `http`
  or `https` page as text (GET only). It never reaches private or local
  addresses (`localhost`, `10.x`, `192.168.x`, …), reads at most 2 MB,
  returns at most 20 000 characters and blanks known secrets. It does not
  use a proxy.
- **Web search: Brave Search / Tavily / Exa** gives the AI `web_search`
  with that provider. Picking one without a key asks for it (the text is
  hidden). The key is kept in your secret store as `web_search_key`; the
  provider's environment variable (`BRAVE_API_KEY`, `TAVILY_API_KEY`,
  `EXA_API_KEY`) works too. A search sends the query to the provider.
- Both only read, so a call runs without a card. Each call is a line in
  the AI audit log. Page text is data for the AI, not orders.

The same screen can add servers (MCP) for this work. Each shows the exact
`mcp.json` entry first, with **Cancel** first:

| Row | Server |
|---|---|
| Library docs | `https://mcp.context7.com/mcp` |
| Code search | `https://mcp.grep.app` (public GitHub code) |
| Web search | `https://mcp.exa.ai/mcp` (no key, rate limited) |
| Browser automation | `npx -y @playwright/mcp@latest --isolated` (needs Node) and the user skill `visual-qa` |

The `visual-qa` skill tells the AI how to check a page in the browser:
phone and desktop widths, console errors, the main buttons.

## Keys at a glance

| Key | Action |
|---|---|
| `Alt+A` | Open / close the chat panel |
| `Tab` | Accept ghost text, or go to / apply the next edit |
| `Alt+\` | Suggest now |
| `Alt+|` | Toggle automatic ghost text |
| `Alt+I` | Edit selection with AI |
| `Alt+Shift+P` or `Alt+N` | Next AI profile ([AI profiles](ai-profiles.md)) |
| `Ctrl+.` | AI code actions (Explain / Fix / Edit / Doc) |

## Troubleshooting

**No ghost text appears**
:   Check that automatic suggestions are on (`Alt+|`). Check the `✦` badge in
    the status bar — if it says *AI off*, set up a provider. Local models can
    take a few seconds.

**No next edit marker appears**
:   Check that it is on (**AI: Toggle Next Edit Suggestions**). It waits for
    a short pause after an edit and shows nothing when the AI is not sure.

**`Alt+\` or `Alt+|` does nothing**
:   Your terminal may not send `Alt` keys. Use the palette:
    **AI: Trigger Inline Completion** and **AI: Toggle Inline Completions**.

**"Fix with AI" is not in the list**
:   It only shows on a line with a diagnostic. Use **Edit with AI…** instead.

**The edit was not applied**
:   The file changed after you asked. Ask again.

**Copy says "no clipboard found" or "no system clipboard"**
:   termocode found no system clipboard (the editor can still paste it). Install `wl-clipboard` (Wayland) or `xclip`
    (X11). Over SSH, use a terminal with OSC 52 (kitty, WezTerm, foot,
    Alacritty); GNOME Terminal does not support it. On Android (Termux),
    install the Termux:API app and run `pkg install termux-api`.

**The agent stopped in the middle**
:   It hit the step limit (25 by default, `ai_max_steps`). Send "continue".
