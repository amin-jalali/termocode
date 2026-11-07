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
| `W` | Send PR feedback to its agent: review comments, `@termocode` mentions, failing CI (asks first) |
| `w` | Follow its PR: new feedback asks to go to the agent; again: stop |
| `P` | The linked pull request: checks and their logs, review threads, merge |
| `N` | The notes agents left on it, with their times |
| `d` | Diff vs base: what it changed since it left its base; keep or revert each change |
| `n` | New worktree (see below) |
| `c` | New child worktree: it starts from the selected one's branch; its PR targets that branch |
| `I` | Start from an issue: pick an open issue, the New worktree form fills in |
| `e` | Restack a child: replay its commits onto its parent's branch (asks first) |
| `H` | Hand off: a new agent goes on with a note of this work (see below) |
| `L` | Show the setup of a new worktree (its terminal tab) |
| `r` | Run the dev server on the worktree's own port (`$PORT`); again: show its tab |
| `o` | Open the running dev server's address in the browser |
| `A` | Adopt a worktree made outside termocode |
| `F` | Forget a worktree whose folder is gone |
| `s` | Sleep: end its agent tabs and move it under ASLEEP; again: wake it |
| `T` | Its agent sessions; `Enter` shows the session's tab |
| `O` | New agent session: one more agent in it, in its own tab |
| `v` | Preview: the agent's live screen in the panel (see below); again: close |
| `V` | Agent grid: up to 4 agent screens side by side |
| `x` | Archive: remove the folder, keep the branch; on an archived row: restore |
| `D` | Delete the worktree and its branch (asks step by step) |
| `C` | Clean up: tick merged and old worktrees, archive them together |
| `S` | Ship: commit (AI message), push, pull request (AI text), CI fixes, merge, archive; each step asks (see below) |
| `b` | Background task: an agent works on it alone in a sandbox container (asks first) |
| `B` | Background tasks: state, follow output, stop, remove |
| `M` | Move to another machine: pack it into one file (see below) |
| `U` | Unpack a moved lane as a new worktree, or pull one from a host |
| `f` | Filter: All, Mine, Agents, Dirty, Asleep, External |
| `/` | Filter by name (`Esc` clears) |
| `R` | Read every worktree again |
| `?` | Show these keys |

## Watch agents: preview and grid

`v` (**Worktree: Preview Agent Screen**) opens a **PREVIEW** tab in the
bottom panel. It shows the live screen of the agent that works in the
selected worktree, read only. The list keeps the keys: move with `j` and
`k` and the preview follows the cursor.

`V` (**Worktree: Preview Agent Grid**) shows an **AGENT GRID** instead:
up to 4 agent screens of this window side by side (2 by 2 when there are
3 or 4), the selected worktree's agent first.

| Key in the panel | What it does |
|---|---|
| `Tab` `←` `→` | Pick a tile |
| `Enter` or a click | Jump into that agent's terminal tab |
| `g` | Switch between one screen and the grid |
| `r` | Read the screens again now |
| `Esc` | Give the keys back |

- The screens are read twice a second while the tab is on show, never
  while it is hidden. Nothing is typed into the agents.
- An agent of another termocode window has no screen here; the preview
  says so, and **Ctrl+Shift+J** jumps to it.
- A hibernated agent tab shows as **asleep**; `Enter` wakes it.
- `v` or `V` again (the same way) closes the tab.

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
  "archive": ["docker compose down"], // run before a worktree's folder goes
  "branch_prefix": "amin/"            // starts a branch made from the task
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
- **archive** commands run in a worktree's folder just before Archive,
  Delete or Clean up removes it, for example to stop its services and
  free its ports. They share the trust of this file with **setup**. The
  archive card shows them; while the file is not trusted, **Archive**
  skips them and **Trust script and archive** runs them first. The delete
  and clean up cards say whether they run. Each command may take two
  minutes at most. A failing command never keeps the folder: the removal
  goes on and a warning names the command and its log file.

## A dev server per worktree

Parallel worktrees of a web app fight for the same port. Give the
repository one run line in `.termocode/worktrees.json`, and each
worktree gets a port of its own:

```jsonc
{
  "run": "pnpm dev --port $PORT",   // the dev server; reads $PORT
  "ports": "3000-3099",             // where ports come from (default 3001-3099)
  "url": "http://localhost:$PORT"   // what o opens (this is the default)
}
```

- `r` (**Worktree: Run Dev Server of Selected**) picks the lowest port of
  the range that no other worktree holds and nothing listens on. The
  worktree keeps it (in `worktrees.json` in the git folder), so it gets
  the same port next time.
- The line runs in a terminal tab inside the worktree, with `PORT` set
  and your environment minus termocode's secrets. It comes from the
  repository, so it needs the same trust as **setup**: a card shows the
  line and the port, **Not now** comes first, and a changed file asks
  again.
- The card shows `▶ localhost:3004 (o opens)` while it runs, and
  `port 3004 · r runs` when it does not. `o` (**Worktree: Open Dev Server
  of Selected**) opens the address; `r` again shows the tab. Close the
  tab to stop the server.
- When every port of the range is held or in use, nothing starts and a
  message says to stop a server or widen `ports`.

### Warm setup and env vars

Two more keys make a new worktree ready faster:

```jsonc
{
  "setup": ["npm ci"],
  "env": { "PORT": "3001" },                // added to the setup
  "warm": {
    "paths": ["node_modules"],              // what the setup makes
    "key": ["package-lock.json"]            // when it changes, set up again
  }
}
```

- **env** adds plain variables to the setup. Names must be shell names.
  A `${secret:NAME}` value is dropped: secrets never come from a file in
  the repository.
- **warm**: after a setup that worked, termocode keeps a copy of the
  **paths** in the git folder (`termocode/warm/`). The next new worktree
  whose **key** files have the same bytes gets that copy and skips the
  setup; a toast says so. A changed key file, setup line or config file
  runs the setup again. The newest 3 copies are kept, each at most 4 GB.
- A path the new worktree has already is kept as it is. Every path must
  stay inside the worktree.
- The trust card names the env variables and the warm paths. A warm copy
  is only used after you trust the file.
- Use warm paths that still work when moved, like `node_modules`. A
  Python `.venv` holds its own folder path, so leave it out.

## Child worktrees

`c` (**Worktree: New Child of Selected…**) opens the New worktree form
for a child of the selected worktree: it starts from that worktree's
branch, and the list draws it under its parent (`└`; deeper children
show as `└ …`). A child whose parent is gone moves up to the top level.

### Stacked pull requests

A child is stacked on its parent. termocode keeps the parent's branch
as the child's base, so **Git: Create Pull Request…** and the ship loop,
run inside the child, target the parent's branch instead of the default
branch. The card shows it: `feat/child → feat/parent (stacked: the
branch of the parent worktree)`. When the parent is archived, its kept
branch is still the target; when the parent is gone, the branch kept at
creation is.

When the parent moves on (new commits, or it was rebased), `e`
(**Worktree: Restack Selected onto Parent…**) replays the child's
commits onto the parent's branch with `git rebase`. A card names both
branches first; **Cancel** is first and focused, `Esc` cancels.
Uncommitted changes stop it before anything runs. A conflict leaves the
rebase open in the child: open it to continue or abort. A child that was
pushed before needs a force-with-lease push afterwards. The AI may ask
for a restack only through the same card.

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

## Hand off to a new agent

`H` (**Worktree: Hand Off Selected…**) passes the selected worktree's
work to a new agent, for example when the old one runs out of context
or you want another agent to finish it. termocode writes a short note
from what git already knows; no AI request runs:

- the branch and the branch it started from (the parent's branch for a
  child, else the main checkout's branch),
- the commits since then and the changed files,
- the files not committed yet,
- the notes and the status agents left on the worktree, and its linked
  issue or pull request,
- one line you add on the card.

The card shows a preview. **Cancel** comes first. Pick **Where**:

- **this worktree**: the agent starts in the same folder. Its first task
  is one line that names the note. The worktree gets the note
  "Handed off to …". The old agent keeps running; `s` puts it to sleep.
  The same agent kind cannot start twice in one worktree.
- **new child worktree**: the New worktree form opens for a child, with
  the agent picked and the task typed in. Type a name, then
  **Create + Start**.

The note is saved in `termocode/handoff/` inside the repository's git
folder, never in your project, with secrets hidden. The AI never starts
a handoff. The AI chat has its own **AI: Handoff to a New Chat**.

## Link an issue or pull request

`i` (**Worktree: Link Issue or PR…**) links the selected worktree to an
issue or a pull request: type its number, `#412`, or its GitHub URL. A
URL must be `https` on the repository's own GitHub host. termocode asks
`gh` with your own sign-in, so the card shows `#412 open` or
`PR #12 open · CI ✓`. The state is read again while the view shows: every
20 seconds while checks run, else every 5 minutes. Without `gh`, or when
it is not signed in, the number is kept without its title. **Unlink**
on the same card takes it off. The AI can read links but never set one.

### The pull request pane

`P` (**Worktree: Show Pull Request of Selected**) opens the linked pull
request on one card: title, state, checks, changed files, description,
reviews and review threads. Without a linked pull request a short note
says to press `i` first.

- **Checks…** lists every check. A failed GitHub Actions check has two
  rows: **Show Log** opens the failed part of its log in a read-only tab,
  and **Fix with AI** puts that log in the chat. Nothing is sent until
  you press Enter. Any other check with a link opens its page.
- **Threads…** lists the review threads, open ones first. A thread's
  card shows the code and every comment, with **Reply…** and
  **Resolve** or **Reopen**.
- **Actions…** reviews, checks out or merges the pull request. Merge
  asks first on a card where **Cancel** is ready.

## Send PR feedback to the agent

`W` (**Worktree: Send PR Feedback to Agent…**) reads the worktree's pull
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
does not ask about them again; `W` still sends everything open at any
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

To hand a task to a new agent in a new worktree from a script, use
`termocode agent start "<task>"`: termocode shows this **New worktree**
card filled in, and nothing is made until you press **Create + Start**
(see [Start a background task](agents.md#start-a-background-task)).

## Review changes vs base

`d` on a card (or **Worktree: Review Changes vs Base of Selected**)
shows everything the worktree changed since it left its base: its
commits, edits not committed yet and new files. It does not matter who
made them: the AI chat, an agent in a terminal tab, or you. The base is
the parent's branch for a child worktree, else the main branch
(`origin`'s default, `main` or `master`). Changes made on main after the
worktree left it are not shown.

The files open in the same review card as AI edits, one hunk at a time:

- **Accept** keeps the change as it is.
- **Reject** writes the base text back for that hunk only. On a new
  file, rejecting every hunk removes the file.

Nothing is written until you finish a file. When the agent changed the
file again while you looked, or the file has unsaved edits in the
editor, nothing is written and a note says why; press `d` again to read
it anew. Binary files, files over 1 MB and links are left out (the count
is shown). At most 200 files are listed.

## What waits for what

When several agents split one job, one worktree's work often needs
another's first. An agent (or you) records it from the worktree that
waits:

```sh
termocode worktree block feat/api     # this worktree waits for feat/api
termocode worktree unblock feat/api   # it no longer does
```

The other worktree is a path, a branch or a name. The AI chat and outside
agents use the `worktree_block` tool; programs use the control op
`worktree-block`. A loop (A waits for B, B waits for A) is refused.

A blocker is settled when its status is `done`, its pull request is
merged, its issue is closed, or the worktree is gone or archived. The
card's last line says **waits for feat/api** while it is not, and
**ready: its blockers are done** after. `termocode worktree list` has a
WAITS FOR column.

**Worktree: Show Dependency Graph** (`F1`) draws the graph in waves:
wave 1 can start now, wave 2 starts when wave 1 is done, and so on. Each
row says **done**, **ready** or **waits**, and what it waits for.

## Several agent sessions

One worktree can have more than one agent at work: separate sessions on
the same files, each in its own terminal tab.

- `O` (**Worktree: New Agent Session in Selected…**) lists the installed
  agents. The one you pick starts in the worktree in a new tab, even
  when the same agent already runs there. **Agent: Start…** shows the
  running tab instead.
- A second session of the same agent gets a number in the tab bar:
  **Claude Code 2**.
- `T` (**Worktree: Agent Sessions of Selected…**) lists the sessions of
  the worktree: the ones waiting for you first, then the working ones.
  `Enter` shows the tab; the last row starts one more session.
- The card counts every session. Close a session like any terminal tab.

## A task list shared with agents

The repository has one task list that you and every agent share. Each
task is a plain Markdown file in `.termocode/tasks/` of the main
checkout (like `0007-fix-the-login.md`), so all worktrees see the same
tasks and you can edit them in any editor or commit them.

**Tracker: Show Tasks…** lists them: doing first, then blocked, todo and
done, with the owner. Pick one to open its file, set another status, or
copy `termocode task claim 7` for an agent. **Tracker: Add Task…** adds a
todo.

An agent (or a script) uses the same files from its terminal; no running
termocode is needed:

```sh
termocode task list                  # open tasks (--all: done too, --json)
termocode task add Fix the login     # prints the new id
termocode task claim 7               # doing, owned by you, in this worktree
termocode task note 7 "found it in auth.go"
termocode task status 7 done         # todo, doing, blocked or done
termocode task show 7
```

A task someone else is doing can not be claimed. Two agents that add at
the same time never get the same id. The owner is `--as <name>`, else
`$TERMOCODE_TASK_OWNER`, else the termocode tab (`agent t3`), else your
user name. Task text is shown as plain text and never runs.

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

## Ship

`S` (**Worktree: Ship Selected (commit, push, PR, merge, archive)…**)
takes a worktree from finished work to merged in one flow:

1. **Commit.** A worktree with changed files shows a card with a commit
   message the AI wrote from the diff. **Cancel** comes first;
   **Commit and ship** commits all its changes in one commit; **Edit
   message…** lets you change the text first. Without an AI the message
   is "Work on <branch>" and the card says so. A clean worktree skips
   this step.
2. **Push.** A plain push of the branch, never forced (**Not now**
   first).
3. **Pull request.** The AI writes the title and the description from
   the commits and the changed files; the card shows them (**Cancel**
   first, then **Create** or **Create as Draft**).
4. **CI.** termocode waits for CI. When it is red, the failing log goes
   to the AI chat as a fix turn (at most 3), and the fix is pushed after
   its card, like **AI: Ship Branch to Merge-Ready**.
5. **Merge.** On green CI a card asks to merge (`gh pr merge`, never
   `--admin`): **Cancel** first, then **Merge**, **Squash and merge** or
   **Rebase and merge**. Cancel stops at merge-ready.
6. **Archive.** After the merge the archive card of the worktree opens
   (see below), **Cancel** first. The branch stays.

Cancel or Esc on any card stops the flow there. Agent lanes, archived
rows and worktrees without a branch are not shipped. Running **AI: Ship
Branch to Merge-Ready** again stops a running ship. The AI never starts
it.

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

## Background tasks

`b` (**Worktree: Run Agent Task in Background…**) gives the selected
worktree to an agent that works on its own. You type the task; a card
shows everything before anything starts (**Cancel** first):

- the agent runs in its headless mode (Claude Code, Codex, Gemini CLI,
  Qwen Code, OpenCode or Aider): the worktree's own agent when it has
  one, else the first one you have installed;
- it runs in a throwaway docker or podman container (`node:22-bookworm`;
  npm agents start through `npx`, so the image needs no install);
- the container sees only the worktree and the repository's git folder,
  at their own paths. Its edits land in the worktree. It runs as your
  user, with every capability dropped;
- the agent's own approval prompts are off, because nobody is there to
  answer them: the container is the sandbox. The network stays on, for
  the agent's API;
- API key variables (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
  `GEMINI_API_KEY` and a few more) are passed by name only; the value
  never is part of a command line or the card.

**Start in background** starts the container detached. It keeps working
when termocode or the session ends. When termocode runs on your own
server (over SSH, with the persistent session), the laptop can be off.
A container engine on another machine (a remote context) is refused:
its folders are not this host's.

`B` (**Worktree: Background Tasks…**) lists every task with its state
(working, done, failed with its exit code), agent and worktree. Enter on
one opens its card: **Follow output** opens a terminal tab with its log,
**Stop the task** ends a running one, **Remove container** clears an
ended one. The worktree and its changes always stay; review them as
usual (diff, commit, pull request).
## Move a lane to another machine

Start a lane on your laptop and let it go on on a server, or bring a
server's lane to your laptop to test it. The repository must be on both
machines; termocode too.

`M` (**Worktree: Move Selected to Another Machine…**) packs the worktree
into one `.tlane` file in `~/.local/share/termocode/lanes/`:

- the commits the remotes do not have yet (a git bundle),
- the changes not committed (staged and not staged),
- the new files git does not track yet (ignored files stay; files over
  10 MB are left out and listed),
- its notes, its name and its agent,
- its Claude Code chats, while **agent transcripts** is allowed in
  Settings, so `claude --resume` goes on there.

A card asks first; **Cancel** comes first. **Pack and download** also
sends the file to your own computer (over ssh: through the terminal).
Nothing in the worktree changes. Once the lane runs on the other
machine, archive it here (`x`). If an agent still runs in it, `s` puts
it to sleep first.

`U` (**Worktree: Unpack a Moved Lane…**) lists the packs in the lanes
folder and in `~/Downloads`. Its card shows where the lane comes from,
the new folder and branch, and what comes with it. Unpack always makes
a **new** worktree next to the others: a free branch name (`-moved` is
added when the name is taken), a new folder, and it never writes over a
file. When the changes do not apply, the worktree stays on its commit
and the changes are kept next to it as `<folder>.lane.patch`. When the
repository here lacks the commits the lane is built on, `git fetch`
first.

**Pull a lane from a host…** (the first row of `U`, or **Worktree: Pull
a Lane from a Host…**) asks for `host:folder`, then a card, then packs
the lane there over your own ssh (keys, agent, `~/.ssh/config`; no
password is asked) and opens the unpack card.

The same from a shell:

```sh
termocode lane pack [--out FILE|-] [--chats] [DIR]   # pack the worktree DIR is in
termocode lane info FILE                             # what a pack holds
termocode lane unpack [--repo DIR] [--name BRANCH] FILE|-
termocode lane pull [--chats] host:~/src/app.worktrees/fix-login
termocode lane push [--chats] [DIR] host:~/src/app   # pack here, unpack there
```

The AI never moves, unpacks or pulls a lane.
### Auto clean-up

Two settings (Settings, **Git** group) archive worktrees by themselves.
Both are off (`0`) until you set them:

- `worktree_auto_archive_days`: a worktree with no activity for this
  many days is archived.
- `worktree_max`: when more worktrees than this are live (the main
  checkout does not count), the oldest ones are archived until it fits.

termocode checks when the worktree list is read (the WORKTREES view,
Jump, the web page), at most once an hour. It archives like `x`: the
folder goes and its disk is freed (git-ignored files such as build
output go too), the branch stays. One toast names each worktree, why it
went and how much disk was freed. `x` on its row under **ARCHIVED**
brings it back in one step.

It only takes clean worktrees termocode made or you adopted. It never
takes the main checkout, the worktree this window is in, an agent lane,
a locked or pinned worktree, one an agent works in, one with changed
files, or one used in the last day. A file that changes just before the
archive stops it: termocode never forces it. The AI can not change these
settings.

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
- A worktree's card data carries its dev server port, and the address
  while it runs. Starting it stays on the laptop (`r`).

### Start a new task from the phone or the page

**New task** (the **+** at the top of the page's Worktrees list, or
**More → New task** on a phone) starts a background task on the laptop
while you are away from it:

1. Type the **Task** (one line). The agent gets it typed in once it is
   ready.
2. Pick the **Agent**. Only agents installed on the laptop are listed.
3. **Name** is optional. With no name the branch comes from the task's
   first words, under `task/` (for example `task/fix-the-login-bug`).
4. **From** is the start point; it is the laptop's current branch until
   you pick another one.
5. **Start task** asks on the page first, with **Cancel** first.

The laptop then does what **Create + Start** does in **Worktree: New…**:
it makes the worktree and its branch, copies and links the files of
`.termocode/worktrees.json`, and starts the agent there. A note on the
new worktree says which device asked and the task. The task only adds a
new branch and folder, so no card waits on the laptop. The repository's
setup commands run only when the laptop trusts them already; if not,
the laptop asks first, and the page says so.

Follow the agent in the Worktrees list and in its terminal tab. When it
is done, open a pull request from the branch as usual.
