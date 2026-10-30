//go:build !unix

package webdevices

// lockFile: the web panel runs on unix only; inside one process procMu
// is the lock.
func lockFile(string) (func(), error) { return func() {}, nil }
