//go:build !unix

package git

import "os/exec"

// detachTTY is a no-op where there are no sessions; GIT_TERMINAL_PROMPT=0
// still keeps git from prompting.
func detachTTY(*exec.Cmd) {}
