package mcp

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// Spec (MCPX-33, DI-18): a missing runtime says what to install before
// any start.
func TestCheckRuntimeNamesWhatToInstall(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for cmd, want := range map[string]string{
		"npx": "Node.js", "node": "Node.js", "uvx": "uv", "docker": "Docker", "python3": "Python", "my-tool": "full path",
	} {
		err := CheckRuntime(cmd)
		var mr *MissingRuntime
		if !errors.As(err, &mr) || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), cmd) {
			t.Fatalf("%s: %v (want %q)", cmd, err, want)
		}
	}
	if err := CheckRuntime(os.Args[0]); err != nil {
		t.Fatalf("a full path: %v", err)
	}
}

func TestManagerStartMissingRuntimeSaysInstall(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"pkg": map[string]any{"command": "uvx", "args": []string{"some-mcp"}}})
	t.Setenv("PATH", t.TempDir())
	m := NewManager(ManagerOptions{UserPath: e.user, ProjectPath: e.project, CacheDir: e.cache,
		Expander: func() Expander { return Expander{Env: os.LookupEnv} }, CallTimeout: 5 * time.Second})
	t.Cleanup(m.Close)
	_, err := m.Call(context.Background(), nil, "pkg", "any", nil)
	if err == nil || !strings.Contains(err.Error(), "docs.astral.sh/uv") {
		t.Fatalf("call error: %v", err)
	}
}
