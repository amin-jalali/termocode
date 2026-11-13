// Package agentswap switches the signed-in account of an agent CLI in
// place (AGX-14). It is opt-in (agent_account_swap): it writes the CLI's
// own credential file in the user's home, which termocode otherwise
// never touches.
//
// Capture copies the current credential file into the vault (the secret
// store, base64, name agent.account.<cli>.<label>); Apply writes a
// captured account's file back through a temp file and a rename, after
// keeping the old one as <file>.termocode-backup; Undo puts it back. A
// swap is refused while a tab of that CLI runs, when the file is a
// symlink, and for the CLI that keeps its sign-in in the macOS Keychain.
// The contents are never shown, logged or put in an error.
package agentswap

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Vault is the part of the secret store the swap uses.
type Vault interface {
	Get(name string) (value string, ok bool, err error)
	Set(name, value string) error
	Delete(name string) error
}

var (
	ErrRunning   = errors.New("an agent of this CLI is running: close its tabs first")
	ErrNoAccount = errors.New("no such saved account")
	ErrNoBackup  = errors.New("nothing to undo")
)

// BackupSuffix is added to the credential file's name for the backup.
const BackupSuffix = ".termocode-backup"

// Swapper holds what a swap needs. Running says whether a tab of the CLI
// runs; GOOS is runtime.GOOS (a field for tests).
type Swapper struct {
	Home    string
	Vault   Vault
	Running func(cli string) bool
	GOOS    string
}

var labelRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`)

// ValidLabel reports a label that is safe in a secret name.
func ValidLabel(l string) bool { return labelRe.MatchString(l) }

// credFile is the credential file of a CLI under home.
func credFile(cli, home string) (string, bool) {
	switch cli {
	case "claude":
		return filepath.Join(home, ".claude", ".credentials.json"), true
	case "codex":
		return filepath.Join(home, ".codex", "auth.json"), true
	}
	return "", false
}

// SecretName is the vault name of an account.
func SecretName(cli, label string) string { return "agent.account." + cli + "." + label }

// Supported says whether the CLI keeps its sign-in in a file here; why
// explains a no.
func (s Swapper) Supported(cli string) (bool, string) {
	if _, ok := credFile(cli, s.Home); !ok {
		return false, "this CLI has no credential file termocode knows"
	}
	if cli == "claude" && s.GOOS == "darwin" {
		return false, "Claude Code keeps its sign-in in the macOS Keychain here; termocode does not touch it"
	}
	return true, ""
}

func (s Swapper) path(cli, label string) (string, error) {
	if ok, why := s.Supported(cli); !ok {
		return "", errors.New(why)
	}
	if label != "" && !ValidLabel(label) {
		return "", fmt.Errorf("account name %q: use letters, digits, - and _ (up to 32)", label)
	}
	p, _ := credFile(cli, s.Home)
	return p, nil
}

// regular reads a regular file; a symlink or a missing file is an error
// that never carries content.
func regular(p string) ([]byte, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("the credential file is not a regular file (a link?): termocode will not use it")
	}
	return os.ReadFile(p)
}

// Capture saves the CLI's current sign-in as the account label.
func (s Swapper) Capture(cli, label string) error {
	p, err := s.path(cli, label)
	if err != nil {
		return err
	}
	if label == "" {
		return errors.New("an account needs a name")
	}
	b, err := regular(p)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("this CLI is not signed in: sign in first, then capture the account")
		}
		return err
	}
	return s.Vault.Set(SecretName(cli, label), base64.StdEncoding.EncodeToString(b))
}

// Preview is what Apply would do, without the contents.
type Preview struct {
	Path string
	Size int // bytes that would be written
}

func (s Swapper) load(cli, label string) (string, []byte, error) {
	p, err := s.path(cli, label)
	if err != nil {
		return "", nil, err
	}
	v, ok, err := s.Vault.Get(SecretName(cli, label))
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, ErrNoAccount
	}
	b, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return "", nil, errors.New("the saved account is damaged: capture it again")
	}
	return p, b, nil
}

// Preview names the file and the size of the saved account.
func (s Swapper) Preview(cli, label string) (Preview, error) {
	p, b, err := s.load(cli, label)
	if err != nil {
		return Preview{}, err
	}
	return Preview{Path: p, Size: len(b)}, nil
}

// Apply writes the saved account as the CLI's sign-in.
func (s Swapper) Apply(cli, label string) error {
	p, b, err := s.load(cli, label)
	if err != nil {
		return err
	}
	if s.Running != nil && s.Running(cli) {
		return ErrRunning
	}
	if fi, err := os.Lstat(p); err == nil && !fi.Mode().IsRegular() {
		return errors.New("the credential file is not a regular file (a link?): termocode will not use it")
	}
	if cur, err := regular(p); err == nil {
		if err := writeAtomic(p+BackupSuffix, cur); err != nil {
			return fmt.Errorf("could not keep a backup: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return writeAtomic(p, b)
}

// Undo restores the file Apply replaced and removes the backup.
func (s Swapper) Undo(cli string) error {
	p, err := s.path(cli, "")
	if err != nil {
		return err
	}
	if s.Running != nil && s.Running(cli) {
		return ErrRunning
	}
	b, err := regular(p + BackupSuffix)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNoBackup
		}
		return err
	}
	if err := writeAtomic(p, b); err != nil {
		return err
	}
	return os.Remove(p + BackupSuffix)
}

// writeAtomic writes b to p with mode 0600 through a temp file and a rename.
func writeAtomic(p string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), ".termocode-swap-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, p)
}
