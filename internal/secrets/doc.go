// Package secrets is termocode's secret store (plan U19, ADR 0013 to come).
//
// A Store keeps named secrets ("ai.key.anthropic", "mcp.secret.GITHUB_PAT",
// "db.local-pg") in one backend and lists their names in a non-secret
// index, secrets.json, next to config.json. A secret is either a value or
// a reference (${env:NAME}, ${dotenv:NAME}, ${cmd:line}) that is resolved
// on use and never stored.
//
// Backend order: the OS keyring when one answers (a later phase), else an
// age-encrypted file (secrets.age, scrypt passphrase), and the old plain
// credentials.json only when the user opts in. Open never picks plain on
// its own.
//
// Once per process: the age passphrase is asked on first use after start
// and kept in the UI process until Lock or quit. Under ADR 0007 the UI
// process is the persistent session, so a reconnect does not ask again.
//
// Threat model: theft or backup of the home directory and other users on
// a shared host. Not in scope: root on the same host, a debugger on the
// running process, a ${cmd:} line the user wrote themselves. Go cannot
// lock memory pages portably, so the passphrase and the decrypted values
// live in the normal heap while unlocked; Lock zeroes the passphrase
// bytes and drops the values.
//
// Nothing in this package prints or logs a secret value. Errors name the
// secret or the reference, never the value.
package secrets
