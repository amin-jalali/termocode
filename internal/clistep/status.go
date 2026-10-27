package clistep

import (
	"context"
	"os"
	"regexp"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
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

// LoginLine is the shell line of the CLI's own login (termocode runs it
// in a terminal tab and never sees the credentials).
func LoginLine(cli string) string {
	switch cli {
	case ai.CLIClaude:
		return "claude auth login"
	case ai.CLICodex:
		return "codex login"
	}
	return ""
}

// InstallLine is the shell line that installs the CLI (shown to the user
// in a terminal tab, who runs it).
func InstallLine(cli string) string {
	switch cli {
	case ai.CLIClaude:
		return "npm install -g @anthropic-ai/claude-code"
	case ai.CLICodex:
		return "npm install -g @openai/codex"
	}
	return ""
}

var loggedOutJSON = regexp.MustCompile(`"loggedIn"\s*:\s*false`)

// Check says whether cli is installed and logged in, by the CLI's own
// status command (env nil: os.Environ()). termocode never reads the
// CLI's credential files.
func Check(ctx context.Context, cli string, env []string) Status {
	if env == nil {
		env = os.Environ()
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
	done := make(chan struct{})
	defer close(done)
	var out []byte
	type res struct {
		b   []byte
		err error
	}
	ch := make(chan res, 1)
	go func() {
		b, err := cmd.CombinedOutput()
		ch <- res{b, err}
	}()
	select {
	case r := <-ch:
		out, err = r.b, r.err
	case <-ctx.Done():
		if cmd.Process != nil {
			killGroup(cmd.Process.Pid)
		}
		<-ch
		return StatusUnknown
	}
	switch {
	case loggedOutJSON.Match(out) || isLoggedOut(string(out)):
		return StatusLoggedOut
	case err != nil:
		if cmd.ProcessState == nil { // it did not start
			return StatusUnknown
		}
		return StatusLoggedOut
	}
	return StatusReady
}
