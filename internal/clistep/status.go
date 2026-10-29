package clistep

import (
	"bytes"
	"context"
	"regexp"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// Status is whether a CLI can run a step.
type Status int

const (
	StatusUnknown   Status = iota // not checked yet, or the check did not answer
	StatusReady                   // installed and logged in
	StatusMissing                 // not on PATH
	StatusLoggedOut               // installed, no login
)

func (s Status) String() string {
	switch s {
	case StatusReady:
		return "ready"
	case StatusMissing:
		return "not installed"
	case StatusLoggedOut:
		return "not logged in"
	}
	return "unknown"
}

// statusArgs is the CLI's own login check (it never uses tokens).
func statusArgs(cli string) []string {
	switch cli {
	case ai.CLIClaude:
		return []string{"auth", "status"}
	case ai.CLICodex:
		return []string{"login", "status"}
	}
	return nil
}

// LoginLine is the shell line of the CLI's own login, named in the
// "not logged in" error for a user who signs in from a terminal (the Add
// AI card runs the same login under a pty; internal/clilogin).
func LoginLine(cli string) string {
	switch cli {
	case ai.CLIClaude:
		return "claude auth login"
	case ai.CLICodex:
		return "codex login"
	}
	return ""
}

var loggedOutJSON = regexp.MustCompile(`"loggedIn"\s*:\s*false`)

// Check says whether cli is installed and logged in, by the CLI's own
// status command (env nil: os.Environ() with termocode's tools/bin
// first, where it installs a CLI). termocode never reads the CLI's
// credential files.
func Check(ctx context.Context, cli string, env []string) Status {
	if env == nil {
		env = lspinstall.EnvWithToolsBin()
	}
	env = CleanEnv(env)
	bin, err := lookPath(Bin(cli), env)
	if err != nil {
		return StatusMissing
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := spawn.Command(bin, statusArgs(cli)...)
	cmd.Env = env
	setProcGroup(cmd)
	// Start here, not in the waiting goroutine: a stop must see the
	// started process to kill its group.
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Start(); err != nil {
		return StatusUnknown
	}
	ch := make(chan error, 1)
	crash.Go("an AI tool check", func() { ch <- cmd.Wait() })
	select {
	case err = <-ch:
	case <-ctx.Done():
		killGroup(cmd.Process.Pid)
		<-ch
		return StatusUnknown
	}
	out := buf.Bytes()
	switch {
	case loggedOutJSON.Match(out) || isLoggedOut(string(out)):
		return StatusLoggedOut
	case err != nil:
		return StatusLoggedOut
	}
	return StatusReady
}
