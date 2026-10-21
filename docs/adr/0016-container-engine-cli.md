# ADR 0016 — Container engines are driven through their CLI, not the Engine API

Status: **Accepted** (2026-10-21)

## Context
The Services view (U6) started with Docker Compose and chose the `docker`
command line without writing it down. The Containers view (U22) adds every
container, image, volume and network on the host, contexts, registries and
logins. Each of these could be done with the engine's HTTP API on its socket
instead.

Constraints:
- Docker and Podman must both work, on Linux, macOS and Termux.
- Contexts, `DOCKER_HOST`, `ssh://` and TLS endpoints, and credential
  helpers must behave as they do in the user's own shell.
- Logs, shells, attach and pulls need a terminal (a TTY), and compose has
  no API.
- A hidden view must cost no process, goroutine or timer.
- Child processes go through `internal/spawn` (ADR 0011 on Android).

Alternatives on the table:
- **The Docker SDK**: a large module, it bypasses `internal/spawn` on
  Android, and it still has no compose.
- **The raw socket API**: our own code for contexts, TLS, ssh tunnels,
  credential helpers, and a TTY.
- **The CLI with JSON output** (chosen).

## Decision
- Every list and action runs the engine's CLI (`docker`, else `podman`;
  `containers_engine` can force one) with JSON output, through
  `internal/spawn`, the same way the compose backend does.
- One `docker events` process runs only while the view is on screen. It
  stops when the view is hidden.
- Long, visible jobs (logs, shell, attach, stats, pull, compose) are
  terminal tabs, so the user sees progress and can type.
- Secrets go to the CLI on stdin only (`login --password-stdin`), never as
  an argument or in the environment.
- Names and ids are checked and passed as plain arguments, never through a
  shell.
- Engine access sits behind the `internal/containers` package, so the API
  could later serve list calls without touching the views.

## Consequences
- **+** Contexts, ssh and TLS endpoints, credential helpers and Podman come
  with the CLI for free; no socket or TLS code to own or secure.
- **+** Tests use a fake engine binary; no daemon is needed.
- **−** A process starts for every call. Calls have time limits, and the
  view reads on open, on refresh and after actions rather than on a timer.
- **−** The JSON parsers depend on the CLI's output. They are tested with
  fixtures per engine version.
- **−** Behaviour follows the installed CLI version; a very old CLI may
  lack a field.
