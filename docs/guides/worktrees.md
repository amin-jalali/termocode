# Worktrees

The **WORKTREES** view lists every worktree of the repository in the
sidebar: the main checkout, the worktrees you made in `<repo>.worktrees/`,
the agent lanes termocode made, and worktrees made outside termocode.
Open it with **Alt+Shift+W** or **Worktree: Show**. It lives under the
Source Control icon, which is lit while it shows.

- Groups: **PINNED**, **ACTIVE**, **ASLEEP**, **EXTERNAL** and **ARCHIVED**.
- Each card shows the name, the agent's state (working, waiting for you,
  done), the branch, commits ahead and behind its upstream, and the
  count of changed files. A coding agent in a terminal tab of this or
  another termocode window shows on the worktree its folder is in.
- A worktree whose folder was deleted shows as **gone**. `F` forgets
  it: termocode drops its notes and git's record of that one worktree.
  It never deletes files, and it refuses a worktree whose folder is there.
- A worktree made outside termocode shows in **EXTERNAL** with
  `no meta`. Open, add, pin and link work on it. `A` adopts it, so
  termocode keeps its notes; it stays in EXTERNAL.
- In a short window each worktree is one line; Enter shows its card.
- The list is read again every 10 seconds while it is on screen. Git
  runs in the background, at most 4 calls at once.

Pins and names are stored in `termocode/worktrees.json` inside the
repository's git folder, never in your project.

| Key | What it does |
|---|---|
| `↑` `↓` `j` `k` | Move (`g` `G` top, end) |
| `Enter` | Open the worktree in this window; the old session keeps running |
| `a` | Add the worktree as a folder of this window |
| `p` | Pin or unpin |
| `i` | Link an issue or pull request (see below) |
| `N` | The notes agents left on it, with their times |
| `n` | New worktree (see below) |
| `c` | New child worktree: it starts from the selected one's branch |
| `M` | New worktree in each repository of this window (see below) |
| `L` | Show the setup of a new worktree (its terminal tab) |
| `A` | Adopt a worktree made outside termocode |
| `F` | Forget a worktree whose folder is gone |
| `s` | Sleep: end its agent tabs and move it under ASLEEP; again: wake it |
| `x` | Archive: remove the folder, keep the branch; on an archived row: restore |
| `D` | Delete the worktree and its branch (asks step by step) |
| `C` | Clean up: tick merged and old worktrees, archive them together |
| `f` | Filter: All, Mine, Agents, Dirty, Asleep, External |
| `/` | Filter by name (`Esc` clears) |
| `R` | Read every worktree again |
| `?` | Show these keys |

## New worktree

**Worktree: New…** (`n` in the view) opens a form: **Name**, **From**
(the branch, remote branch or tag it starts from; **Start point…** lists
them), **Branch** (made from the name; type to change it), **Agent** and
**Task**. **Issue** is optional: give an issue number or URL and leave
the name empty, and the branch is made from its title
(`412-pinned-row-in-worktrees`); the new worktree is linked to it.
Nothing is made until you press **Create**; **Cancel** comes
first. The worktree goes to `<repo>.worktrees/<branch>` next to the
repository (setting `worktree_dir`). When git refuses (the branch or the
folder is there already, an unknown start point) the card says why and
nothing is left behind.

**Create + Start** also starts the agent in a terminal tab inside the
new worktree and types the task once the agent shows its input box. It
never types into a question or a y/n prompt. An agent whose screen
termocode cannot read gets no task; type it yourself.

**Agent: Start…** opened from the WORKTREES view starts the agent in the
selected worktree.

A repository can list files for each new worktree in
`.termocode/worktrees.json`:

```jsonc
{
  "include": [".env", ".env.local"], // copied
  "shared": ["node_modules"],        // linked to the main checkout's folder
  "setup": ["pnpm install"]          // run in the new worktree
}
```

- **include** files are copied (at most 20 MB each). **shared** files and
  folders are links, so both worktrees see each other's writes.
- A path that leads out of the repository is refused, and so is a file
  git already put in the worktree. The form lists what is copied, linked
  and skipped, by name only.
- **setup** commands come from the repository, so they run only after you
  trust them on a card that shows each command (**Not now** comes first).
  The answer is kept for this exact file: a changed file asks again. They
  run in a terminal tab of their own inside the new worktree, so you can
  watch them, with your environment minus termocode's secrets. The card
  shows `setup ● running`, `✓` or `✗`; `L` shows the tab.

## Child worktrees

`c` (**Worktree: New Child of Selected…**) opens the New worktree form
for a child of the selected worktree: it starts from that worktree's
branch, and the list draws it under its parent (`└`; deeper children
show as `└ …`). A child whose parent is gone moves up to the top level.

**Worktree: Jump…** (**Ctrl+Shift+J**) lists the worktrees and the
agents in them, from every termocode window, with the ones that wait for
you first.

## A worktree in each repository

A task often needs a change in more than one repository: a frontend and
a backend, or several services in one folder. When the folders of this
window hold two or more git repositories (a folder of projects, or
repositories added with **File: Add Folder…**),
**Worktree: New in Each Repository…** (`M` in the view) makes one
worktree in each of them, on the same branch, in one step.

- The card lists every repository with a **yes** / **no** row and the
  folder its worktree goes to (`<repo>.worktrees/<branch>`, or setting
  `worktree_dir`; when two repositories would share a folder, the
  repository's name is added). **From** is empty for each repository's
  current branch, or a branch or tag they all have.
- **Cancel** comes first; nothing is made until **Create**. Create
  checks every repository first and says which one is in the way (the
  branch or the folder is there already).
- All or nothing: when git fails in one repository, the worktrees made
  before go again, branch and folder, and the card says why.
- **Create + Add Folders** also adds the new worktrees as folders of
  this window.
- Each repository keeps its own git state. termocode tags the worktrees
  with their branch (`group` in each repository's `worktrees.json`), so
  they read as one task.
- The AI cannot use this card.

## Link an issue or pull request

`i` (**Worktree: Link Issue or PR…**) links the selected worktree to an
issue or a pull request: type its number, `#412`, or its GitHub URL. A
URL must be `https` on the repository's own GitHub host. termocode asks
`gh` with your own sign-in, so the card shows `#412 open` or
`PR #12 open · CI ✓`. The state is read again while the view shows: every
20 seconds while checks run, else every 5 minutes. Without `gh`, or when
it is not signed in, the number is kept without its title. **Unlink**
on the same card takes it off. The AI can read links but never set one.

## Notes and status from agents

An agent (or a script) in a worktree can say what it does there:

```sh
termocode worktree note "adding the pin row"
termocode worktree status blocked     # working, blocked or done
termocode worktree list               # every worktree with its status and last note
termocode worktree link 412           # asks you first
```

The worktree is the one the current folder is in. The AI chat and outside
agents have the same through the `worktrees_list` and `worktree_note`
tools. A note is one line of plain text, at most 500 characters, with
secrets hidden; the 50 newest are kept. The card shows the last note and
the status; `N` lists all notes with their times. A link from the command
line shows a card first (**Deny** comes first).

## Sleep and wake

`s` (**Worktree: Sleep or Wake Selected**) puts a worktree to sleep: the
agent tabs of this window that run in it end, and it moves under
**ASLEEP**. Its files are not touched. Agents in other termocode windows
keep running; the card says so.

- When agent tabs would end, a card names them first. **Keep running**
  comes first and is lit; Enter and Esc keep them running.
- A worktree with children asks about them on the same card:
  **Sleep only this one** or **Sleep with its child** (or its N
  children). Their agent tabs end too.
- A worktree with no agent tab and no child sleeps at once.
- `s` on an asleep worktree wakes it and its asleep children.

## Archive and restore

`x` (**Worktree: Archive or Restore Selected…**) archives a worktree:
`git worktree remove` takes its folder, its branch stays, and the row
stays under **ARCHIVED** with the branch name. `x` on the archived row
restores the folder from the branch (`git worktree add`).

- A card always asks first, and **Cancel** comes first.
- A worktree with changed files that are not committed says how many,
  and the button says it again: **Archive anyway (loses 2 changed
  files)**. Archive loses them.
- A parent asks about its children: **Archive with its child** when they
  have no changed files; a child with changed files is archived on its own.
- The main checkout, the worktree this window is in, agent lanes (their
  AI run keeps or discards them) and locked worktrees are never archived.
  termocode never forces a locked worktree; `git worktree unlock` it first.

## Delete

`D` (**Worktree: Delete Selected…**) removes the worktree's folder and
deletes its branch. On an archived row it deletes the branch only. It
asks in steps, and **Cancel** comes first at every step:

1. What goes: the folder, the branch, and whether the branch is merged
   into the default branch (`main`, `master`, or the remote's HEAD). A
   branch with commits that are not there says
   "not merged into main: 3 commits would be lost". A linked pull request
   that is merged counts as merged.
2. A folder with changed files gets a second card that names how many:
   **Delete anyway (loses 2 changed files)**. termocode never removes a
   folder with uncommitted changes without this card.
3. A branch that is not merged needs its name typed. Enter in the field
   does nothing; only the **Delete** button deletes, and only when the
   name is exact.

The default branch is never deleted here.

## Clean up

`C` (**Worktree: Clean Up…**) lists the worktrees that can go, each with
a box and the reason:

- ticked: the branch is merged into the default branch, its pull request
  is merged, or nothing happened in it for 14 days;
- never ticked: a worktree with changed files (the row says how many);
- one more row forgets git's records of folders that are gone
  (`git worktree prune`; no file is touched).

Space (or Enter on a row) ticks and unticks. Enter on the first row
opens one card that lists every worktree that will be archived
(**Cancel** first). A ticked worktree with changed files then asks on
its own card: **Keep it** comes first. Clean up archives, so the
branches stay and `x` restores any of them.

## From the web page

The web panel's desktop page has a **Worktrees** item in its activity bar
(and **View: Show Worktrees** in its command palette). It shows the same
list by group, with a filter of its own.

- **Pin**, **Note** (one line, secrets hidden) and **Wake** run at once.
- **Sleep** asks on the page first; when agent tabs would end, the laptop
  asks again with **Keep running** first.
- **Archive**, **Restore** and **Delete** wait for a card on the laptop.
  After yes, the laptop's own steps follow (changed files, the branch
  name).
- The page names a worktree by an id the laptop made; it never sends a
  folder path.
