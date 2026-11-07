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
- Put the cards of a group in your own order with `K` and `J`
  (**Worktree: Move Selected Up** / **Down**). The order is kept after a
  restart. New worktrees come after the ones you placed. A pin or unpin
  drops a card's place; **Worktree: Reset Order** drops every place.
- In a short window each worktree is one line; Enter shows its card.
- The list is read again every 10 seconds while it is on screen. Git
  runs in the background, at most 4 calls at once.

Pins, names and your own order are stored in `termocode/worktrees.json` inside the
repository's git folder, never in your project.

| Key | What it does |
|---|---|
| `↑` `↓` `j` `k` | Move (`g` `G` top, end) |
| `Enter` | Open the worktree in this window; the old session keeps running |
| `a` | Add the worktree as a folder of this window |
| `p` | Pin or unpin |
| `K` `J` | Move the worktree up or down in its group (`Shift+↑` `Shift+↓` too) |
| `i` | Link an issue or pull request (see below) |
| `r` | Send PR feedback to its agent: review comments, `@termocode` mentions, failing CI (asks first) |
| `w` | Follow its PR: new feedback asks to go to the agent; again: stop |
| `N` | The notes agents left on it, with their times |
| `n` | New worktree (see below) |
| `c` | New child worktree: it starts from the selected one's branch |
| `I` | Start from an issue: pick an open issue, the New worktree form fills in |
| `L` | Show the setup of a new worktree (its terminal tab) |
| `A` | Adopt a worktree made outside termocode |
| `F` | Forget a worktree whose folder is gone |
| `s` | Sleep: end its agent tabs and move it under ASLEEP; again: wake it |
| `T` | Its agent sessions; `Enter` shows the session's tab |
| `S` | New agent session: one more agent in it, in its own tab |
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
Type only a **Task** and the name and the branch come from its first
line: `Please fix the login timeout on Safari` gives the name `Fix the
login timeout on Safari` and the branch `fix/login-timeout-safari`.
Polite words and small words are dropped, at most five words stay, and a
first verb such as fix, add, refactor, document or test gives the
`fix/`, `feat/`, `refactor/`, `docs/` or `test/` prefix. The card shows
both before anything is made; a typed name, branch or issue always wins.
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
  "setup": ["pnpm install"],         // run in the new worktree
  "branch_prefix": "amin/"           // starts a branch made from the task
}
```

- **branch_prefix** is used for a branch made from the task, in place
  of the `fix/` or `feat/` prefix (`amin/fix-login-timeout`).

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

## Start from an issue

`I` (**Worktree: Start from Issue…**) lists the open GitHub issues of the
repository (read by `gh` with your own sign-in). Pick one, and the New
worktree form opens filled in:

- **Issue** is `#12`, so the worktree is linked to it;
- **Branch** comes from the title, like `12-login-crashes-on-empty-password`;
- **Agent** is the first installed agent;
- **Task** is `Work on issue #12: <title>. Read it first with gh issue view 12, …`.

Change any field first. Nothing is made until **Create** or
**Create + Start** (**Cancel** is the first button). The title is shown
as plain text: control bytes are dropped and it is cut at 200 letters.
**Git: Issues…** has the same row (**Start Work: New Worktree + Agent…**)
for one issue. The AI can not start it; it is your choice.

## Link an issue or pull request

`i` (**Worktree: Link Issue or PR…**) links the selected worktree to an
issue or a pull request: type its number, `#412`, or its GitHub URL. A
URL must be `https` on the repository's own GitHub host. termocode asks
`gh` with your own sign-in, so the card shows `#412 open` or
`PR #12 open · CI ✓`. The state is read again while the view shows: every
20 seconds while checks run, else every 5 minutes. Without `gh`, or when
it is not signed in, the number is kept without its title. **Unlink**
on the same card takes it off. The AI can read links but never set one.

## Send PR feedback to the agent

`r` (**Worktree: Send PR Feedback to Agent…**) reads the worktree's pull
request with `gh`: the open review threads, comments in the PR
conversation that call the agent (`@termocode` or `@agent`), and a
failed CI run with its failing log. A card shows a short preview;
**Cancel** is first. **Send** writes all of it to a file in the
repository's git folder (secrets are hidden, the file is never part of
your project) and types one line into the agent tab of that worktree:
read the file, fix it, commit, do not push. You push when you are happy.
When no agent works in the worktree, the feedback goes into the AI chat
input instead, and you press Enter to send it.

`w` (**Worktree: Follow PR Feedback**) follows a linked pull request
(link it with `i` first; the card then says `· following`). While the
view shows, termocode reads the PR every 2 minutes. When there are new
review comments, new mentions or a new failing run, the same card asks
before anything is sent. **Skip** (or `Esc`) marks them as seen, so it
does not ask about them again; `r` still sends everything open at any
time. `w` again stops following. The AI can never send feedback or turn
following on.

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

## Several agent sessions

One worktree can have more than one agent at work: separate sessions on
the same files, each in its own terminal tab.

- `S` (**Worktree: New Agent Session in Selected…**) lists the installed
  agents. The one you pick starts in the worktree in a new tab, even
  when the same agent already runs there. **Agent: Start…** shows the
  running tab instead.
- A second session of the same agent gets a number in the tab bar:
  **Claude Code 2**.
- `T` (**Worktree: Agent Sessions of Selected…**) lists the sessions of
  the worktree: the ones waiting for you first, then the working ones.
  `Enter` shows the tab; the last row starts one more session.
- The card counts every session. Close a session like any terminal tab.

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
