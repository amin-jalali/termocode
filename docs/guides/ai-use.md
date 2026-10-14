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

To put code from the answer into your file, run
**AI: Apply Last Code Block to File**. You see a diff first.

| Key (chat focused) | Action |
|---|---|
| `Enter` | Send |
| `Shift+Enter`, `Alt+Enter` or `Ctrl+J` | New line |
| `↑` / `↓`, `PgUp` / `PgDn` | Scroll the chat |
| `Ctrl+C` | Stop generating |
| `Ctrl+N` | New chat |
| `Ctrl+U` | Clear the input |
| `Esc` | Back to the editor |

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
3. For Fix, Edit and Doc, a side-by-side diff opens. The right side shows
   the proposal.
4. Pick **Apply**, **Review diff** or **Discard**.

**Apply** is one edit. Press `u` once to undo it. If the file changed while
the AI was working, termocode does not apply the edit.

Shortcut: `Alt+I` runs **Edit with AI** on the selection or the current line.

### Fix from the Problems panel

In the **Problems** panel (`Alt+M`), select a problem and press `a`. Or
click **✦ Fix with AI** at the end of the selected row, or right-click the
row and pick **Fix with AI**. termocode jumps to the problem and runs the
same Fix flow: diff, then **Apply** as one undo step.

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
| Editor | open a file (at a line), read the cursor and selection, show a panel, reveal in the explorer, show a toast |
| IDE commands | find a command palette command (`list_actions`) and run it (`execute_action`): fold, split, show a panel, save, run tests, … |
| Settings | list settings with their values and allowed values (`settings_list`), change one (`settings_set`): theme, tab size, icons, word wrap, … |
| Layout | sidebar width, panel height, show / hide the sidebar and the bottom panel, zen mode, show the AI panel (`layout_set`) |
| IDE state | read focus, panels, terminal tabs, layout, theme, git branch, language servers, problems, debug and running tasks / tests (`get_ide_state`) |

Read and editor tools run freely. So "open it" opens the file in a tab.
**Before any write, git or run tool, termocode asks you.** You see
what the tool will do, and you can say no.

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
- **Change** — `lsp_rename` and applying a code action ask you first
  (kind *fileWrite*). The change goes through the editor: open tabs show
  it and you can undo it. A changed file is saved only when it had no
  unsaved edits before; your half-done edits stay unsaved. No new tab
  opens for the other files. An action that does not exist is refused
  before you are asked.
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

**The agent stopped in the middle**
:   It hit the step limit (25 by default, `ai_max_steps`). Send "continue".
