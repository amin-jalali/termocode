package git

import (
	"errors"
	"path"
	"strings"
	"sync"
)

// A workspace that is not on this machine (a container, ADR 0018) runs
// git where it lives. SetRemoteRoot names its root folder and the Runner
// that reaches it; the write commands (Stage, Commit, Push, Checkout …)
// given a folder under that root then run git through the Runner, so
// every caller keeps passing just the folder. Folders outside it stay
// local.

var (
	remoteMu    sync.RWMutex
	remoteRoots = map[string]Runner{}
)

// ErrRemoteUnsupported is returned by the commands that cannot yet run
// through a Runner (hunk staging feeds git apply on stdin).
var ErrRemoteUnsupported = errors.New("not available in a container workspace yet; use the terminal")

// SetRemoteRoot makes git under root run through x. The returned func
// undoes it. A nil x or a relative root does nothing.
func SetRemoteRoot(root string, x Runner) (unset func()) {
	if x == nil || !path.IsAbs(root) {
		return func() {}
	}
	root = path.Clean(root)
	remoteMu.Lock()
	remoteRoots[root] = x
	remoteMu.Unlock()
	return func() {
		remoteMu.Lock()
		delete(remoteRoots, root)
		remoteMu.Unlock()
	}
}

// remoteFor is the Runner for dir when dir is a remote root or under one.
func remoteFor(dir string) (Runner, bool) {
	if dir == "" {
		return nil, false
	}
	remoteMu.RLock()
	defer remoteMu.RUnlock()
	if len(remoteRoots) == 0 {
		return nil, false
	}
	d := path.Clean(dir)
	for root, x := range remoteRoots {
		if d == root || strings.HasPrefix(d, strings.TrimSuffix(root, "/")+"/") {
			return x, true
		}
	}
	return nil, false
}

// remoteRun runs git args in dir through its Runner; ok is false when
// dir is local (the caller runs git here).
func remoteRun(dir string, args ...string) (out string, ok bool, err error) {
	x, ok := remoteFor(dir)
	if !ok {
		return "", false, nil
	}
	out, err = viaGit(x, dir)(args...)
	return out, true, err
}
