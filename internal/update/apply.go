package update

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// OldPath is where Apply keeps the replaced binary for one Rollback.
func OldPath(exe string) string { return exe + ".old" }

// CanRollback reports whether a binary kept by Apply is there.
func CanRollback(exe string) bool {
	info, err := os.Lstat(OldPath(exe))
	return err == nil && info.Mode().IsRegular()
}

// Apply puts the staged binary in place: the current one is kept as
// termocode.old (a hard link, or a copy where links do not work), then
// the new one is renamed over it, which is atomic: a running termocode
// keeps its old image, the next start runs the new one.
func (s Staged) Apply() error {
	if s.NewPath == "" || s.Exe == "" {
		return errors.New("nothing staged")
	}
	old := OldPath(s.Exe)
	_ = os.Remove(old)
	if err := os.Link(s.Exe, old); err != nil {
		if err := copyFile(s.Exe, old); err != nil {
			s.Discard()
			return fmt.Errorf("could not keep the old binary: %w", err)
		}
	}
	if err := os.Rename(s.NewPath, s.Exe); err != nil {
		_ = os.Remove(old)
		s.Discard()
		return err
	}
	return nil
}

// Rollback puts termocode.old back in place (one step; the new binary is
// gone after it).
func Rollback(exe string) error {
	if !CanRollback(exe) {
		return errors.New("no earlier termocode is kept")
	}
	return os.Rename(OldPath(exe), exe)
}

// copyFile copies src to the new file dst with src's mode.
func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dst)
	}
	return err
}
