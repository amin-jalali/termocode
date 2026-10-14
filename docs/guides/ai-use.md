# Use AI

Set up a provider first: [Set up AI](ai-setup.md). Then you have four tools:

1. **Ghost text** — inline suggestions while you type.
2. **Chat panel** — ask questions about your code.
3. **AI code actions** — explain, fix, edit or document code, then review the diff.
4. **Agent** — the chat can read files, search, run commands and edit, with your OK.

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

## Chat panel

1. Press `Alt+A` (or run **AI: Open Chat**). The chat opens on the right.
2. Type your question. Press `Enter` to send.
3. The answer streams in. Press `Ctrl+C` to stop it.
4. Press `Esc` to go back to the editor. `F6` also moves focus.

To give the AI context, open a file or select code, then run
**AI: Add File / Selection to Chat**.

To copy from the chat, drag over the text: it is copied when you let go.
Double-click copies a word, triple-click a whole paragraph. **Copy** on an
answer copies the whole answer (Markdown); right-click a message for
Copy Message / Copy Selection / Copy Code Block.

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
| `Ctrl+U` | Clear the input |
| `Esc` | Clear the selection, then back to the editor |

Chats are saved. Run **AI: Chat History…** to open an old one, and
**AI: New Chat** to start fresh. The last 50 chats are kept.

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
changed again after the turn, a dialog lists it and asks first. A file
whose tab has unsaved edits is skipped and named. The chat gets a line
saying what was restored, so the agent knows.

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

Read and editor tools run freely. So "open it" opens the file in a tab.
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

## Keys at a glance

| Key | Action |
|---|---|
| `Alt+A` | Open / close the chat panel |
| `Tab` | Accept ghost text |
| `Alt+\` | Suggest now |
| `Alt+|` | Toggle automatic ghost text |
| `Alt+I` | Edit selection with AI |
| `Ctrl+.` | AI code actions (Explain / Fix / Edit / Doc) |

## Troubleshooting

**No ghost text appears**
:   Check that automatic suggestions are on (`Alt+|`). Check the `✦` badge in
    the status bar — if it says *AI off*, set up a provider. Local models can
    take a few seconds.

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
    Alacritty); GNOME Terminal does not support it.

**The agent stopped in the middle**
:   It hit the step limit (25 by default, `ai_max_steps`). Send "continue".
