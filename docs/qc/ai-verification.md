# AI verification record

What is checked about termocode's AI features, how, and what is **not**
checked. This page is honest on purpose: "tested" here means an automated
test that runs on every change, not a manual try.

## How tests run

- Go tests with `-race` on GitHub CI (Linux on every push; macOS, coverage
  and performance jobs on `main`). Nightly and fuzz jobs run on a timer.
- AI providers, MCP servers, search providers, `gh`, CI and outside agents
  are **fakes** in tests: recorded or scripted answers, local HTTP servers
  on `127.0.0.1`, fake runners. Tests use temporary `HOME` and config
  folders, never the real ones.
- Each bug fix comes with a test that failed before the fix.

## What is verified, and how

| Area | Verified by | Not verified |
|---|---|---|
| Chat turns, tool calls, the confirm and review cards | Unit tests drive full turns with a scripted provider; every tool has a gate (auto / confirm / review) checked by a table test | Real model quality; how a real model reacts to the prompts |
| Request bodies per provider | Golden files of the exact JSON sent | That every provider's server still accepts them today |
| Safe-first cards (Deny / Cancel / Not now first) | Tests check the safe button is first and focused, and that Esc never does the risky thing | — |
| Secrets | Tests put fake tokens in files, tool output and errors and check they are blanked before the model, logs and audit | Secrets that look like nothing known |
| Project files (rules, skills, commands, agent files, hooks, MCP config) | Tests check the trust card comes first, a "no" keeps them out, and a changed file asks again | — |
| Hooks after tool calls | Tests run real `sh` hooks with a timeout, capped output and redaction | Windows `cmd /C` hooks (skipped there) |
| Web fetch / search | Local fake servers: text extraction, size caps, redirects, refusal of private addresses, the key only in the provider's header | Live providers; pages with heavy scripts (no browser is used) |
| Lesson recall | Tests with saved lessons: a match is added once per chat; check mode asks a fake quick model | Whether recalled lessons help a real model |
| Ship to merge-ready | The loop runs against fake git/gh steps: push card, PR card, red CI → fix turn → push → green → stop | A real push, a real pull request and real CI |
| Agent picker, start in a folder, one-click install (AGX-1…3) | Unit tests: no duplicate binaries, every preset has screen rules or is marked unknown; picker groups (INSTALLED / BUILT-IN / NOT INSTALLED) and Enter; a missing agent installs through the Not-now-first card into termocode's tools folder, then signs in; goldens of the picker; a perf test with 30 presets | A real install from npm or pip; a real sign-in |
| Agent race and compare (AGX-4…6) | App tests with fake lanes on real git repos in temp folders: the task typed once, only into the input box; stop; merge the winner, a conflict asks first, Keep the other lanes first; discard; verify exit codes; the summary with a scripted quick model (capped, redacted); `agent_race_start` always asks; goldens of the card and the compare page | Real agents racing; real model quality of the summary |
| Agent transcripts: chat view, sessions, search (AGX-7…10) | Fixture files of both formats in a temporary HOME: parse, truncated last line, unknown format, 16 MB cap, secrets hidden; nothing opened while `agent_transcripts` is off; chat view follows a growing file; sessions index, resume lines, search caps; goldens; perf tests (tail 10 MB, index, search) | New file formats of future CLI versions |
| Agent usage limits, auto-continue, plan pill (AGX-11, AGX-12) | Sample screens of the limit messages; app tests with a fake clock: one toast, one typed `continue` after the reset, never into a prompt or a working agent; the pill at three widths; goldens of AI: Usage PLANS | The real CLIs' limit text in future versions; e2e with a real binary |
| Agent accounts (AGX-13) | Tests with a temporary HOME and config folder: 0700 folders, `env CLAUDE_CONFIG_DIR=…` / `CODEX_HOME=…` lines, labels checked, nothing made under `~/.claude` or `~/.codex`, folders hidden in bug reports | A real sign-in into an account folder |
| Hibernate idle agents (AGX-15) | App tests with a fake clock and a fake stop: idle clock, Keep-running-first card, never a prompt or the tab on show, resume lines; a prompt to a hibernated tab is not typed into the shell | The real SIGTERM / SIGKILL of a live agent (process groups); Windows (not built) |
| Suggested names (AGX-16) | A scripted quick model: slug cleanup, cap, redaction, no request without a model; goldens | Real model quality |
| Agent control socket and CLI | Socket protocol tests on a real unix socket; app tests for list / wait / report / the prompt card; CLI tests with a fake socket call | Typing into a live agent; Windows (no socket there) |
| Worktrees: sleep, archive, delete, cleanup | Unit tests on real git repos in temp folders (dirty and merged checks, locked worktrees refused, restore from the branch); safe-first card tests (Cancel / Keep running / Keep it first, Esc and Enter never act, a dirty folder needs a second card with the changed files count, an unmerged branch needs its name typed); goldens of each card; e2e on the real binary (a fake agent tab ends on sleep, a dirty archive is cancelled, a merged one is deleted, cleanup archives the ticked one) | A real agent CLI; Windows |
| Worktrees from the web page | Topic and op tests (ids, never paths; pin, note, sleep; archive and delete only through a laptop card), a replay golden, and a browser test of the desktop view against the fake hub | A real phone browser |
| State hooks in other apps' config | Tests write and remove the hooks in temporary config files, keep backups and the user's own entries | That the other apps keep the same hook format |
| Screen-based agent state | Pattern tests on recorded screen text | New versions of the agents that change their screen |

## Known gaps

- No test calls a real AI provider, search provider or GitHub. Breaking
  changes on their side show up only when users report them.
- The text parts of the UI are checked by golden screenshots, not by
  people reading every screen.
- Model output is never graded automatically; "the AI did the right
  thing" is not something these tests can prove.
