# ADR 0018 — A workspace is reached through a Host; termocode stays local

Status: **Accepted** (2026-10-27)

## Context
Users want to open a project that lives inside a container (local
engine or a remote one over `ssh://`). The code reads the process
working folder (`os.Getwd`) in many places and calls `os` directly for
files, so today a workspace can only be a local folder.

Constraints from the owner:
- Never change a target container: no binaries, no installs, no
  helper files. The only writes are the user's own file edits.
- If the image has no language server, the local one must still work.
- Containers are reached like the Containers view does it (ADR 0016).

Alternatives on the table:
- **Run termocode or nvim inside the container**: changes the target.
- **Copy a helper binary in**: changes the target.
- **Two-way sync to a local copy as the source of truth**: conflicts,
  and the container stops being the truth.
- **termocode stays local, a Host abstraction reaches the files and
  commands** (chosen).

## Decision
- termocode, its UI and nvim always run on the local machine.
- A `workspace.Workspace{ID, Kind, Root, Host}` names the open folder.
  The ID is `local:/abs/path` or `container:<context>/<name>:/app`.
  Sessions, backups and per-workspace state are keyed by
  `workspace.Key(ID)`; for a local ID it is the old path key, so state
  written before still loads.
- A `Host` gives `FS()` (Stat, ReadDir, ReadFile, WriteFile, Mkdir,
  Rename, Remove; paths inside Root only), `Exec()` (the
  `services.Exec` type), `TermArgv()`, `Caps()`, `Online()` and
  `Label()`.
- `workspace/localhost` uses `os`. A container host uses only the engine
  CLI: `docker cp` tar streams and `docker exec` with what the image
  already has (`sh` one-liners with paths as positional arguments).
  Nothing is installed or left in the target.
- Language servers, in this order: the one in the image → a local one
  through a URI-rewriting proxy (bind mount or a read-only local
  mirror) → an opt-in disposable sidecar container → none.
- The app reads the workspace root from the Model (`m.ws`), not from the
  process folder.

## Consequences
- **+** Every feature that goes through `FS` and `Exec` works for a
  container without a new code path.
- **+** The target container stays exactly as the user built it.
- **−** Every file and command call on a container is a process start;
  calls need time limits, and remote engines add latency.
- **−** Features that call `os` or the process folder directly must be
  moved to the Host first; until then they work on local workspaces only.
- **−** A local language server may not see dependencies that exist only
  inside the image.
