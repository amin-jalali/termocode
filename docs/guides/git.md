# Git: stage, commit, sync, conflicts, clone

termocode has a full git panel. You can stage single hunks, commit, push,
pull, read the history, compare commits, fix merge conflicts and clone a
repository.

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

## The GRAPH

**GRAPH** lists the commits, newest first. Each row shows a node, the
branch and tag names, the message and the age (`2h`, `3d`, `4w`).

- **Date groups.** Commits sit under **Today**, **Yesterday**, **This
  week**, then one group per month. Press `Enter` or `←` on a group (or
  click it) to fold it. `→` or a second click opens it again.
- **Show more.** GRAPH shows the recent commits first (8 to 15). Click
  **Show 15 more** for more. When all loaded commits are on screen the row
  says **Load older commits**: it loads the next page.
- **Nodes.** `◉` is `HEAD`. A fork glyph is a merge. Other commits get a
  small dot. In ASCII mode they are `@`, `*` and `o`.
- **Not pushed yet.** `⇡` on the right means the commit is not on the
  upstream branch yet. It goes away after a push.
- **Names.** Branches, tags and remote branches show as small pills. When
  they do not fit, the rest fold into `+N`. The hover card lists them all.
- **Hover card.** Rest the pointer on a commit. A card shows the author,
  the date, the message, all names, `+N −M  K files` and the short SHA.

Keys on a commit row:

| Key | Action |
|---|---|
| `Enter` or `d` | Open the commit view |
| `y` | Copy the short SHA |
| `Space` | Mark the commit for compare (see below) |
| `Esc` | Close the commit view, or clear the compare marks |

Right-click a commit for a menu: **Open Changes**, **Copy Commit Hash**,
**Copy Commit Message**, **Show Raw (git show)** and **Refresh**.

## The commit view

Open a commit from GRAPH. Its diff opens in a card:

- the SHA, the message, the author, the dates, the parents and the names,
- a file list with `+N −M` per file (`A` added, `M` changed, `D` deleted,
  `R` renamed),
- the diff, side by side or in one column, with syntax colours.

When the editor area is 80 cells or wider, the card sits over the editor.
The sidebar stays visible. Click another commit, or press `j` / `k` in
GRAPH, and the card follows. On a narrow screen the card floats over
everything.

Keys in the commit view:

| Key | Action |
|---|---|
| `n` / `p` (or `]` / `[`) | Next / previous change |
| `Tab` / `Shift+Tab` | Next / previous file |
| `t` | Side by side / one column |
| `z` | Unfold all unchanged lines |
| `o` | Open the file in the editor |
| `O` | Open the file as it was in this commit (read-only) |
| `y` | Copy the full SHA |
| `r` | Raw: the `git show` text in the editor |
| `b` | Show / fold the long message |
| `v` | Revert this commit |
| `c` | Cherry-pick this commit |
| `Esc` / `q` | Close |

The buttons at the bottom do the same: **Copy SHA**, **Open file**,
**File @ commit**, **Revert…**, **Cherry-pick…** and **Close**. Click a
parent SHA in the header to open that commit.

**Revert** makes a new commit that undoes this one. **Cherry-pick** puts
this commit on top of your branch as a new commit. Both ask first. If git
stops with conflicts, the **CONFLICTS** section appears and a toast says
how many. Fix them as in [Resolve a merge conflict](#resolve-a-merge-conflict).

### Compare two commits

1. Move to a commit in GRAPH. Press `Space`. The row shows a small `1`.
2. Move to another commit. Press `Space` again.

The card opens on all changes from the older commit to the newer one. The
title says `Compare a1b2c3d → f31bb77`. Press `y` (or **Copy range**) to
copy the range. `Esc` in the sidebar clears a mark.

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
| `Enter` | Open the file, fold a section or group, or show a commit |
| `←` / `→` | Collapse / expand |
| `s` | Stage / unstage |
| `d` | Diff (file) or show commit |
| `y` | Copy the commit's short SHA |
| `Space` | Mark a commit for compare |
| `Esc` | Close the commit view / clear marks |
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

