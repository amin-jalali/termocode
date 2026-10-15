package git

import (
	"os"
	"os/exec"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// remoteCmd builds a git command that may talk to a remote (push, pull,
// fetch, clone). termocode owns the terminal, so such a command must never
// prompt on it: a "Username for …" prompt would draw over the UI and wait
// for input the user cannot see. It runs with GIT_TERMINAL_PROMPT=0 and
// batch-mode ssh (see cloneEnv), in its own session so it has no
// controlling terminal at all (an ssh or credential-helper prompt that
// opens /dev/tty fails instead of blocking), and with no stdin.
// Credential helpers, askpass programs and ssh-agent still work.
func remoteCmd(dir string, args ...string) *exec.Cmd {
	cmd := spawn.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = cloneEnv(os.Environ(), "", "", "")
	cmd.Stdin = nil
	detachTTY(cmd)
	return cmd
}
