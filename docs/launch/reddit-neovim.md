# Reddit drafts

Post one subreddit per day, from an account with history. Read each
sidebar's rules on the day; they change. Use the right flair.

## r/neovim (image or video post: `assets/gifs/hero.gif`)

**Title:** `termocode: an IDE shell around a real nvim --embed (Go, open source, v0.1)`

**Body:**

termocode is not a plugin and not a Neovim fork. It starts `nvim --embed`
and builds the rest of an IDE around it in your terminal: file explorer,
tabs, command palette, git with hunk staging, debugger, test explorer, an
integrated terminal, and an AI chat whose edits you review per hunk.

Your `init.lua`, keymaps, registers and macros keep working. The point is
to get the IDE parts without maintaining dozens of plugins for them.

It also keeps your work alive: over SSH the editor, terminals and running
jobs stay on the server when the connection drops, and `termocode`
brings them back.

Early release: Linux, macOS, Termux. No Windows. One Go binary, about 60 MB
of RAM with Neovim (docs/benchmarks.md has the method).

I am looking for people with big `init.lua` setups to try it and tell me
what breaks. Install:

    curl -fsSL https://raw.githubusercontent.com/amin-jalali/termocode/main/scripts/install.sh | sh

Repo and docs: <repo link>

## r/commandline (day +1)

**Title:** `A full IDE in the terminal in one Go binary: explorer, git, debugger, AI chat, sessions that survive SSH drops`

**Body:** the GIF, three lines on what it is, the install line, "feedback
welcome, especially over slow links".

## r/golang (Show and tell flair)

**Title:** `termocode: a terminal IDE in Go (Bubble Tea + embedded Neovim)`

**Body:** lead with the engineering: a patched Bubble Tea renderer that
brought idle CPU close to zero (docs/adr/0010-patched-bubbletea-renderer.md),
the `nvim --embed` RPC client, allocation guards and perf budgets that run in
CI, a session server so the UI outlives the terminal. Link the architecture
page.

## r/LocalLLaMA (only the local-AI angle)

**Title:** `A terminal IDE whose AI agent runs against Ollama or LM Studio: every edit is reviewed per hunk, works offline`

**Body:** which local servers work, how the confirm cards work (safe answer
first), per-hunk review, undo for a whole turn. Show the GIF with a local
model name visible. Say which models you tested and how it behaved; do not
claim more than that.
