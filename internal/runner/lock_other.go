//go:build !unix

package runner

import "sync"

// The runner is for unix servers; elsewhere one process-wide mutex is
// the lock (a second runner process is not kept out).
var procMu sync.Mutex

func lockFile(string) (func(), error) {
	procMu.Lock()
	return procMu.Unlock, nil
}

func tryLockFile(string) (func(), error) { return func() {}, nil }
