//go:build !unix

package app

// pidAlive cannot check another process here, so it never takes the
// backup of what may be a running instance; only a re-exec of this
// process (same pid) gets its backups back.
func pidAlive(int) bool { return true }
