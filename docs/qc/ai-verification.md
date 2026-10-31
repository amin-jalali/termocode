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
| Agent control socket and CLI | Socket protocol tests on a real unix socket; app tests for list / wait / report / the prompt card; CLI tests with a fake socket call | Typing into a live agent; Windows (no socket there) |
| State hooks in other apps' config | Tests write and remove the hooks in temporary config files, keep backups and the user's own entries | That the other apps keep the same hook format |
| Screen-based agent state | Pattern tests on recorded screen text | New versions of the agents that change their screen |

## Known gaps

- No test calls a real AI provider, search provider or GitHub. Breaking
  changes on their side show up only when users report them.
- The text parts of the UI are checked by golden screenshots, not by
  people reading every screen.
- Model output is never graded automatically; "the AI did the right
  thing" is not something these tests can prove.
