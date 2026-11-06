//go:build !unix

package ledger

import (
	"os"
	"sync"
)

// Without flock the claim holds inside this process only.
var (
	ownerMu   sync.Mutex
	ownerHeld = map[string]bool{}
)

func lockOwner(path string) (*os.File, func(), error) {
	ownerMu.Lock()
	defer ownerMu.Unlock()
	if ownerHeld[path] {
		return nil, nil, ErrOwned
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, nil, err
	}
	ownerHeld[path] = true
	return f, func() {
		ownerMu.Lock()
		delete(ownerHeld, path)
		ownerMu.Unlock()
	}, nil
}
