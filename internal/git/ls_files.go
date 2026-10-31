package git

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// TrackedFiles lists the files in the index, relative to dir (the top
// of the work tree), with one `git ls-files -z`. It takes no lock.
func TrackedFiles(dir string) ([]string, error) {
	cmd := spawn.Command("git", "--no-optional-locks", "ls-files", "-z")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	s := strings.TrimRight(string(out), "\x00")
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, "\x00"), nil
}
