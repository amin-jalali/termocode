package aihooks

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Spec: the user and project files are merged; the project hash covers
// only the project file; a broken file is named and skipped.
func TestLoad(t *testing.T) {
	root, cfg := t.TempDir(), t.TempDir()
	user := filepath.Join(cfg, "hooks.json")
	writeFile(t, user, `{"post_tool":[{"tools":["*"],"run":"echo user"}]}`)
	writeFile(t, filepath.Join(root, ".termocode", "hooks.json"), `{"post_tool":[{"tools":["write_file"],"run":"gofmt -w x"}]}`)
	s := Load(root, user)
	if len(s.PostTool) != 2 || s.ProjectHash == "" || len(s.Project()) != 1 || s.Project()[0].Run != "gofmt -w x" {
		t.Fatalf("set %+v", s)
	}
	if !s.PostTool[1].Matches("write_file") || s.PostTool[1].Matches("read_file") || !s.PostTool[0].Matches("anything") {
		t.Error("matching")
	}
	writeFile(t, filepath.Join(root, ".termocode", "hooks.json"), `{"post_tool":[{"run":""}]}`)
	s = Load(root, user)
	if len(s.Errors) != 1 || s.ProjectHash != "" || len(s.PostTool) != 1 {
		t.Fatalf("broken file: %+v", s)
	}
	if got := (Hook{TimeoutS: 9999}).Timeout(); got != MaxTimeout {
		t.Errorf("timeout cap %v", got)
	}
}

// Spec: a hook runs in the workspace with the call in its env; the exit
// code and the capped, redacted output come back; a hang is stopped.
func TestRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh scripts")
	}
	root := t.TempDir()
	r := Run(context.Background(), Hook{Run: `pwd; echo "$TERMOCODE_TOOL $TERMOCODE_TOOL_PATH"; echo sk-` + `test1234567890abcdefghij; exit 3`},
		Call{Tool: "write_file", Path: "/w/a.go", Root: root})
	if r.Exit != 3 || r.Err != nil {
		t.Fatalf("result %+v", r)
	}
	real, _ := filepath.EvalSymlinks(root)
	if !strings.Contains(r.Output, "write_file /w/a.go") || !strings.Contains(r.Output, filepath.Base(real)) {
		t.Errorf("output %q", r.Output)
	}
	if strings.Contains(r.Output, "1234567890abcdef") {
		t.Errorf("secret not redacted: %q", r.Output)
	}
	r = Run(context.Background(), Hook{Run: "yes | head -c 100000"}, Call{Root: root})
	if len(r.Output) > OutputMax+4 || !strings.HasSuffix(r.Output, "…") {
		t.Errorf("output not capped: %d", len(r.Output))
	}
	start := time.Now()
	r = Run(context.Background(), Hook{Run: "sleep 30", TimeoutS: 1}, Call{Root: root})
	if r.Exit != -1 || r.Err == nil || time.Since(start) > 10*time.Second {
		t.Errorf("hang not stopped: %+v after %v", r, time.Since(start))
	}
}
