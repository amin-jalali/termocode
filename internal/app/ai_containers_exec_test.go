package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/workspace"
	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

// containers_exec: a shell, attach or a command in a container opens a
// terminal tab in the IDE (as e / a / Exec with Options… do); debug starts
// the toolbox; files / read browse its files and upload puts one in;
// pair_sync starts the GUI's Pair Sync, whose own card lists every change.

type aiCtrTab struct {
	cmd services.Command
	key string
}

func aiExecTool(t *testing.T) (*Model, ai.Tool, *[]aiCtrTab, string) {
	t.Helper()
	m, _, call := aiContainersTestModel(t)
	var tabs []aiCtrTab
	old := aiCtrTerminal
	aiCtrTerminal = func(_ *Model, c services.Command, key string) error {
		tabs = append(tabs, aiCtrTab{c, key})
		return nil
	}
	t.Cleanup(func() { aiCtrTerminal = old })
	box := t.TempDir() // the container's file system
	oldFS := aiCtrFiles
	aiCtrFiles = func(context.Context, aiContainersHost, string) (workspace.FS, error) {
		ws, err := localhost.New(box)
		return ws.Host.FS(), err
	}
	t.Cleanup(func() { aiCtrFiles = oldFS })
	return m, aiContainersTool(t, m.aiContainersTools(call), "containers_exec"), &tabs, box
}

func TestAIContainersExecGates(t *testing.T) {
	_, tl, _, _ := aiExecTool(t)
	if got := aiParamEnum(tl.Params["op"]); strings.Join(got, ",") != "shell,attach,exec,debug,files,read,upload,pair_sync" {
		t.Errorf("ops = %v", got)
	}
	for op, want := range map[string][2]any{
		"shell": {false, ""}, "attach": {false, ""}, "files": {false, ""}, "read": {false, ""}, "pair_sync": {false, ""},
		"exec":  {true, ai.ClassContainers},
		"debug": {true, ""}, "upload": {true, ""},
	} {
		if ask, class := tl.Gate(map[string]any{"op": op}); ask != want[0] || class != want[1] {
			t.Errorf("%s gate = %v %q, want %v %q", op, ask, class, want[0], want[1])
		}
	}
}

func TestAIContainersExecOpensTabs(t *testing.T) {
	_, tl, tabs, _ := aiExecTool(t)
	for _, a := range []map[string]any{
		{"op": "shell", "target": "termocode-api-1"},
		{"op": "attach", "target": "termocode-api-1"},
		{"op": "exec", "target": "termocode-api-1", "command": []any{"ls", "-la", "/app"}, "user": "root", "workdir": "/app"},
		{"op": "debug", "target": "termocode-api-1"},
		{"op": "shell", "target": "termocode-api-1", "context": "prod"},
	} {
		if err := tl.Validate(a); err != nil {
			t.Fatalf("%v: %v", a, err)
		}
		out, err := tl.Run(context.Background(), a)
		if err != nil || !strings.Contains(out, "terminal tab") {
			t.Fatalf("%v: %q %v", a, out, err)
		}
	}
	if len(*tabs) != 5 {
		t.Fatalf("tabs = %+v", *tabs)
	}
	want := []string{
		"exec -it 9f3c2e1a1234 sh -c",
		"attach --sig-proxy=false 9f3c2e1a1234",
		"exec -it --user root --workdir /app 9f3c2e1a1234 ls -la /app",
		"run --rm -it --pid container:9f3c2e1a1234 --network container:9f3c2e1a1234",
		"exec -it 9f3c2e1a1234 sh -c",
	}
	for i, w := range want {
		if got := strings.Join((*tabs)[i].cmd.Argv, " "); !strings.Contains(got, w) {
			t.Errorf("tab %d argv %q, want %q", i, got, w)
		}
	}
	if env := (*tabs)[4].cmd.Env; env["DOCKER_CONTEXT"] != "prod" {
		t.Errorf("prod shell env = %v", env)
	}
	if (*tabs)[0].key == (*tabs)[4].key {
		t.Error("the prod shell shares the local shell's tab key")
	}
	for _, bad := range []map[string]any{
		{"op": "shell", "target": "nginx"}, // not running
		{"op": "exec", "target": "termocode-api-1"},
		{"op": "exec", "target": "termocode-api-1", "command": []any{"-x"}},
		{"op": "exec", "target": "termocode-api-1", "command": []any{"ls"}, "workdir": "app"},
		{"op": "debug", "target": "termocode-api-1", "image": "-it"},
	} {
		if err := tl.Validate(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestAIContainersExecFiles(t *testing.T) {
	m, tl, _, box := aiExecTool(t)
	if err := os.MkdirAll(filepath.Join(box, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(box, "app", "config.yml"), []byte("port: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := tl.Run(context.Background(), map[string]any{"op": "files", "target": "termocode-api-1", "path": "/app"})
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "files", out, "/app", "config.yml")
	out, err = tl.Run(context.Background(), map[string]any{"op": "read", "target": "termocode-api-1", "path": "/app/config.yml"})
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "read", out, "port: 8080")
	local := filepath.Join(m.services.root, "seed.sql")
	if err := os.WriteFile(local, []byte("select 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	up := map[string]any{"op": "upload", "target": "termocode-api-1", "file": "seed.sql", "path": "/app"}
	covAIMustContain(t, "upload card", tl.Describe(up), "Upload seed.sql", "/app", "termocode-api-1")
	if _, err := tl.Run(context.Background(), up); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(box, "app", "seed.sql")); err != nil || string(b) != "select 1;\n" {
		t.Errorf("uploaded %q %v", b, err)
	}
	if _, err := tl.Run(context.Background(), up); err == nil || !strings.Contains(err.Error(), "replace") {
		t.Errorf("second upload: %v", err)
	}
	up["replace"] = true
	if _, err := tl.Run(context.Background(), up); err != nil {
		t.Errorf("replace: %v", err)
	}
	if err := tl.Validate(map[string]any{"op": "upload", "target": "termocode-api-1", "file": "../x", "path": "/app"}); err == nil {
		t.Error("upload from outside the workspace accepted")
	}
}
