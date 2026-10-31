package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// "envFile" loads KEY=value lines into the server's env; a key in "env"
// wins. A missing file means the server needs it and does not start.
func TestExpandEnvFile(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "conf", "mcp.env"), "A=from-file\nB=also\n# c\n")
	body := `{"mcpServers": {"s": {"command": "x", "envFile": "${workspaceFolder}/conf/mcp.env", "env": {"B": "own"}}}}`
	list, _, err := ParseFile([]byte(body), SourceUser, "/u/mcp.json")
	if err != nil || len(list) != 1 || list[0].Problem != "" {
		t.Fatalf("parse: %v %+v", err, list)
	}
	got, err := list[0].Expand(Expander{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	if got.Env["A"] != "from-file" || got.Env["B"] != "own" {
		t.Errorf("env = %v", got.Env)
	}
	s := list[0]
	s.EnvFile = filepath.Join(ws, "nope.env")
	_, err = s.Expand(Expander{Workspace: ws})
	var ne *NeedsError
	if !errors.As(err, &ne) {
		t.Errorf("missing envFile: err = %v, want a NeedsError", err)
	}
}

// A relative envFile is read from the workspace folder.
func TestExpandEnvFileRelative(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, ".env"), "K=v\n")
	got, err := Server{Name: "s", Command: "x", EnvFile: ".env"}.Expand(Expander{Workspace: ws})
	if err != nil || got.Env["K"] != "v" {
		t.Errorf("env = %v, err = %v", got.Env, err)
	}
}

// A project file's envFile must stay inside the workspace: a cloned repo
// must not feed ~/.secrets.env (or a link to it) into a server.
func TestProjectEnvFileStaysInWorkspace(t *testing.T) {
	ws := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.env")
	writeFile(t, outside, "TOKEN=x\n")
	if err := os.Symlink(outside, filepath.Join(ws, "link.env")); err != nil {
		t.Skip("no symlinks:", err)
	}
	for _, f := range []string{outside, "link.env", "../x.env"} {
		s := Server{Name: "p", Command: "x", EnvFile: f, Source: SourceProject}
		got, err := s.Expand(Expander{Workspace: ws})
		if err == nil || got.Env["TOKEN"] != "" {
			t.Errorf("envFile %q: err = %v env = %v, want refused", f, err, got.Env)
		}
	}
	// The user's own file may point anywhere.
	got, err := Server{Name: "u", Command: "x", EnvFile: outside, Source: SourceUser}.Expand(Expander{Workspace: ws})
	if err != nil || got.Env["TOKEN"] != "x" {
		t.Errorf("user envFile: env = %v err = %v", got.Env, err)
	}
}
