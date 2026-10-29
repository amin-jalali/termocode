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

## Stage single lines

A hunk is sometimes too big. Stage just the lines you want.

1. Put the cursor on a changed line, or select several lines with `V`.
2. Press `Alt+Shift+S`. Only those lines go to the index. The cursor moves
   to the next line, so you can press the key again.
3. `Alt+Shift+U` takes the line (or the selected lines) out of the index.
4. **Git: Discard Line (or Selected Lines)** throws the change away. It asks
   first, and **Cancel** is the choice that is ready.

Save the file first: the lines are taken from the saved file. A removed line
counts as part of the line below it. A binary file or a renamed file cannot
be cut into lines, and you cannot discard lines of a new file that git does
not track yet; termocode says so.

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
| `R` | Rebase from this commit (see [Interactive rebase](#interactive-rebase)) |
| `F` | Fix the staged changes up into this commit (see [Fixup](#fixup-into-an-older-commit)) |
| `Esc` | Close the commit view, or clear the compare marks |

Right-click a commit for a menu: **Open Changes**, **Copy Commit Hash**,
**Copy Commit Message**, **Show Raw (git show)**, **Rebase from Here…**
and **Refresh**.

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

## Interactive rebase

Clean up your commits before you push: change their order, join them,
change a message, or remove one.

1. In GRAPH, move to the oldest commit you want to change. Press `R`, or
   right-click it and pick **Rebase from Here…**. From the palette, run
   **Git: Interactive Rebase…** and pick the commit.
2. A card lists that commit and every newer one, oldest first. Each row
   starts as `pick`. Set an action with one key:

    | Key | Action | What it does |
    |---|---|---|
    | `p` | pick | Keep the commit |
    | `r` | reword | Keep it with a new subject (the body stays). termocode asks for the subject |
    | `e` | edit | Stop after this commit so you can change it |
    | `s` | squash | Join it into the row above; keep both messages |
    | `f` | fixup | Join it into the row above; drop its message |
    | `d` | drop | Remove the commit |

3. Move a row with `J` / `K` (or `Alt+↓` / `Alt+↑`). Move the cursor with
   `j` / `k`. The line under the list shows the result, for example
   `5 commits → 3 commits`.
4. Press `Enter`. A card says what will happen. **Cancel** is first and has
   the focus. Pick **Start Rebase** to run it.

Good to know:

- **Already pushed?** When some of the commits are on the upstream branch
  already, the card warns you. After the rebase you need to force-push.
  termocode never force-pushes for you.
- **Uncommitted changes.** The button is then **Stash and Continue**: git
  puts your changes aside and brings them back after the rebase.
- **Merge commits** cannot be rebased here yet. termocode says so and
  does nothing.
- **Undo.** git keeps the old branch tip. Press `u` in Source Control to
  go back (see [Undo a git action](#undo-a-git-action)). The toast at the
  end also names the git command: `git reset --hard ORIG_HEAD`.

### When the rebase stops

A conflict, or an `edit` row, stops the rebase. A banner shows at the top
of the Source Control panel, for example `⇅ Rebasing 1/3 · 1 conflict`.

1. Fix the conflicts as in [Resolve a merge conflict](#resolve-a-merge-conflict).
   For an `edit` row, change the commit with **Commit (Amend)**.
2. Press `Enter` on the banner (or run **Git: Rebase in Progress (Continue
   / Skip / Abort)…**). A card opens:
    - **Close** (first, with the focus) changes nothing. `Enter` and `Esc`
      never throw your conflict fixes away.
    - **Abort** puts the branch back as it was.
    - **Skip** drops the commit the rebase stopped on and goes on.
    - **Continue** goes on. It stages the files you fixed. It refuses
      while a file still has conflict markers.

## Fixup into an older commit

You found a small mistake in an older commit. Fix it in the files, then:

1. Stage the fix.
2. In GRAPH, move to the commit the fix belongs to and press `F`. Or run
   **Git: Fixup Staged Changes Into…** and pick the commit.
3. A card says what will happen (Cancel first). Pick **Fixup**.

termocode makes a `fixup!` commit and runs an autosquash rebase: the fix
ends up inside the older commit and the history keeps its messages. Your
unstaged changes are stashed and put back (**Stash and Fixup**). The same
warnings and the same undo apply as for an interactive rebase.

## Undo a git action

git writes every move of HEAD to the reflog: a commit, an amend, a reset,
a checkout, a merge, a rebase, a cherry-pick, a pull. termocode reads it
and can take the last one back.

1. Press `u` in the Source Control panel, or run **Git: Undo Last Git
   Action…**.
2. A card says what the last action was, in plain words, for example
   `Last action: commit 'fix x' (2 min ago).` It also says what the undo
   does: `HEAD (main) goes back to a1b2c3d 'old subject'.`
3. **Cancel** is first and has the focus. Pick **Undo** to do it.

How it works:

- The undo runs `git reset --keep`. It never throws local changes away.
  The commits it leaves stay in the reflog for 90 days.
- Undo of a checkout switches back to the branch you left.
- **Local changes in the way.** When you changed a file the undo must
  change, the button is **Stash and Undo**: termocode stashes your
  changes, undoes, and puts them back. If they do not go back cleanly,
  the stash stays (`stash@{0}`) and the toast says so.
- **Already pushed?** When commits that leave the branch are on the
  upstream already, the card warns you: you would need to force-push.
  termocode never force-pushes.
- **Redo.** After an undo, click the toast or run **Git: Redo Undone Git
  Action**. It works while HEAD has not moved since.
- A stopped merge, rebase, cherry-pick or revert must be finished (or
  aborted) first. termocode says so and does nothing.

**Git: Reflog…** lists the last 50 actions, newest first. Pick one and
press `Enter`: the card asks **Move HEAD back to here?** with the same
checks.

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
5. Commit. In an interactive rebase, use **Continue** on the rebase banner
   (see [When the rebase stops](#when-the-rebase-stops)).

Each accept is one edit, so one `u` brings the markers back.

## Clone a repository

1. On the welcome screen, pick **Clone repository**. Or run
   **Git: Clone Repository...**.
2. Paste the URL. Press `Enter`.
3. Check the target folder. Press `Enter`.
4. A toast shows the progress. When it is done, termocode opens the new folder.

For a private HTTPS repository, termocode asks for a token. The token is only
passed to that one `git clone`. It is not saved.

## CI and pull requests

termocode shows the GitHub CI of your current branch. It uses the `gh`
command. Install it and run `gh auth login` once. Without `gh`, the CI card
only says "Install and sign in to gh to see CI".

After you push, a chip shows next to the branch in the status bar:

| Chip | Meaning |
|---|---|
| `CI …` | A run is still going |
| `CI ✓` | All runs passed |
| `CI ✗ 2` | Two runs failed |

Click the chip, or run **Git: CI and Pull Request…**. The card shows:

- the pull request of the branch: number, title, state and review state
- each check with ✓, ✗ or …

The buttons:

- **Fix with AI** (only when CI is red): reads the log of the failed job and
  puts it in the AI chat input. It has the job, the step and up to 200 log
  lines around the error. Secrets are hidden. Nothing is sent yet: read the
  message, then press `Enter`.
- **Open Log**: shows the failed job's log in a read-only tab.
- **Open in Browser**: opens the pull request (or the run). Over SSH it
  copies the link instead.

When CI turns red, a toast says so once. Press `Alt+C` (or click the
toast) to start **Fix with AI**. `Alt+C` on a green branch opens the card.

termocode asks GitHub right after a push, every 20 seconds while a run is
going, and every 5 minutes otherwise. It does not ask while the terminal
window is in the background.

### Work with pull requests

termocode uses the `gh` command, so you need `gh` installed and signed in.
Without it you see one quiet hint.

**Git: Pull Requests…** lists the open pull requests. Each row has the
number, title, author and branch, and the state: Draft, checks (✓ ✗ …) and
review. Type to filter. `Enter` opens a pull request.

The pull request card shows the title, state, branches, checks, changed
files, the description and the reviews. Its buttons:

- **Files…**: opens the changes in the diff card (the changed parts of each
  file).
- **Actions…**: a list of what you can do:
  - **Open in Browser** (over SSH it copies the link)
  - **Read Description**: the whole text in a read-only tab
  - **Check Out Branch**: refused when tracked files have changes
  - **Approve…**, **Request Changes…**, **Comment…**: a small text input.
    Request Changes and Comment need a message.
  - **Mark Ready for Review** / **Convert to Draft**
  - **Merge…**: a card with **Cancel** first. Choose squash, merge commit or
    rebase. The card warns when checks failed or still run, when there are
    conflicts, or when a reviewer asked for changes. termocode never
    bypasses the rules of the repository.

**Git: Create Pull Request…** starts from your current branch. The title is
the first commit, the description lists all commit subjects, and the base is
the default branch. Edit the title, then choose **Create** or **Create as
Draft**. If the branch is not pushed, termocode offers a plain push first
(never a forced one).

The AI can open the list. It cannot create, merge or review a pull request.

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
| `R` | Interactive rebase from the commit |
| `F` | Fixup staged changes into the commit |
| `u` | Undo the last git action (asks first) |
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
| `Alt+Shift+S` / `Alt+Shift+U` | Stage / unstage line (or selected lines) |
| `Alt+B` | Blame line |
| `Alt+C` | Fix red CI with AI |
| `]x` / `[x` | Next / previous merge conflict |

## Troubleshooting

**The panel is empty**
:   The folder is not a git repository, or `git` is not on your `PATH`.

**Push fails with an auth error**
:   termocode uses your normal git setup. Fix it in a shell first
    (`git push`), for example with an SSH key or a credential helper.

**No CI chip**
:   Run `gh auth status` in a shell. The branch must be pushed to GitHub
    and have workflow runs.

**`Alt+S` types a letter instead of staging**
:   Some terminals (macOS Terminal by default) do not send `Alt`. Turn on
    "Use Option as Meta key", or use the palette: **Git: Stage Hunk (at cursor)**.

