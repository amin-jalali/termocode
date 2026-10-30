package ai

import (
	"context"
	"strings"
	"testing"
)

// Spec (web-easy): a command the AI sends that would open the web panel
// (termocode web, termocode web start, any path to the binary) is
// caught; stop, status and help, and other words, are not.
func TestStartsWebPanel(t *testing.T) {
	for cmd, want := range map[string]bool{
		"termocode web":                         true,
		"termocode web start --yes":             true,
		"termocode  web --dir /x":               true,
		"cd /x && /usr/local/bin/termocode web": true,
		"echo hi; termocode web -y":             true,
		"(termocode web)":                       true,
		"termocode web stop":                    false,
		"termocode web status":                  false,
		"termocode web --help":                  false,
		"termocode webby":                       false,
		"echo termocode-web":                    false,
		"go test ./internal/web":                false,
	} {
		if got := StartsWebPanel(cmd); got != want {
			t.Errorf("%q: %v, want %v", cmd, got, want)
		}
	}
}

// Spec (web-easy): run_command refuses to open the web panel; the
// command never runs.
func TestRunCommandRefusesWebPanel(t *testing.T) {
	root := t.TempDir()
	tl, ok := FindTool(WorkspaceTools(Workspace{Root: root}), "run_command")
	if !ok {
		t.Fatal("no run_command")
	}
	_, err := tl.Run(context.Background(), map[string]any{"command": "touch ran; termocode web"})
	if err == nil || !strings.Contains(err.Error(), "web panel") {
		t.Fatalf("err %v", err)
	}
	if out, _ := tl.Run(context.Background(), map[string]any{"command": "ls"}); strings.Contains(out, "ran") {
		t.Fatal("the command ran")
	}
}
