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

## Agent: let the chat use tools

The chat is an agent. It can call tools to answer you:

| Kind | Tools |
|---|---|
| Read | read a file, list a folder, search, git status / diff / branches, go to definition, diagnostics |
| Write files | write, create, delete, rename files and folders, format |
| Git | stage, commit, checkout, push, pull |
| Run | run a shell command in the workspace (build, test, …) |

Read tools run freely. **Before any write, git or run tool, termocode asks
you.** You see what the tool will do, and you can say no.

To skip the question for one kind, run **AI: Agent Auto-Approve…** and turn
on *fileWrite*, *git* or *run*. Turn on only what you trust.

The agent stops after 6 tool steps per message. Ask again to continue.

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
:   It hit the 6-step limit. Send "continue".
