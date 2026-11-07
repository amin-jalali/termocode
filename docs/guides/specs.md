# Specs

A spec takes a change from an idea to reviewed code in five phases:
**Requirements**, **Design**, **Tasks**, **Build** and **Review**. The AI
(or you) writes each phase; you approve it before the next one starts.

## Start a spec

**Spec: New…** asks for the goal (`Add a dark mode toggle`).
**Spec: New Bugfix…** asks for the bug (`Login times out on Safari`).
termocode makes a folder in the project:

```text
.termocode/specs/add-a-dark-mode-toggle/
  spec.json          kind, goal and approvals
  requirements.md    bugfix.md for a bug
  design.md
  tasks.md
  review.md
```

The files are plain Markdown: commit them with the code, edit them by
hand, or let the AI write them.

## The spec card

**Spec: Open…** lists the specs with the phase each one is in. Pick one
to see its card:

- every phase with its file and where it stands: not written, empty
  template, draft, approved (`✓`) or changed since approved;
- for Build, how many tasks are ticked and which tasks can start next.

The buttons:

| Button | What it does |
| --- | --- |
| **Close** | Comes first. Nothing changes. |
| **Open** | Opens the current phase's file in the editor. |
| **Ask AI** | Puts the current phase's prompt into the AI chat input. Read it, change it if you like, then press Enter to send it. Nothing is sent by itself. For Build and Review the button says **Build with AI** and **Review with AI**. |
| **Approve…** | Asks first (**Cancel** comes first), then approves the phase. |

## What each phase asks for

- **Requirements**: user stories and acceptance criteria, each as
  `WHEN <condition> THEN <expected result>`. No design or code yet.
- **Bugfix** (in place of Requirements): **Current behavior** (with the
  steps to see it), **Expected behavior**, and **Unchanged behavior**:
  what must keep working, so the fix does not break other things.
- **Design**: the parts to change, data and interfaces, error handling
  and the tests.
- **Tasks**: small tasks as tick boxes. A task that needs another one
  says so:

  ```markdown
  - [ ] 1. Add the setting
  - [ ] 2. Draw the toggle (after 1)
  - [ ] 3. Save it in the session (after 1)
  ```

  Tasks without open dependencies form the next wave (here 2 and 3,
  once 1 is ticked).
- **Build**: the AI takes the next open tasks, writes each test first,
  then the code, runs the test and ticks the task (`- [x]`).
- **Review**: the AI writes `review.md`: each acceptance criterion (for a
  bug: the expected and each unchanged behavior), whether the code and
  its tests meet it, and what was missed.

## Approvals

- Phases are approved in order: Design needs approved Requirements.
- An empty template or a missing file cannot be approved.
- An approval is bound to the file as it was. Change the file later and
  the card says **changed since approved**: approve it again before the
  next phase. Ticking tasks in Build does not undo the Tasks approval.
- Build can be approved only when every task is ticked.
- Only you approve. The AI cannot run the Spec commands; it writes the
  files with its normal file tools, which ask as usual.

Specs work in a local project folder for now; a remote or container
window says so.
