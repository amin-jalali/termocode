# ADR 0013 — Secret store: OS keyring, else an age-encrypted file; plain only by opt-in

Status: **Accepted** (2026-10-22)

## Context
Every secret (AI API keys, Claude sign-in tokens, MCP secrets and sign-ins)
lived in one plain JSON file, `ai/credentials.json` (mode 0600). That file
is easy to leak: a lost laptop, a backup, a careless `scp -r ~/.config`,
other users on a shared server.

Who we must serve:
- Desktop users with a keyring (Linux, macOS), who want no new prompt.
- SSH users on servers, with no keyring and no D-Bus.
- Termux users: no keyring on Android.
- Users of password managers, who never want a second copy of a key.
- Scripts and CI that already give keys through environment variables.

Alternatives on the table:
- **Keep the plain file**: simple, and the problem stays.
- **Keyring only**: nothing on a server or on Termux.
- **One encrypted file only**: a passphrase prompt even on a desktop that
  has a keyring.
- **Keyring, else encrypted file, plain by choice** (chosen).

## Decision
- A small package, `internal/secrets`, with one `Store` interface and three
  backends. Callers keep their old shapes; `ai.LoadCredentials` and
  `ai.SaveCredentials` go through the store.
- Backend order under `secret_store: auto`: the **OS keyring** when one
  answers, else an **age-encrypted file** (`secrets.age`, scrypt
  passphrase). `auto` never picks the **plain** file. Plain is for users
  who choose it, and for an existing plain file until it is moved.
  `keyring` and `age` force one backend, with no silent fallback.
- The keyring is not tried on Android, on Linux without a D-Bus session
  (`DBUS_SESSION_BUS_ADDRESS` empty) and in an SSH session
  (`SSH_CONNECTION`). Otherwise one probe (set, get, delete a throwaway
  item, 2 s limit) decides. The choice is recorded in `secrets.json`, so
  later starts do not probe again.
- The age passphrase is asked on first use after start and kept in the UI
  process until **AI: Lock Secrets** or quit. Under ADR 0007 the UI process
  is the persistent session, so reconnecting does not ask again. A
  passphrase has at least 8 characters; under 12 it only warns. Three wrong
  tries leave the store locked.
- `secrets.json` is a non-secret index: names, kinds, references. It never
  holds a value. `secrets.age` is one encrypted object `{name: value}`.
- A secret can be a **reference** instead of a value: `${env:NAME}`,
  `${dotenv:NAME}` or `${cmd:line}`. The text is stored, the value is read
  on use. `${cmd:}` runs only from the user's own configuration, so a
  project file (`.termocode/mcp.json`, `databases.json`) may not use it;
  the loaders refuse it with a warning.
- Every value the store hands out is registered for redaction, so it does
  not reach chats, toasts or logs. The AI can not read, set, list or
  unlock secrets, and can not change `secret_store*`.
- **Migration** from the plain file is one flow: write every secret to
  the new store, read each one back and compare, only then overwrite the
  old file with zeros, delete it and write the empty marker
  `credentials.json.migrated`. A failure before the delete leaves the plain
  file as it was. A move cut off in the middle is finished on the next
  start.

## Consequences
- **+** On a desktop, no prompt and no secret in a text file. On a server,
  one passphrase per running UI process, and an admin sees only ciphertext.
- **+** Password-manager users keep one copy of a key, in their manager.
- **+** The backends are pure Go (no cgo), so one binary still builds for
  every platform.
- **−** Two new modules: `github.com/zalando/go-keyring` (with
  `github.com/godbus/dbus/v5` on Linux) and `filippo.io/age`. The binary
  grows. The growth is not measured: the secrets package arrived 80
  commits before this record, so a fair before-and-after build was not
  possible.
- **−** A passphrase prompt on servers and on Termux.
- **−** Go can not lock memory pages portably. While the file is unlocked,
  the passphrase and the values sit in the normal heap. Lock drops them.
  Root on the same host and a debugger are out of scope.
- **−** There is no plain export. A plain copy is what this removes. An
  older termocode finds no `credentials.json` after a move and shows AI as
  not configured; the user sets the keys again.
- **−** The idle timer (`secret_lock_minutes`), Change passphrase and
  Switch backend are not built yet.
