package git

import (
	"context"
	"errors"
	"path"
	"strconv"
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

// ErrRemoteUnsupported is returned by the commands that cannot run
// through a Runner (hunk and line staging feed git apply on stdin, so
// they need a Runner that is also an InputRunner).
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

// InputRunner is a Runner that can also feed a command's stdin
// (services.Exec is one: `docker exec -i`).
type InputRunner interface {
	Runner
	CaptureInput(ctx context.Context, dir string, input []byte, argv ...string) (stdout, stderr string, code int, err error)
}

// gitOutAt runs git args in dir and gives its stdout: through the Runner
// for a folder under a remote root, here otherwise.
func gitOutAt(dir string, args ...string) (string, error) {
	if x, ok := remoteFor(dir); ok {
		return viaGit(x, dir)(args...)
	}
	return runOut(dir, "git", args...)
}

// gitStdin runs git args in dir with stdin fed to it (git apply -): the
// patch goes over the Runner's stdin for a remote root, so nothing is
// written to a file in the container.
func gitStdin(dir, stdin string, args ...string) error {
	x, ok := remoteFor(dir)
	if !ok {
		return runStdin(dir, stdin, "git", args...)
	}
	ix, ok := x.(InputRunner)
	if !ok {
		return ErrRemoteUnsupported
	}
	ctx, cancel := context.WithTimeout(context.Background(), viaTimeout)
	defer cancel()
	_, stderr, code, err := ix.CaptureInput(ctx, dir, []byte(stdin), append([]string{"git"}, args...)...)
	if err != nil {
		return err
	}
	if code != 0 {
		if msg := firstStderrLine(stderr); msg != "" {
			return errors.New("exit status " + strconv.Itoa(code) + ": " + msg)
		}
		return errors.New("git exited with code " + strconv.Itoa(code))
	}
	return nil
}
