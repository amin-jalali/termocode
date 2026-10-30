package git

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
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
	return runCapped(dir, max, untracked, args...)
}

// runCapped runs git with args in dir and reads at most max bytes of its
// output: a longer output is cut at the last line end before max, cut is
// true and git is stopped. exit1OK accepts exit status 1 (diff --no-index
// says "they differ" so).
func runCapped(dir string, max int, exit1OK bool, args ...string) (out string, cut bool, err error) {
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
		var ee *exec.ExitError
		if !(exit1OK && errors.As(werr, &ee) && ee.ExitCode() == 1) {
			return "", false, cmdError(werr, bytes.TrimSpace(stderr.Bytes()))
		}
	}
	return string(buf), false, nil
}

// CommitHead is the head of a commit for a reader.
type CommitHead struct {
	Hash    string // short
	Author  string
	When    int64 // committer date, Unix seconds
	Subject string
	Body    string
}

// ShowCapped returns a commit's head and its diff (as DiffFile: no
// external diff programs or filters, at most max bytes of output). With
// paths, the diff has only those files.
func ShowCapped(dir, rev string, max int, paths ...string) (head CommitHead, diff string, cut bool, err error) {
	const sep = "\x1f"
	args := []string{"-c", "core.quotepath=off", "show", "--no-color", "--no-ext-diff", "--no-textconv",
		"--format=%h" + sep + "%an" + sep + "%ct" + sep + "%s" + sep + "%b\x1e", rev, "--"}
	out, cut, err := runCapped(dir, max, false, append(args, paths...)...)
	if err != nil {
		return CommitHead{}, "", false, err
	}
	meta, rest, ok := strings.Cut(out, "\x1e")
	if !ok {
		return CommitHead{}, "", cut, errors.New("the commit is too big to show")
	}
	if f := strings.SplitN(meta, sep, 5); len(f) == 5 {
		head = CommitHead{Hash: f[0], Author: f[1], Subject: f[3], Body: strings.TrimSpace(f[4])}
		head.When, _ = strconv.ParseInt(f[2], 10, 64)
	}
	return head, strings.TrimLeft(rest, "\n"), cut, nil
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
