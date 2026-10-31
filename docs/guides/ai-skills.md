# AI skills

A **skill** is a short knowledge file for the AI: how to run the tests in
this repo, how to cut a release, how a tricky part of the code works. The
AI does not read every skill on every message. It sees only a list of
names and one-line descriptions, and it loads a skill's full text only
when a task needs it. So you can keep many skills and still send small
prompts.

The chat itself is explained in [Use AI](ai-use.md).

## Built-in skills

Five skills are ready on first start: `git-commit`, `review-work`,
`remove-ai-slop`, `init-agents-md`, and `debugging`. Open **AI: Skills…**
to read them. Their bodies are loaded only when needed, just like your
own skills. They do not need project trust.

A user or project skill with the same name replaces a built-in skill.
User skills have first priority; project replacements still need trust.

## Write a skill

A skill is a markdown file with a small header (front matter):

```markdown
---
name: go-tests
description: How to run and fix the Go tests in this repo
---

Run `go test -race ./...`. Golden screens live in
internal/devtools/shotkit/testdata; update them with UPDATE_GOLDEN=1
only after you looked at the new frame.
```

- `name`: letters, digits, `.`, `_` and `-`, at most 64 characters. When
  it is missing, the file name (without `.md`) is the name.
- `description`: one line. The AI picks a skill by this line, so say when
  the skill helps. Long lines are cut to 160 characters.
- The text after the header is the skill. At most 32 KB per file.

The quickest way: run **AI: Skills…** and pick **New project skill…** or
**New user skill…**. termocode writes a template (`new-skill.md`,
`new-skill-2.md`, …) and opens it in the editor.

## Where skills live

| Kind | Folder | Who trusts it |
|---|---|---|
| Project skills | `.termocode/skills/*.md` in the workspace | You, once per content (see below) |
| User skills | `~/.config/termocode/skills/*.md` (`$XDG_CONFIG_HOME/termocode/skills`) | Always used |

Project skills can be committed, so the whole team shares them. User
skills are yours and work in every workspace. When both have a skill with
the same name, your user skill wins.

The files are read again at the start of every message and on every load.
There is no cache: an edit counts on the next message.

## Trust for project skills

Project skills come from the repository, so they can steer the AI. They
work like the [project rules file](ai-use.md#project-rules-file):

- The first message in a workspace with project skills opens a card **Use
  this project's skills?**. It lists the skills with their descriptions.
- **Not in this project** (the first button, and the one Enter picks)
  sends your message; the project skills are not offered.
- **Always in this project** sends it and offers them.
- **Esc** sends nothing and puts your message back in the input.

The answer is kept per workspace for that exact content. When a project
skill is added, removed or changed, the next message asks again. To
change your answer, run **AI: Skills…** and pick **Project skills:
trust…**.

## How the AI uses skills

When at least one skill is offered, the system prompt gets a short list:

```text
Skills you can load (…):
- go-tests — How to run and fix the Go tests in this repo
- release — Steps to cut a release and update the changelog
```

The AI then calls the `skill_load` tool with a name to read that skill.
This tool only reads; it never asks you. In the chat, a load shows as a
small line:

```text
📘 loaded skill: go-tests
```

When the AI asks for a name that does not exist, the tool tells it the
names it can use. Without any skill, there is no list and no tool, so
the prompt does not grow.

Pipeline runs ([AI profiles](ai-profiles.md)) get the same list and tool
in every step.

## AI: Skills…

**AI: Skills…** lists your skills by source (project, user) with their
descriptions.

- **Enter** on a skill opens its file in the editor.
- **New project skill…** / **New user skill…** create a template.
- **Project skills: trust…** opens the trust card again.
- Files that could not be used are shown at the top as `skipped …` with
  the reason, for example `no description in the front matter` or
  `over 32 KB`. A bad file is skipped; it never stops the chat.

The AI can not run this command.

## Safety

- Known secrets and key-looking strings are replaced by `[REDACTED]`, in
  the descriptions in the prompt and in the text `skill_load` returns.
- Terminal escape codes, control characters and bidi marks are removed.
- A project skill that is a symlink to a place outside the workspace is
  skipped.
- Skill text is reference material. It gives the AI no new tools and never
  answers a confirm card for you.
- At most 64 skills are listed.
