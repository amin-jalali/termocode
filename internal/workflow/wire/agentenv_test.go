//go:build unix

package wire

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentenv"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/run"
)

type wireSecrets map[string]string

func (w wireSecrets) Get(name string) (string, bool, error) {
	v, ok := w[name]
	return v, ok, nil
}

func agentEnvFile(t *testing.T, src string) *agentenv.Env {
	t.Helper()
	e, err := agentenv.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func envHas(env []string, prefix string) bool {
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return true
		}
	}
	return false
}

func TestAgentStepRunsSetupOnceWithSecretsAndNetworkRules(t *testing.T) {
	wt := t.TempDir()
	var specs []clistep.Spec
	s := &Steps{RunID: "r1", Workspace: t.TempDir(), AgentEnv: []string{"PATH=" + os.Getenv("PATH")},
		AgentEnvFile: agentEnvFile(t, `{"setup": ["echo $NPM_TOKEN-$NODE_ENV >> setup.log"],
			"env": {"NODE_ENV": "test"}, "secrets": {"NPM_TOKEN": "npm.token"},
			"network": {"mode": "package-managers"}}`),
		AgentSecrets: wireSecrets{"npm.token": "s3cret"},
		Agent: func(_ context.Context, sp clistep.Spec, _ string, _ func(ai.Event)) (clistep.Result, error) {
			specs = append(specs, sp)
			return clistep.Result{Text: "done"}, nil
		}}
	call := run.RunCall{Step: agentStep("codex", "worktree"), Item: agentItem(), Env: run.ItemEnv{Worktree: wt}}
	for i := 0; i < 2; i++ {
		if _, err := s.Run(context.Background(), call); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(wt, "setup.log"))
	if err != nil || strings.TrimSpace(string(b)) != "s3cret-test" {
		t.Errorf("setup ran %q (%v); want once with the secret", b, err)
	}
	if len(specs) != 2 {
		t.Fatalf("%d agent runs", len(specs))
	}
	env := specs[0].Env
	if !envHas(env, "NODE_ENV=test") || !envHas(env, "HTTPS_PROXY=http://127.0.0.1:") {
		t.Errorf("agent env lacks the vars or the proxy: %v", env)
	}
	if envHas(env, "NPM_TOKEN=") {
		t.Error("the agent got the setup secret")
	}
}

func TestAgentStepReadOnlySkipsSetup(t *testing.T) {
	ws := t.TempDir()
	var got clistep.Spec
	s := &Steps{RunID: "r1", Workspace: ws, AgentSandbox: workflow.SandboxReadOnly, AgentEnv: []string{"PATH=" + os.Getenv("PATH")},
		AgentEnvFile: agentEnvFile(t, `{"setup": ["touch ran"], "network": {"mode": "open"}}`),
		Agent: func(_ context.Context, sp clistep.Spec, _ string, _ func(ai.Event)) (clistep.Result, error) {
			got = sp
			return clistep.Result{}, nil
		}}
	if _, err := s.Run(context.Background(), run.RunCall{Step: agentStep("codex", "")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, "ran")); err == nil {
		t.Error("setup ran in the workspace of a read-only step")
	}
	if envHas(got.Env, "HTTPS_PROXY=") {
		t.Error("open network still has a proxy")
	}
}

func TestAgentStepSetupFailureStopsTheAgent(t *testing.T) {
	calls := 0
	s := &Steps{RunID: "r1", Workspace: t.TempDir(), AgentEnv: []string{"PATH=" + os.Getenv("PATH")},
		AgentEnvFile: agentEnvFile(t, `{"setup": ["exit 7"]}`),
		Agent: func(context.Context, clistep.Spec, string, func(ai.Event)) (clistep.Result, error) {
			calls++
			return clistep.Result{}, nil
		}}
	_, err := s.Run(context.Background(), run.RunCall{Step: agentStep("codex", "worktree"), Item: agentItem(), Env: run.ItemEnv{Worktree: t.TempDir()}})
	if err == nil || !strings.Contains(err.Error(), "setup") || calls != 0 {
		t.Errorf("err %v, %d runs", err, calls)
	}
}

func TestAgentStepMissingSecretStopsTheAgent(t *testing.T) {
	calls := 0
	s := &Steps{RunID: "r1", Workspace: t.TempDir(), AgentEnv: []string{"PATH=" + os.Getenv("PATH")},
		AgentEnvFile: agentEnvFile(t, `{"setup": ["true"], "secrets": {"A": "missing.one"}}`),
		AgentSecrets: wireSecrets{},
		Agent: func(context.Context, clistep.Spec, string, func(ai.Event)) (clistep.Result, error) {
			calls++
			return clistep.Result{}, nil
		}}
	_, err := s.Run(context.Background(), run.RunCall{Step: agentStep("codex", "worktree"), Item: agentItem(), Env: run.ItemEnv{Worktree: t.TempDir()}})
	if err == nil || !strings.Contains(err.Error(), "missing.one") || calls != 0 {
		t.Errorf("err %v, %d runs", err, calls)
	}
}
