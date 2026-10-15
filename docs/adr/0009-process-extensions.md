# ADR 0009 — Process extensions (Tier 2) over JSON-RPC on stdio

Status: **Accepted** (2026-10-15)

## Context
Tier 1 extensions are Lua inside the embedded nvim (ADR 0006). That is
light, but Lua only, no isolation, and slow for heavy work. ADR 0006 left a
follow-up: out-of-process extensions in any language, with the Tier 1 API
names mapped onto them.

Choices to make:

1. **Framing:** newline-delimited JSON (simple to write by hand) or LSP's
   `Content-Length` headers (every language already has a codec; messages
   may hold newlines without escaping rules of our own).
2. **Where registrations live:** a second set of palette rows, panels and
   status items, or the same registry the Lua side fills.
3. **Life cycle:** when to start, what to do on a crash, how to make sure
   no process outlives termocode.

## Decision
- **Protocol:** JSON-RPC 2.0, framed like LSP (`Content-Length: N\r\n\r\n`
  + body), over the process's stdin/stdout. The message shapes follow
  `mobocode/agent/rpc.go`. stderr goes to the extension log. Methods and
  params: `docs/extensions.md` "Process extensions (Tier 2)".
  - termocode → ext: `initialize` (workspace root, version, capabilities),
    `command/execute`, `event/notify` (`open`, `save`), `panel/select`,
    `shutdown`, `exit`.
  - ext → termocode: `registerCommand`, `registerStatusItem`,
    `statusItem/update`, `registerPanel`, `panel/setContent` (Tier 1
    markup), `notify`, `preview`, `openFile`, `prompt` / `pick` (requests
    the UI answers later), `readFile`, `log`.
- **Manifest:** `extension.json` with `"run": {"command", "args", "env"}`
  marks a process extension (`internal/ext/process.go`). Optional
  `runtime` hint, `activationEvents` (`onStartup`, `*`, `onSave`,
  `onOpen`) and `contributes.commands` (palette rows before the process
  runs; the first run starts it).
- **Host:** `internal/extproc` — `rpc.go` (codec; `Conn` with id matching,
  context timeouts and a non-blocking write queue) and `host.go` (one
  supervisor goroutine per extension). It answers `readFile` and `log`
  itself and checks `openFile` / `readFile` paths against the workspace
  (`workspace.go`, symlinks resolved). Everything else becomes an `Event`
  on one channel.
- **App glue:** `internal/app/ext_proc.go`. A Cmd waits on the event
  channel (taking up to 64 at once) so Update never waits on a process.
  Process registrations are merged with the Lua registry into
  `extState.reg` (`extRebuild`), so the palette (`ext-<id>`), activity bar,
  status bar, hit map, hover labels and `keymap.json` `ext:` bindings work
  the same for both tiers. A command id owned by a process goes to
  `Host.Execute` (a Cmd); a panel click becomes `panel/select`.
- **Life cycle:** one process per extension; process group of its own
  (`Setpgid`), Pdeathsig on Linux. On a crash: drop its registrations,
  toast, restart after 0.5 s / 1 s / 2 s; a 4th crash in a row leaves it
  failed until reload. Stop = `shutdown` request, `exit` notification,
  stdin closed, 2 s grace, then SIGKILL to the whole group. `Model.Close`
  and `app.StopExtensions` (cmd/termocode, after Bubble Tea returns, also
  on the crash path) stop them. Each run has a host-wide generation number
  so events of an old run (before a crash or a reload) are dropped.
- **AI:** process command palette ids are `ext-…` like Lua ones, so the
  AI policy (confirm, class run) covers them with no new rule.

## Consequences
- **+** Extensions in any language, each in its own process: a crash or a
  busy loop cannot take termocode or nvim down, and heavy work is fine.
- **+** One registry for both tiers: no second set of UI code paths.
- **+** Testable without a real runtime: the tests re-run the test binary
  as a fake extension (`internal/extproc/extproctest`).
- **−** Not a sandbox. The process has the user's rights; the
  workspace-only rule covers the API (`readFile`, `openFile`), not what
  the program does itself. Documented as "run code you trust".
- **−** Each extension needs its runtime installed (the Python sample
  needs `python3`). Start-up cost per process; lazy start helps.
- **−** Panels are pushed (`panel/setContent`), not pulled each second as
  in Tier 1; the extension decides when to refresh. Panel size is not sent.
- Follow-ups: more events (`changed`, `cursor`), buffer access (read the
  unsaved text), a `workspace/files` call, a Node helper next to the
  Python one.
