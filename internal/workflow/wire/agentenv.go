package wire

import (
	"context"
	"strings"

	"github.com/amin-jalali/termocode/internal/agentenv"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/workflow/run"
)

// setupRun is one worktree's setup: it runs once per run, the other
// items in the same worktree wait for it and share its result.
type setupRun struct {
	done chan struct{}
	err  error
}

// agentEnvFor builds an agent run's environment from the project's
// agent environment file (CMP-63). write steps run the setup commands
// once in dir first (secrets only there); read-only steps never do (dir
// is then the user's workspace). The returned proxy keeps the network
// rule (nil: open); the caller closes it.
func (s *Steps) agentEnvFor(ctx context.Context, agent, dir string, write bool) ([]string, *agentenv.Proxy, error) {
	ae := s.AgentEnvFile
	if ae == nil {
		return s.AgentEnv, nil, nil
	}
	base := s.AgentEnv
	if base == nil {
		base = lspinstall.EnvWithToolsBin()
	}
	base = agentenv.Merge(base, ae.VarEnv()...)
	if write && len(ae.Setup) > 0 {
		if err := s.setupOnce(ctx, dir, base, ae); err != nil {
			return nil, nil, err
		}
	}
	pol := ae.Network.Policy(agentenv.AgentHosts(agent)...)
	if pol.Open() {
		return base, nil, nil
	}
	px, err := agentenv.StartProxy(pol)
	if err != nil {
		return nil, nil, run.Permanent(err)
	}
	return agentenv.Merge(base, px.EnvVars()...), px, nil
}

// setupOnce runs ae's setup in dir unless it ran there in this run.
func (s *Steps) setupOnce(ctx context.Context, dir string, base []string, ae *agentenv.Env) error {
	key := dir + "\x00" + ae.Hash
	s.setupMu.Lock()
	if s.setupRuns == nil {
		s.setupRuns = map[string]*setupRun{}
	}
	r, ok := s.setupRuns[key]
	if !ok {
		r = &setupRun{done: make(chan struct{})}
		s.setupRuns[key] = r
	}
	s.setupMu.Unlock()
	if ok {
		select {
		case <-r.done:
			return r.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer close(r.done)
	secretEnv, values, err := ae.SecretEnv(s.AgentSecrets)
	if err != nil {
		r.err = run.Permanent(err)
		return r.err
	}
	if _, err := agentenv.RunSetup(ctx, dir, agentenv.Merge(base, secretEnv...), ae.Setup, ae.SetupTimeout, values); err != nil {
		r.err = err
		if ctx.Err() == nil {
			r.err = run.Permanent(err) // a broken setup does not heal by a retry
		}
		return r.err
	}
	return nil
}

// networkNote is the line added to an agent's output for blocked hosts.
func networkNote(blocked []string) string {
	return "[network rule] blocked: " + strings.Join(blocked, ", ") + " (" + agentenv.File + ")"
}
