package git

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// DiffFile returns one side of path's change as a unified diff: the staged
// change (index against HEAD), the unstaged one (work tree against the
// index), or, for an untracked file, the whole file as added. A symlink is
// shown as a link (its target's name), never by reading what it points at.
// The user's external diff program and text filters never run.
//
// At most max bytes are read: a longer diff is cut at the last line end
// before max and cut is true (git is stopped, the rest is never read).
func DiffFile(dir, path string, staged, untracked bool, max int) (out string, cut bool, err error) {
	args := []string{"-c", "core.quotepath=off", "diff", "--no-color", "--no-ext-diff", "--no-textconv"}
	switch {
	case untracked:
		args = append(args, "--no-index", "--", "/dev/null", path)
	case staged:
		args = append(args, "--cached", "--", path)
	default:
		args = append(args, "--", path)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := spawn.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", false, err
	}
	if err := cmd.Start(); err != nil {
		return "", false, err
	}
	buf, rerr := io.ReadAll(io.LimitReader(pipe, int64(max)+1))
	if len(buf) > max {
		cut = true
		buf = buf[:max]
		if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
			buf = buf[:i+1]
		} else {
			buf = buf[:0]
		}
		cancel() // the rest is not wanted
		_, _ = io.Copy(io.Discard, pipe)
	}
	werr := cmd.Wait()
	if cut {
		return string(buf), true, nil
	}
	if rerr != nil {
		return "", false, rerr
	}
	if werr != nil {
		// --no-index exits 1 when the files differ, which they do.
		var ee *exec.ExitError
		if !(untracked && errors.As(werr, &ee) && ee.ExitCode() == 1) {
			return "", false, cmdError(werr, bytes.TrimSpace(stderr.Bytes()))
		}
	}
	return string(buf), false, nil
}
