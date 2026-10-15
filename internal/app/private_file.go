package app

import (
	"os"
	"path/filepath"
)

// writePrivate writes a state file only its user may read (0600; a new
// folder is 0700). On a shared server other users must not see the
// recent files, folders, open tabs or errors (Group S5 / S6). A file an
// older version left 0644 is narrowed too.
func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// openPrivateAppend opens a log file for appending with mode 0600, also
// narrowing one an older version left 0644.
func openPrivateAppend(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if fi, err := f.Stat(); err == nil && fi.Mode().Perm()&0o077 != 0 {
		_ = f.Chmod(0o600)
	}
	return f, nil
}
