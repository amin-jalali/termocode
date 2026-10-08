---
title: termocode
hide:
  - navigation
---

# termocode

**A terminal IDE that feels like VSCode — with a real Neovim inside.**

<!--
Hero GIF. It is not committed yet. Make it with:
  go build -o termocode ./cmd/termocode
  vhs assets/screenshots/demo.tape
The docs workflow copies assets/screenshots/ into the site, so after you
commit demo.gif, remove this comment wrapper around the next line:
![termocode demo](assets/screenshots/demo.gif)
-->

## Why termocode

- **Real Neovim, not a copy.** The editor is a real `nvim --embed` process.
  Your motions, registers, undo tree and Lua config all work.
- **IDE chrome around it.** You get tabs, a file explorer, a command palette,
  a git panel, a terminal, a debugger view and a test explorer.
- **Runs anywhere.** It is one binary in your terminal. It works over SSH,
  in tmux and on a fresh server.

## Highlights

<div class="grid cards" markdown>

-   :material-keyboard-outline: **Easy to learn**

    ---

    Press `F1` for the command palette. Every action is there, with its key.

    [:octicons-arrow-right-24: First 5 minutes](getting-started/first-steps.md)

-   :material-robot-outline: **AI assistant**

    ---

    Ghost-text completion, a chat panel, an agent with tools, and
    review-then-apply edits. Works with Anthropic, OpenAI, OpenRouter
    or a local Ollama.

    [:octicons-arrow-right-24: Set up AI](guides/ai-setup.md)

-   :material-bug-outline: **Run and debug**

    ---

    Breakpoints, step, call stack, variables, watch and a Debug Console.
    Uses `launch.json`, like VSCode.

    [:octicons-arrow-right-24: Debug a program](guides/debug.md)

-   :material-test-tube: **Test explorer**

    ---

    See every test in a tree. Run one, a file, or all. Failures link to
    the line.

    [:octicons-arrow-right-24: Run tests](guides/tests.md)

-   :material-source-branch: **Git built in**

    ---

    Stage hunks, commit, sync, resolve merge conflicts and clone — without
    leaving the editor.

    [:octicons-arrow-right-24: Git guide](guides/git.md)

-   :material-magnify: **Fast search**

    ---

    Find and replace across every folder with ripgrep. Case, word and
    regex toggles, plus include / exclude globs.

    [:octicons-arrow-right-24: Search guide](guides/search.md)

-   :material-format-list-checks: **Tasks and Problems**

    ---

    Run build and test tasks in a terminal tab. Compiler errors land in
    the Problems panel.

    [:octicons-arrow-right-24: Tasks guide](guides/tasks-problems.md)

-   :material-puzzle-outline: **Lua extensions**

    ---

    Add commands, sidebar panels and status items with a few lines of Lua.

    [:octicons-arrow-right-24: Write an extension](extensions.md)

</div>

## Install

termocode is built from source for now. You need Go 1.26 or newer and
`nvim` 0.10 or newer.

```sh
git clone https://github.com/amin-jalali/termocode.git && cd termocode && go install ./cmd/termocode
```

Then run `termocode` in any project folder. See [Install](getting-started/install.md)
for the details, and for Homebrew / AUR (coming soon).

[Start here: your first 5 minutes :material-arrow-right:](getting-started/first-steps.md){ .md-button .md-button--primary }
