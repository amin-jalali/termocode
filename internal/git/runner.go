package git

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Runner runs a command where a workspace lives, for a workspace that is
// not on this machine (a container, ADR 0018). services.Exec fits it:
// dir is a folder on that machine, argv[0] the program.
type Runner interface {
	Capture(ctx context.Context, dir string, argv ...string) (stdout, stderr string, code int, err error)
}

// viaTimeout caps one git command run through a Runner: a hung engine
// must not keep a refresh waiting forever.
const viaTimeout = 30 * time.Second

// gitRunFn runs one git command (args without "git") and gives its stdout.
type gitRunFn func(args ...string) (string, error)

// localGit runs git in dir on this machine.
func localGit(dir string) gitRunFn {
	return func(args ...string) (string, error) { return runOut(dir, "git", args...) }
}

// viaGit runs git in dir through x. A non-zero exit is an error naming
// git's first stderr line, like a local run's.
func viaGit(x Runner, dir string) gitRunFn {
	return func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), viaTimeout)
		defer cancel()
		out, stderr, code, err := x.Capture(ctx, dir, append([]string{"git"}, args...)...)
		if err != nil {
			return "", err
		}
		if code != 0 {
			if msg := firstStderrLine(stderr); msg != "" {
				return out, errors.New(msg)
			}
			return out, errors.New("git exited with code " + strconv.Itoa(code))
		}
		return out, nil
	}
}

func firstStderrLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
