package ai

import (
	"context"
	"strings"
	"testing"
)

// Spec (owner, 2026-11-09): "We built the tools. termocode knows the
// containers, knows how to connect, knows the databases. So when the
// agent does something it must use our MCP tools, not go digging around
// on its own." (Codex step, "how many tables in my database?": it ran
// `run_command psql -c 'select 1'` and answered from a connection the
// owner had not set up.) A shell command (run_command, terminal_send)
// that does what a termocode tool of this turn does is refused before it
// runs, and the refusal names the tool to use; real build and test
// commands run.

func guardTools(names ...string) []Tool {
	var out []Tool
	for _, n := range names {
		out = append(out, Tool{Name: n, Run: func(context.Context, map[string]any) (string, error) { return "ok", nil }})
	}
	return out
}

func TestShellOwnToolRefusesWhatATermocodeToolDoes(t *testing.T) {
	tools := guardTools("run_command", "terminal_send", "db_connections", "db_query", "db_schema", "redis_command",
		"containers_list", "containers_exec", "contexts_list", "kube_list", "git_status", "git_commit")
	for cmd, want := range map[string]string{
		"psql -c 'select 1'":                           "db_query",
		"PGPASSWORD=x psql -h 127.0.0.1 app":           "db_query",
		"cd /x && mysql -e 'show tables'":              "db_query",
		"sqlite3 app.db .tables":                       "db_query",
		"/usr/bin/mongosh":                             "db_query",
		"redis-cli keys '*'":                           "redis_command",
		"docker ps":                                    "containers_list",
		"sudo podman ps -a":                            "containers_list",
		"docker exec -it db psql":                      "containers_list",
		"kubectl get pods":                             "kube_list",
		"helm list":                                    "kube_list",
		"ssh host":                                     "contexts_list",
		"scp a host:/b":                                "contexts_list",
		"echo hi | ssh -T host":                        "contexts_list",
		"git status":                                   "git_status",
		"env GIT_PAGER=cat git log":                    "git_status",
		"ls; $(docker ps -q)":                          "containers_list",
		"go test ./... && psql -c 'select count(*)' x": "db_query",
	} {
		err := ShellOwnTool(tools, cmd)
		if err == nil {
			t.Errorf("%q ran; want a refusal naming %s", cmd, want)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q: refusal %q does not name %s", cmd, err, want)
		}
	}
	for _, cmd := range []string{"go test ./...", "go build ./cmd/termocode", "npm run build", "make test",
		"grep -rn psql internal", "echo docker", "cat docker-compose.yml", "ls ssh_keys", "gitk-not-a-git-tool", "select 1;", "\\dt"} {
		if err := ShellOwnTool(tools, cmd); err != nil {
			t.Errorf("%q refused: %v", cmd, err)
		}
	}
}

// Without the tool in the turn, the command is the only way: it runs.
func TestShellOwnToolOnlyWhenTheToolIsThere(t *testing.T) {
	tools := guardTools("run_command")
	for _, cmd := range []string{"git status", "psql -c 'select 1'", "docker ps", "ssh host"} {
		if err := ShellOwnTool(tools, cmd); err != nil {
			t.Errorf("%q refused with no termocode tool for it: %v", cmd, err)
		}
	}
}

// The agent refuses such a run_command / terminal_send before the card
// and before Run: the model gets the refusal with the tool to use.
func TestAgentRefusesShellThatDuplicatesATool(t *testing.T) {
	ran := 0
	run := func(context.Context, map[string]any) (string, error) { ran++; return "ok", nil }
	a := &Agent{
		Tools: []Tool{
			{Name: "run_command", Mutating: true, Class: ClassRun, Run: run},
			{Name: "terminal_send", Mutating: true, Class: ClassRun, Run: run},
			{Name: "db_query", Run: run},
			{Name: "containers_list", Run: run},
		},
		Confirm: func(context.Context, Tool, string) bool { return true },
	}
	for _, c := range []ToolCall{
		{ID: "1", Name: "run_command", Args: map[string]any{"command": "psql -c 'select 1'"}},
		{ID: "2", Name: "terminal_send", Args: map[string]any{"text": "docker ps"}},
	} {
		out := a.runTool(context.Background(), c, func(Event) {})
		if !strings.Contains(out, "error:") || !strings.Contains(out, "termocode") {
			t.Errorf("%s %v: %q", c.Name, c.Args, out)
		}
	}
	if ran != 0 {
		t.Fatalf("a refused command ran %d times", ran)
	}
	out := a.runTool(context.Background(), ToolCall{ID: "3", Name: "run_command", Args: map[string]any{"command": "go test ./..."}}, func(Event) {})
	if out != "ok" || ran != 1 {
		t.Fatalf("go test: %q (ran %d)", out, ran)
	}
}
