---
title: termocode
hide:
  - navigation
---

# termocode

## Your whole coding workflow, right in the terminal.

termocode is an open-source development environment for terminal workflows. Edit code, explore a project, review changes, run tests, debug programs, and get help from an AI assistant—all in one place.

[Get started in five minutes :material-arrow-right:](getting-started/first-steps.md){ .md-button .md-button--primary }
[Install termocode](getting-started/install.md){ .md-button }
[View the source on GitHub](https://github.com/amin-jalali/termocode){ .md-button }

**Open source under MIT** · **Linux and macOS** · **Android with Termux**

![termocode in action: code editing, search, AI assistance, and the integrated terminal](assets/screenshots/demo.gif)

## The tools for your work, together in one workspace

Move through your project, make changes, and handle the work around your code without switching between separate windows.

<div class="grid cards" markdown>

-   :material-file-code-outline: **Edit and navigate**

    ---

    Keep files, tabs, breadcrumbs, quick open, and a searchable command palette close at hand. Add code completion and diagnostics with language servers.

    [Explore editing and navigation](guides/editing.md)

-   :material-source-branch: **Review and manage changes**

    ---

    Stage files or individual hunks, inspect diffs, commit, sync, and resolve merge conflicts from the source control view.

    [Explore source control](guides/git.md)

-   :material-play-circle-outline: **Run, test, and debug**

    ---

    Run project tasks, explore tests, and debug with breakpoints, variables, a call stack, and a debug console.

    [Debug a program](guides/debug.md) · [Explore tests](guides/tests.md)

-   :material-robot-outline: **Get AI help, on your terms**

    ---

    Ask questions, get code suggestions, or request an explanation, fix, or edit. Review proposed changes as a diff and choose what to apply. Connect a hosted or local model.

    [Set up the AI assistant](guides/ai-setup.md)

-   :material-lan-connect: **Keep working over SSH**

    ---

    Reconnect to the same session after a dropped connection. Your open tabs, unsaved text, terminal output, and running jobs are still there.

    [Work remotely](guides/remote-ssh.md)

-   :material-tune-variant: **Make it yours**

    ---

    Customize themes, key bindings, and settings. Add your own commands and panels with Lua extensions.

    [Customize termocode](guides/customize.md) · [Build an extension](extensions.md)

</div>

## Built for real projects

Search across your workspace, see errors in the Problems panel, and keep your terminal, tests, tasks, and debugging tools close to the code. Language servers are available for Go, Python, JavaScript and TypeScript, Rust, C and C++, Lua, and more, with an installer built in.

Press `F1` to find a command, or `Ctrl+P` to open a file. termocode runs locally or on a remote machine over SSH.

![Editor with the project explorer, tabs, and source control status](assets/screenshots/editor.png)

## Get started

On Linux or macOS, run the installer, then open a project folder with termocode. It installs to `~/.local/bin` without requiring `sudo`. See the install guide for other methods and Android instructions.

```sh
curl -fsSL https://raw.githubusercontent.com/amin-jalali/termocode/main/scripts/install.sh | sh
cd ~/code/my-project
termocode
```

Prerequisites: `nvim` 0.10 or newer and `git`. Fast search uses `ripgrep`; if it is not installed, built-in search is available.

[Read the install guide](getting-started/install.md){ .md-button .md-button--primary }
[Take the first five minutes](getting-started/first-steps.md){ .md-button }

## Open source, built in the open

termocode is released under the **MIT License**. Read the source, report an issue, suggest an improvement, or contribute on GitHub.

[Explore the repository](https://github.com/amin-jalali/termocode)
· [Contribution guide](https://github.com/amin-jalali/termocode/blob/main/CONTRIBUTING.md)

<small>termocode is in early 0.1.x releases. On Windows, use WSL.</small>
