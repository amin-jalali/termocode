package git

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// RefreshIndex writes fresh stat data into the index (FIX-40). The
// background status runs with --no-optional-locks, so it never does
// this itself: a file whose times changed but whose text did not stays
// "stat-dirty" and every status reads it again (17 vs 70 ms at 20 000
// files). This is the one write the poll does; call it rarely.
//
// It takes .git/index.lock only for the moment of the write and never
// waits for it: when another git command holds the lock, it does
// nothing and reports no error (the next call tries again).
func RefreshIndex(dir string) error {
	cmd := spawn.Command("git", "update-index", "-q", "--ignore-submodules", "--refresh")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	// -q prints nothing when the lock is held (exit 128): look for it.
	if lock, lerr := runOut(dir, "git", "rev-parse", "--git-path", "index.lock"); lerr == nil {
		lock = strings.TrimSpace(lock)
		if !filepath.IsAbs(lock) {
			lock = filepath.Join(dir, lock)
		}
		if _, serr := os.Lstat(lock); serr == nil {
			return nil // another git command is busy: skip this round
		}
	}
	return cmdError(err, out)
}
