package secrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// ── Failure cases (U19 SS-8, plan §5) ──────────────────────────────────

// keyringFail is called when the OS keyring fails a Get (not a missing
// item): it worked when it was chosen but does not answer here, as in an
// SSH login to the same machine.
var keyringFail struct {
	sync.Mutex
	notify func()
}

// SetKeyringFailNotifier sets the func called on every failed keyring
// Get (nil: none). It may run on any goroutine and must not block; the
// app shows one toast per process.
func SetKeyringFailNotifier(fn func()) {
	keyringFail.Lock()
	defer keyringFail.Unlock()
	keyringFail.notify = fn
}

func notifyKeyringFail() {
	keyringFail.Lock()
	fn := keyringFail.notify
	keyringFail.Unlock()
	if fn != nil {
		fn()
	}
}

// BrokenSuffix is added to a damaged secrets.age kept aside, followed by
// the time: secrets.age.broken-20261022-090507.
const BrokenSuffix = ".broken-"

// SetAsideAgeFile renames a damaged secrets.age in dir to
// secrets.age.broken-<YYYYMMDD-HHMMSS> (never deletes it; a second one
// in the same second gets "-2", "-3" …) and drops the index entries of
// the values that were in it, so a new file can start. References and
// account labels stay. It returns the new name.
func SetAsideAgeFile(dir string, now time.Time) (string, error) {
	path := filepath.Join(dir, AgeFile)
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	base := path + BrokenSuffix + now.Format("20060102-150405")
	dst := base
	for i := 2; ; i++ {
		if _, err := os.Lstat(dst); errors.Is(err, os.ErrNotExist) {
			break
		}
		if i > 100 {
			return "", fmt.Errorf("%s: too many kept files", path)
		}
		dst = base + "-" + strconv.Itoa(i)
	}
	if err := renameFn(path, dst); err != nil {
		return "", err
	}
	err := updateIndex(filepath.Join(dir, IndexFile), func(i *Index) {
		for n, e := range i.Entries {
			if e.Kind == KindValue && (e.Backend == "age" || e.Backend == "") {
				delete(i.Entries, n)
			}
		}
	})
	return dst, err
}
