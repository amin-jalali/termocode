//go:build unix

package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// Spec: the manager starts a "sandbox" entry through the container tool
// (a fake docker script that runs the inner command), so its tools come
// from inside the sandbox; argv has network off and env names only.
func TestManagerStartsServerInSandbox(t *testing.T) {
	e := newManagerEnv(t)
	bin := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$SB_ARGS\"\nwhile [ \"$1\" != fake-image ]; do shift; done\nshift\nexec \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	argsFile := filepath.Join(e.dir, "args")
	ent := mcptest.Entry(map[string]string{"FAKE_LOG": e.log, "SB_ARGS": argsFile, "TOKEN": "sk-" + "test-x"})
	ent["sandbox"] = map[string]any{"image": "fake-image"}
	e.write(t, e.user, map[string]any{"fake": ent})
	m := e.managerTools(t, &toolsRec{})
	if n := countTools(m.Tools(context.Background(), nil)); n != 10 {
		t.Fatalf("tools %d (status %+v)", n, status(m, "fake"))
	}
	if got := status(m, "fake").Sandbox; got != "sandbox: container (fake-image) · network off · workspace read-only" {
		t.Errorf("status sandbox %q", got)
	}
	b, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.ReplaceAll(string(b), "\n", " ")
	if !strings.HasPrefix(argv, "run -i --rm --init --network none ") || !strings.Contains(argv, "-e TOKEN ") || strings.Contains(argv, "test-x") {
		t.Fatalf("argv %s", argv)
	}
}
