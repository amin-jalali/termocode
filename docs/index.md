---
title: termocode — The AI-Integrated Terminal IDE
description: A fast, full-featured terminal IDE with integrated AI agents and persistent workspaces.
hide:
  - navigation
  - toc
---

<div class="tc-hero" markdown>

<p class="tc-eyebrow">Full-Featured · AI-Integrated · Persistent</p>

# The AI-Integrated Terminal IDE.

<p class="tc-lead">A full-featured IDE built for your terminal. Code, debug, test, and work with AI agents that create files, edit code and run tasks, with every change under your control.</p>

<p class="tc-tagline">Your Connection Dies. Your Work Lives On.</p>

<p class="tc-support">Your terminal sessions, processes and AI agents keep running after you disconnect.</p>

[Get Started](getting-started/install.md){ .md-button .md-button--primary }
[View on GitHub](https://github.com/amin-jalali/termocode){ .md-button }

</div>

![termocode in a terminal: Quick Open, the command palette, an AI answer, the AI change review and the integrated terminal](assets/screenshots/demo.gif){ .tc-shot }

<p class="tc-meta">Open source (MIT) · Linux and macOS · Android via Termux · over SSH</p>

## Three pillars

<div class="grid cards" markdown>

-   :material-application-outline:{ .lg } **A Full IDE. In Your Terminal.**

    ---

    Explorer, tabs, command palette, Git, debugger, test explorer, language
    servers, search, Docker services and an HTTP client. A real UI with
    mouse support, running entirely inside your terminal.

-   :material-robot-outline:{ .lg } **AI Agents. Built Into Your Workflow.**

    ---

    Agents create files, edit code and run commands inside the IDE. You
    review every change as a diff and approve it hunk by hunk.

-   :material-lan-disconnect:{ .lg } **Disconnect. Keep Working.**

    ---

    Lose your SSH connection and nothing stops: the editor, terminals,
    running jobs and AI agents keep going. Run `termocode` again and you
    are back where you were.

</div>

## AI That Actually Works in Your IDE.

The AI is not a chat window on the side. It works in the same workspace you do.

<div class="grid cards" markdown>

-   :material-file-edit-outline:{ .lg } **Real operations**

    ---

    Agents create and edit files, run commands and tasks, and read
    diagnostics, test results and service logs.

-   :material-check-decagram-outline:{ .lg } **You decide**

    ---

    Every proposed change opens as a diff. Accept or reject each hunk,
    then apply.

-   :material-wrench-outline:{ .lg } **Fix from the terminal**

    ---

    A failed command gets a **Fix** button that hands the error to the AI.

-   :material-server-outline:{ .lg } **Your models**

    ---

    A hosted provider, an OpenAI-compatible endpoint or a local model.

    [:octicons-arrow-right-24: Set up the AI](guides/ai-setup.md)

-   :material-connection:{ .lg } **MCP and services**

    ---

    Local and remote MCP servers. *Connect a service* signs in to GitHub,
    GitLab, Linear, Notion, Sentry, Slack, ClickUp and Atlassian.

    [:octicons-arrow-right-24: Connect services](guides/mcp.md)

</div>

![Reviewing an AI change hunk by hunk](assets/screenshots/diff-review.png){ .tc-shot }

## Your Workspace Survives Disconnects.

Over SSH, termocode runs your workspace in a background session. When the connection drops, the session keeps running.

1. Start a long task: a build, a test run, or an AI agent working on a change.
2. Lose the connection, close your laptop, or switch networks.
3. Run `termocode` again in the same folder.
4. Everything is back: open tabs, unsaved text, terminal output, and the work that kept running.

The session lives on the machine that runs termocode. It survives a dropped connection, not a reboot of that machine. It is on by default over SSH; turn on `persistent_session` to keep it when you close a local terminal.

[Work remotely](guides/remote-ssh.md){ .md-button }

## Everything You Need to Build.

<div class="grid cards" markdown>

-   :material-source-branch:{ .lg } **Git, Without Context Switching.**

    ---

    Stage hunks, commit, sync, stash, blame, a commit graph, a merge-conflict
    resolver and clone.

    [:octicons-arrow-right-24: Source control](guides/git.md)

-   :material-bug-outline:{ .lg } **Debug and Test Where You Code.**

    ---

    Breakpoints, watch and a Debug Console for Go, Python and Node. A test
    explorer for Go, pytest, Rust, Jest and Vitest.

    [:octicons-arrow-right-24: Debug](guides/debug.md) · [Tests](guides/tests.md)

-   :material-docker:{ .lg } **Manage Your Development Stack.**

    ---

    Start and stop Docker Compose services, follow their logs, open a shell
    in a container, and send HTTP requests from `.http` files.

    [:octicons-arrow-right-24: Services](guides/services.md) · [HTTP client](guides/http-client.md)

-   :material-magnify:{ .lg } **Find Anything.**

    ---

    Search and replace across the whole project, with regex and globs. It
    works even without ripgrep installed.

    [:octicons-arrow-right-24: Search](guides/search.md)

-   :material-code-braces:{ .lg } **Language-Aware.**

    ---

    Completion, diagnostics and go-to-definition through language servers for
    Go, Python, JS/TS, Rust, C/C++, Lua and more, with an installer built in.

    [:octicons-arrow-right-24: Language servers](guides/language-servers.md)

-   :material-format-list-checks:{ .lg } **Tasks and Problems.**

    ---

    Run build and test tasks in terminal tabs. Errors land in the Problems
    panel and link to the line.

    [:octicons-arrow-right-24: Tasks](guides/tasks-problems.md)

</div>

![The integrated terminal under the editor](assets/screenshots/terminal.png){ .tc-shot }

## Fast by Design.

A complete development environment does not have to be heavy.

<div class="tc-stats" markdown>

<div markdown><strong>&lt; 70 ms</strong><span>start to a usable window</span></div>
<div markdown><strong>~60 MB</strong><span>RAM, editor engine included</span></div>
<div markdown><strong>~0 %</strong><span>CPU when idle</span></div>
<div markdown><strong>~1 s</strong><span>to open a 100 MB file</span></div>

</div>

Measured on a Linux laptop (Intel i7-13700H). Language servers use their own memory on top. See the [benchmarks](benchmarks.md) for the method and all numbers.

## Work From Anywhere. Make It Yours.

<div class="grid cards" markdown>

-   :material-devices:{ .lg } **Platforms**

    ---

    Linux and macOS (amd64, arm64), Android via Termux, and any machine
    you reach over SSH, with file upload and download.

-   :material-console:{ .lg } **Any terminal**

    ---

    Rich colours and icons where your terminal has them, a clean fallback
    where it does not.

-   :material-palette-outline:{ .lg } **Customize**

    ---

    Themes and a theme editor, key bindings, snippets, settings export, and
    Lua extensions.

    [:octicons-arrow-right-24: Customize](guides/customize.md) · [Extensions](extensions.md)

-   :material-format-textdirection-r-to-l:{ .lg } **Right to left**

    ---

    Persian and Arabic text shows correctly in the AI chat and in Markdown.

</div>

## Get Started

One line, no `sudo`. It installs to `~/.local/bin` and needs `nvim` 0.10 or newer and `git`.

```sh
curl -fsSL https://raw.githubusercontent.com/amin-jalali/termocode/main/scripts/install.sh | sh
cd ~/code/my-project
termocode
```

Press `F1` for every command, or `Ctrl+P` to open a file.

<div class="tc-final" markdown>

**Your connection dies. Your work lives on.**

[Install termocode](getting-started/install.md){ .md-button .md-button--primary }
[Your first five minutes](getting-started/first-steps.md){ .md-button }

<small>termocode is an early release (0.1.x). No Windows build yet; use WSL.</small>

</div>
