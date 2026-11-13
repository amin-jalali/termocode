# Show HN draft

## Title options (pick one, under 80 characters)

1. `Show HN: termocode – a terminal IDE with real Neovim inside, AI included`
2. `Show HN: termocode – a terminal IDE whose sessions survive SSH drops`
3. `Show HN: termocode – a terminal IDE where AI edits are reviewed per hunk`

Link: the GitHub repository.

## First comment (post it right after submitting)

Hi HN, I built termocode, an IDE that runs entirely in a terminal.

The editor is a real `nvim --embed`, so your motions, registers and
`init.lua` work as they do in Neovim. Around it is the layout most people
know from a desktop IDE: explorer, tabs, command palette, git (hunk
staging, graph, conflict resolver), debugger, test explorer, an integrated
terminal, and views for Docker, Kubernetes and databases.

Two things I care about most:

- **Persistent.** Your connection dies, your work lives on. Over SSH the
  editor, terminals and running jobs keep going on the server; run
  `termocode` again and you are back. `termocode ssh host` also reconnects
  by itself after a Wi-Fi switch or sleep.
- **AI that you control.** The agent can open files, run tests, stage hunks
  and set breakpoints, but every file edit is a diff you accept or reject
  per hunk, and one command undoes a whole AI turn. It works with hosted
  providers or a local model (Ollama, LM Studio), so it also works
  offline.

It is one Go binary, about 60 MB of RAM with Neovim, near-zero CPU when
idle (method and raw numbers: docs/benchmarks.md).

What is rough: it is an early release. Linux, macOS and Termux only, no
Windows build. The browser "second screen" is experimental. On the first
start it downloads three Neovim plugins in the background.

Install (no sudo, goes to `~/.local/bin`):

    curl -fsSL https://raw.githubusercontent.com/amin-jalali/termocode/main/scripts/install.sh | sh

I would love feedback on two things: does your own Neovim config work
inside it, and what breaks over a bad SSH link? I am here for the next few
hours.

## Answers to likely questions

**Why not a Neovim plugin?** The shell around the editor (panels, git
views, debugger UI, AI cards, sessions) needs its own screen and process
model. Neovim is embedded over RPC and stays the real editor.

**What happens to my AI data?** Requests go only to the provider you pick.
With a local model nothing leaves the machine. API keys are kept out of
settings files.

**Why Go?** One static binary, easy install, cheap goroutines for the many
background jobs.

**Windows?** Not now. Use WSL.

**How do you measure memory?** See docs/benchmarks.md: machine, versions,
commands and medians of three runs.

**Is it free?** Yes. MIT license.
