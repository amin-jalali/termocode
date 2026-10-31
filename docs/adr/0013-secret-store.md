# ADR 0013 — Secret store: OS keyring, else an age-encrypted file; plain only by opt-in

Status: **Accepted** (2026-10-22). Idle lock, Change passphrase and Switch
backend built 2026-10-22 (U19 SS-8).

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
  the new store, read each one back and compare, the old plain file
  stays. A card asks (Keep for now first, the default); only on Delete
  the copy is checked again, the file overwritten with zeros, deleted and
  the empty marker `credentials.json.migrated` written. A failure leaves
  the plain file as it was. A kept or cut-off move asks again on the next
  start and from AI: Move Secrets to a Safer Store….
- **Switch backend** (store screen) follows the same rule between any two
  backends: copy every name (references stay references), read back and
  compare, record the backend in `secrets.json` and `secret_store`, and
  only then remove the old copies (zeros and delete for a file, delete for
  keyring items). A failure keeps the old store in use.
- **Change passphrase** re-encrypts to a temp file, decrypts it with the
  new passphrase and compares, then renames it over `secrets.age`. The
  current passphrase is always asked.
- **`secret_lock_minutes`** (0 = never) locks the unlocked encrypted file
  after that many idle minutes. It is one one-shot timer, not a ticker: a
  use only stores the time, and the timer, when it fires, either locks or
  waits for the rest. No timer runs with 0, while locked, or for the
  keyring and the plain file.
- A damaged `secrets.age` is never deleted: "Start a new encrypted file"
  keeps it as `secrets.age.broken-<YYYYMMDD-HHMMSS>`. A keyring that
  stops answering (an SSH login to the same machine) shows one toast per
  process.

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
- **−** A passphrase change while a second termocode has the file open:
  that one reads "wrong passphrase" on its next write until it is
  unlocked again with the new passphrase.
- **−** A switch reads the old store once; a secret saved by another
  window during those seconds may stay in the old store only.
