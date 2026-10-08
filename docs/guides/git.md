# Git: stage, commit, sync, conflicts, clone

termocode has a full git panel. You can stage single hunks, commit, push,
pull, fix merge conflicts and clone a repository.

## Open the Source Control panel

Click the **Source Control** icon in the activity bar. Or run
**Git: Focus Source Control** from `F1`.

The panel shows, from top to bottom:

- the branch, with `↑` / `↓` ahead / behind counts,
- a commit message box,
- an action bar: **✓ Commit**, **⟳ Sync** and a **▾** menu,
- the sections **CONFLICTS** (only during a merge), **STAGED**, **CHANGES**
  and **GRAPH**.

The panel refreshes by itself every two seconds.

## Stage and commit

1. Move to a file with `j` / `k` (or the arrow keys).
2. Press `s` to stage it. Press `s` again to unstage it.
3. Press `c` to go to the message box. Type your message.
4. Press `Enter` to commit.

If nothing is staged, termocode asks: *Stage all changes?*

The **▾** menu has **Commit (Amend)**, **Commit (Sign-off)**, **Push**,
**Pull**, **Fetch** and **Refresh**.

Press `t` to switch between a tree view and a flat list. termocode remembers
your choice.

## Stage one hunk

You do not have to stage the whole file.

1. Open the file, or press `d` on it in the panel to see a side-by-side diff.
   The left side is `HEAD`. The right side is your working copy.
2. Move to a change. Use `Alt+]` / `Alt+[` (or `]c` / `[c` in the diff).
3. Press `Alt+S` to stage the hunk under the cursor.
   `Alt+U` unstages it. **Git: Discard Hunk (at cursor)** throws it away.

The colored bars in the left gutter show added, changed and deleted lines.

## Sync with the remote

Click **⟳ Sync**, or run **Git: Sync (Pull, then Push)**. It pulls, then
pushes. On the first push of a new branch it sets the upstream for you.

You can also run **Git: Push**, **Git: Pull** or **Git: Fetch** on their own.

## Branches, history and stash

| Command | What it does |
|---|---|
| **Git: Switch Branch...** | Pick a branch. The first row creates a new one. |
| **Git: Show Log...** | Pick a commit to see its full diff |
| **Git: Show File History...** | Commits that changed this file |
| **Git: Compare with Revision...** | Diff against a branch, tag, `HEAD~3` or a SHA |
| **Git: Blame Current Line** (`Alt+B`) | Who changed this line, and when |
| **Git: Stash Changes** / **Pop Latest Stash** / **Show Stash List...** | Stash work |
| **Git: Show Tags...** | Check out a tag |

Hover over a commit in **GRAPH** to see its details.

## Resolve a merge conflict

When a merge or rebase stops with conflicts, a **CONFLICTS** section appears
at the top of the panel.

1. Press `Enter` on a file in **CONFLICTS**. It opens at the first conflict.
   The `<<<<<<<` … `>>>>>>>` blocks are colored.
2. Pick a side. Use `Ctrl+.` on the conflict, or the palette:
    - **Git: Accept Current Change (Conflict at Cursor)** — keep yours
    - **Git: Accept Incoming Change (Conflict at Cursor)** — keep theirs
    - **Git: Accept Both Changes (Conflict at Cursor)**
3. Jump to the next conflict with `]x`, or back with `[x` (Normal mode).
4. Save the file. Press `s` on it in the panel to mark it resolved.
5. Commit, or continue the rebase in the terminal.

Each accept is one edit, so one `u` brings the markers back.

## Clone a repository

1. On the welcome screen, pick **Clone repository**. Or run
   **Git: Clone Repository...**.
2. Paste the URL. Press `Enter`.
3. Check the target folder. Press `Enter`.
4. A toast shows the progress. When it is done, termocode opens the new folder.

For a private HTTPS repository, termocode asks for a token. The token is only
passed to that one `git clone`. It is not saved.

## Keys at a glance

In the Source Control panel:

| Key | Action |
|---|---|
| `j` / `k` | Move |
| `g` / `G` | Top / bottom |
| `Enter` | Open the file, fold a section, or show a commit |
| `←` / `→` | Collapse / expand |
| `s` | Stage / unstage |
| `d` | Diff (file) or show commit |
| `c` | Go to the message box |
| `x` | Discard changes (asks first) |
| `r` | Refresh |
| `t` | Tree / flat view |

In the editor:

| Key | Action |
|---|---|
| `Alt+]` / `Alt+[` | Next / previous change |
| `Alt+S` / `Alt+U` | Stage / unstage hunk |
| `Alt+B` | Blame line |
| `]x` / `[x` | Next / previous merge conflict |

## Troubleshooting

**The panel is empty**
:   The folder is not a git repository, or `git` is not on your `PATH`.

**Push fails with an auth error**
:   termocode uses your normal git setup. Fix it in a shell first
    (`git push`), for example with an SSH key or a credential helper.

**`Alt+S` types a letter instead of staging**
:   Some terminals (macOS Terminal by default) do not send `Alt`. Turn on
    "Use Option as Meta key", or use the palette: **Git: Stage Hunk (at cursor)**.

