package git

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"

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

// ErrHunkGone: the hunk header asked for is not in the file's diff now
// (the file or the index changed since the diff was read).
var ErrHunkGone = errors.New("that change is not in the diff any more")

// StageHunkHeader stages the one hunk of path's unstaged diff whose
// header line ("@@ -a,b +c,d @@ …") is exactly header. The exact header is
// the precondition: a diff read earlier that no longer matches stages
// nothing (ErrHunkGone) instead of another change.
func StageHunkHeader(dir, path, header string) error {
	return applyHunkHeader(dir, path, header, false)
}

// UnstageHunkHeader takes the one hunk of path's staged diff whose header
// is exactly header out of the index; the work tree keeps it.
func UnstageHunkHeader(dir, path, header string) error {
	return applyHunkHeader(dir, path, header, true)
}

func applyHunkHeader(dir, path, header string, staged bool) error {
	const maxHunkDiff = 8 << 20
	diff, cut, err := DiffFile(dir, path, staged, false, maxHunkDiff)
	if err != nil {
		return err
	}
	patch, ok := extractHunkBy(diff, func(h string) bool { return h == header })
	if !ok {
		if cut {
			return errors.New("the diff is too big to pick one change from")
		}
		return ErrHunkGone
	}
	args := []string{"apply", "--cached"}
	if staged {
		args = append(args, "--reverse")
	}
	return runStdin(dir, patch, "git", append(args, "-")...)
}

// extractHunkBy returns a one-hunk patch (the file header and the first
// hunk whose header line match accepts).
func extractHunkBy(diff string, match func(header string) bool) (string, bool) {
	lines := strings.Split(diff, "\n")
	i := 0
	for i < len(lines) && !strings.HasPrefix(lines[i], "@@") {
		i++
	}
	head := lines[:i]
	for i < len(lines) {
		start := i
		i++
		for i < len(lines) && !strings.HasPrefix(lines[i], "@@") {
			i++
		}
		if match(lines[start]) {
			var b strings.Builder
			for _, l := range append(append([]string(nil), head...), lines[start:i]...) {
				b.WriteString(l)
				b.WriteByte('\n')
			}
			return strings.TrimSuffix(b.String(), "\n\n") + "\n", true
		}
	}
	return "", false
}
