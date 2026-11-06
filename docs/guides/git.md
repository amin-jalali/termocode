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

The **▾** menu has **Commit (Amend)**, **Commit (Sign-off)**, **Commit and
Push**, **Commit and Sync**, **Push**, **Pull**, **Fetch**, **Unstage All**,
**Discard All Changes…**, **Delete Untracked Files…** and **Refresh**.

**Commit and Push** commits, then pushes. **Commit and Sync** commits, then
pulls and pushes. Nothing is pushed when the commit fails.

## Throw away all changes

**Git: Discard All Changes…** asks first, and **Cancel** is the choice that
is ready. **Discard Changes** takes back every change to tracked files,
staged ones too. New files stay, unless you press **Discard and Delete New
Files**. **Git: Delete Untracked Files…** removes only the new files.
Ignored files always stay.

Nothing is lost for good: termocode first saves a safety copy in the stash
list. The toast names it (for example `stash@{0}`). Open **Git: Show Stash
List…** and apply it to get your work back. **Git: Unstage All Changes**
takes every file out of the index and keeps your edits.

Press `t` to switch between a tree view and a flat list. termocode remembers
your choice.

## Write a better commit message

- **History.** In the message box, `↑` shows your older messages and `↓`
  goes back to the newer ones and then to your draft. **Git: Pick Recent
  Commit Message…** lists them.
- **Template.** When git has a `commit.template`, an empty box starts with
  its text (without the `#` lines).
- **Length.** While you type, the box shows how long the first line is. It
  turns amber past 50 characters, or when the second line is not blank, and
  red past 72.
- **Full editor.** **Git: Edit Commit Message in Editor** opens the message
  in an editor tab. Save the file and the box takes the text. Lines that
  start with `#` are left out. A new line shows as `⏎` in the box.
- **Co-authors.** **Git: Add Co-Author…** lists the people who committed in
  this repository and adds a `Co-authored-by:` line.

The **▾** menu has these too.

## AI help for git

These need an AI provider (**AI: Configure Provider…**). termocode never
commits, stashes or sends a chat message for you: you check the text first.

| Command | What it does |
|---|---|
| **Git: Generate Commit Message with AI** | The AI reads the staged changes (or all changes when nothing is staged) and writes a message in the box. `↑` brings your old text back. |
| **Git: Stash with AI Message** | The AI names the stash. You can change the name, then `Enter` stashes; `Esc` keeps the changes as they are. |
| **Git: Explain Changes with AI** | Puts your uncommitted changes in the chat box with a question. Press `Enter` to send. |
| **Git: Explain Line's Commit with AI** | The same for the commit that last changed the cursor line. In the **GRAPH**, right-click a commit for **Explain with AI**. |
| **Git: Write Pull Request Description with AI** | Puts the branch's commits and files in the chat box and asks for a title and a description. |

Only the changes (at most 48 KB) and your recent commit subjects go to the AI.

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
| **Git: Rename Branch…** | Give the current branch a new name |
| **Git: Create Branch From…** | Start a new branch at another branch or a tag |
| **Git: Delete Branch…** | Delete a local or a remote branch (see below) |

**Git: Delete Branch…** asks first, and **Cancel** is the choice that is
ready. A local branch is only deleted when its commits are on another
branch. If it has commits of its own, a second card asks before a **Force
Delete**. The toast names the commit it was at, so `git branch <name>
<hash>` brings it back. To delete a branch on the remote you also type its
full name, for example `origin/feature`.

Right-click a commit in the **GRAPH** for **Create Branch Here…** and
**Check Out This Commit (Detached)**.

| **Git: Merge Branch…** | Merge another branch into this one (see [Merge a branch](#merge-a-branch)) |

## Blame

Blame shows who last changed a line, and when.

| Command | What it does |
|---|---|
| **Git: Toggle Blame in Status Bar** | The status bar shows the cursor line's author and age, for example `Tess, 3 days ago`. Click it to open the commit. |
| **Git: Toggle Inline Blame** | The same, plus the commit subject, in dim text at the end of the cursor line |
| **Git: Toggle Blame Annotations** | Every line gets the hash, author and age of its commit at its start. A run of lines from one commit names it once. The colour shows the age: amber is new, blue is old. |
| **Git: Open Commit of Line** | Opens the commit that last changed the cursor line |
| **Git: Blame Before This Change** | Opens the file as it was before the line's commit, with annotations. Run it again to go further back. |
| **Git: Blame Options…** | Ignore whitespace changes (`-w`), detect moved or copied lines (`-M -C`), and the two toggles above |

The two toggles and the options are saved. Blame reads the saved file, so
save first after an edit. A line you changed and did not commit says **Not
committed yet**. When the repository has a `.git-blame-ignore-revs` file,
blame skips the commits it lists (for example a big formatting commit).

## Line history and older revisions

- **Git: Show Line History** lists the commits that changed the cursor line,
  or the selected lines. `Enter` opens a commit.
- **Git: Open Previous Revision of File** opens the file as it was one commit
  earlier, read-only. Run it again to step further back. The toast says
  which commit it is and how many there are. Renames are followed.
- **Git: Open Next Revision of File** steps forward again. After the newest
  commit you are back at the working file.

## Tags

| Command | What it does |
|---|---|
| **Git: Create Tag…** | Tags `HEAD`. Type a name, then a message. An empty message makes a lightweight tag; a message makes an annotated one. Right-click a **GRAPH** commit for **Create Tag Here…**. |
| **Git: Show Tag Details…** | Kind, tagger, date, message and commit of a tag |
| **Git: Push Tag…** | Pushes one tag, or **All tags**, to the branch's remote (else `origin`) |
| **Git: Delete Tag…** | Deletes a local tag after a card where **Cancel** is ready. The toast names the commit, so `git tag <name> <hash>` brings it back. |
| **Git: Delete Remote Tag…** | Deletes a tag on the remote. A card asks first (**Cancel** is ready), then you type the tag name. Your local tag stays. |

## Remotes and the upstream

**Git: Manage Remotes…** lists the remotes with their URL. Pick **+ Add
Remote…** to add one, or pick a remote to **Rename…**, **Change URL…** or
**Remove…** it. Remove asks first; it only changes this repository, not the
server.

**Git: Set Upstream Branch…** picks the remote branch the current branch
tracks. **Git: Unset Upstream Branch** stops that.

## Force push

**Git: Force Push (with Lease)…** replaces the branch on the remote with
yours, for example after an amend or a rebase. A card says how many remote
commits your branch does not have; **Cancel** is ready. Then you type the
branch name. termocode uses `--force-with-lease`: if someone pushed after
your last fetch, git refuses and nothing changes. Fetch, look, and try
again.

## Stash with options

**Git: Stash…** asks what to stash: **All changes**, **All changes and new
files**, **Staged changes only**, or **Only the current file**. Then type a
message, or leave it empty.

**Git: Stash Actions…** lists your stashes. Pick one to **Apply** it (it
stays), **Pop** it (apply, then drop), **View Changes**, **Create Branch
from Stash…** (the stash is applied on the new branch, then dropped), or
**Drop…** it. Drop asks first, and **Cancel** is ready. The toast says how
to bring a dropped stash back (`git stash store …`).

## Reset and uncommit

Right-click a commit in the **GRAPH** and pick **Reset Current Branch to
Here…**:

| Mode | Your changes |
|---|---|
| **Soft** | The commits after it are undone; their changes stay, staged |
| **Mixed** | The same, but the changes are not staged |
| **Keep** | Your local changes stay; git stops if one is in the way |
| **Hard…** | The changes in your files go. termocode first saves them as a stash and the old commit as a backup ref (`refs/termocode/backup/…`). |

A card says how many commits leave the branch and how to bring them back;
**Cancel** is ready. For **Hard…** you also type the branch name.

**Git: Uncommit Last Commit (Keep Changes)** undoes the last commit. Its
changes stay staged, so you can change them and commit again.

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
  end also names the git command: `git reset --keep ORIG_HEAD`. It refuses
  to run when it would throw away your uncommitted changes.

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

## Merge a branch

1. Run **Git: Merge Branch…**. Pick a branch: a local one, or a remote one
   such as `origin/main`.
2. A card says how many commits come in. **Cancel** is the choice that is
   ready. **Merge** moves your branch forward when it can, or makes a
   merge commit. **Merge (No Fast-Forward)** always makes a merge commit.
3. If files conflict, the merge stops. A **⇄ Merging** banner shows at the
   top of the panel, with the **CONFLICTS** section below it.

Press `Enter` on the banner, or run **Git: Merge in Progress (Continue /
Abort)…**. **Close** is ready, so `Enter` and `Esc` change nothing.
**Continue** stages the fixed files and makes the merge commit; it refuses
while a file still has conflict markers. **Abort** puts the branch back as
it was. A stopped cherry-pick or revert shows the same banner.

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
5. Commit, or use **Continue** on the merge banner. In an interactive
   rebase, use **Continue** on the rebase banner
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

### Ship a branch to merge-ready

**AI: Ship Branch to Merge-Ready** takes the current branch (your work,
or an AI step's work after a plan run) all the way to a green pull
request:

1. **Push.** A card lists the new commits. **Not now** is first; it
   stops the ship. The push is a plain push, never forced.
2. **Pull request.** If the branch has no open pull request, a card shows
   the title, base and description (from the commits). **Cancel** is
   first; **Create** or **Create as Draft** opens it.
3. **CI.** termocode watches the checks of the pushed commit.
4. **Fix.** When CI is red, the failing log goes to the AI chat as a new
   turn. The AI fixes it and commits; its edits get the usual review.
   Then the push card comes again (step 1).
5. **Stop.** When CI is green on the pull request, the ship stops with
   **Merge-ready**. It never merges: you merge when you want.

At most 3 AI fix turns run; after that the ship stops. Run the command
again to stop it at any time. The default branch is never shipped.

### Run a plan in a worktree, then ship it

**AI: Run Next Plan in a Worktree, Then Ship…** prepares the next plan
run. It picks the plan profile; then you type the task in the AI chat.

- The run works in one new git worktree on branch
  `termocode/agent-<run>-plan`. It starts from your last commit, so your
  uncommitted edits stay out of it. Your branch and files do not change.
- Its steps run one at a time, because they share that tree.
- When the run is done, a card asks **Ship the plan's branch?**
  **Not now** is first and keeps the branch (AI: Agent Worktrees… lists
  it; the command again shows the card again). **Ship** commits the work
  in the worktree, with the task as the commit message, and starts
  **AI: Ship Branch to Merge-Ready** on that branch. Each push and the
  pull request still ask first. CI fixes are made in the worktree too.

Run the command again before you send the task to turn it off. It needs
a git repository with at least one commit.

## Find the commit that broke it (bisect)

**Git: Bisect (Find the Commit That Broke It)…** treats `HEAD` as bad and
asks which commit was still good. git checks out a commit in the middle.
A picker asks about it:

| Row | What it does |
|---|---|
| **Good: this commit works** | The problem is not here; git picks a newer commit |
| **Bad: the problem is here** | git picks an older commit |
| **Skip: can't test this one** | git picks a commit next to it |
| **Run a Task to Decide…** | Pick a task. A card shows its command (**Cancel** is ready). git runs it on each commit: exit 0 is good, 125 skip, anything else bad. |
| **End Bisect** | Back to where you started |

When git knows, a card names the first bad commit: **Open Commit**,
**End Bisect**, or **Stay Here** to look around (run **Bisect…** again
for the picker). Commit or stash your changes first: git checks out
other commits.

## Compare branches

**Git: Compare Branches…** picks a branch, remote branch or tag, then
what to compare:

| Row | Shows |
|---|---|
| **Changes on X since it split from yours** | What a merge of X would bring (from the merge-base to X) |
| **Changes on yours since it split from X** | What your branch adds (swap of the row above) |
| **yours → X (every difference)** | The two tips, file by file |
| **Working tree → X** | Your files as they are now (saved, not committed) against X, in a read-only diff card |

The first three open in the commit view, with every changed file.

## Issues, new repositories and publishing

These use the `gh` command (signed in with `gh auth login`).

| Command | What it does |
|---|---|
| **Git: Issues… (Start Work on One)** | Lists the open issues. Pick one: **Start Work: New Branch** makes a branch like `12-login-crashes` (you can change the name) and switches to it; **Copy #12** copies the number. |
| **Git: Initialize Repository** | Runs `git init` (branch `main`) in a folder that is not a repository yet |
| **Git: Publish to GitHub…** | For a repository with no remote: type the name, pick **Private** or **Public**, then a card asks (**Cancel** is ready). gh creates the repository, adds it as `origin` and pushes. |

## Worktrees

The **WORKTREES** view (**Alt+Shift+W**) lists every worktree of the
repository: make, open, pin, link, sleep, archive, delete and clean them
up. See [Worktrees](worktrees.md).

## Cherry-pick, revert and restore

| Command | What it does |
|---|---|
| **Git: Cherry-pick from Branch…** | Pick a branch. A card lists its commits that your branch lacks (changes you already have are left out). **Cancel** is ready; **Cherry-pick All** applies them oldest first. |
| **Revert Merge…** (right-click a merge in **GRAPH**) | Pick the parent to keep (parent 1 is the branch it was merged into). A new commit undoes what the merge brought in. |
| **Cherry-pick a File from Here…** (right-click a commit) | Pick one file of the commit. Its changes go into your working tree and are staged; nothing is committed. |
| **Restore a File from Here…** (right-click a commit) | Pick one file. It is put back as it was in that commit. Uncommitted changes to it are saved in the stash list first. |

Each one asks on a card where **Cancel** is ready. A conflict stops the
pick; fix it in **CONFLICTS** and commit, or run `git cherry-pick --abort`.

## Edit history from the GRAPH

On a **GRAPH** commit (keys, or right-click):

| Key | Menu | What it does |
|---|---|---|
| `E` | **Edit Commit Message…** | Type the subject, then the body (`\n` starts a new line) |
| `S` | **Squash into Previous…** | The commit joins the one before it; both messages are kept |
| `D` | **Drop Commit…** | The commit and its changes leave the branch; newer commits stay |

**Git: Rebase Current Branch onto…** picks a branch and replays your
commits on top of it: **Rebase**, or **Rebase (Keep Merges)** to keep merge
commits. **Git: Autosquash fixup! Commits…** folds `fixup!` and `squash!`
commits into the commits they name.

Every card lists what changes, warns when the commits are on the remote
already (you will need to force-push) and stashes uncommitted changes
first. **Cancel** is ready. A conflict stops the rebase: see
[When the rebase stops](#when-the-rebase-stops). Undo with `u`.

## Patches

| Command | What it does |
|---|---|
| **Copy as Patch** (right-click a commit) | Copies the commit as a mail patch (`git format-patch`) |
| **Git: Save Changes as Patch…** | Writes your uncommitted changes to a file (`changes.patch`). It asks before it replaces a file. |
| **Git: Apply Patch from File…** | Checks the patch, lists the files it changes on a card (**Cancel** is ready), then applies it to your working tree |
| **Git: Apply Patch from Clipboard…** | The same with the patch on the clipboard |

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

