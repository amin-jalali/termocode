// Package ledger keeps the state of workflow runs that must survive a
// crash and must never live in the repo (plan WFX §3.7, §4.3):
//
//   - the write ledger of one run: every outside write is recorded as
//     "sending" before it goes out and "done" after, with the marker text
//     that is part of the written comment or PR body, so a resumed run
//     never sends the same write twice;
//   - the seen items of one workflow (idempotency across runs);
//   - the allow-list of one workflow, bound to the sha256 of the
//     workflow file: a changed file drops it, so a repo cannot
//     pre-approve its own writes.
//
// Everything is under <state>/termocode/workflows/<workspace key>, written
// atomically (temp file + rename) and private (0600). No secret is ever
// stored: arguments are kept only as a hash and all stored text is
// redacted.
package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/amin-jalali/termocode/internal/lazyre"
	"github.com/amin-jalali/termocode/internal/session"
)

// Root is the workflow state dir of a workspace:
// $XDG_STATE_HOME/termocode/workflows/<workspace key> (or
// ~/.local/state/…).
func Root(workspace string) (string, error) {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "termocode", "workflows", session.Key(workspace)), nil
}

// FileHash is the hash an allow-list is bound to: "sha256:<hex>" of the
// workflow file bytes.
func FileHash(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

var nameRe = lazyre.New(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// checkName refuses a workflow name or run id that could leave the state
// dir or hide a file.
func checkName(s string) error {
	if !nameRe.MatchString(s) {
		return fmt.Errorf("workflow state: bad name %q", s)
	}
	return nil
}

// writeAtomic writes data to a temp file next to path and renames it
// over path, so a crash never leaves half a file.
func writeAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
