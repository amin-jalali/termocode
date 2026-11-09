package ai

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// The AI uses termocode's tools, by construction (owner, 2026-11-09): a
// shell command it sends (run_command, terminal_send) that does what one
// of the turn's termocode tools does — a database client, docker,
// kubectl, ssh, git — is refused before the card and before it runs, and
// the refusal names the tool to use. A command with no such tool in the
// turn runs as before; build and test commands always run.

// shellOwnRule is one family of programs and the termocode tools that
// replace it (the first one the turn has is named first).
type shellOwnRule struct {
	progs []string
	tools []string
	what  string
}

var shellOwnRules = []shellOwnRule{
	{[]string{"psql", "pg_dump", "pgcli", "mysql", "mariadb", "mycli", "mysqldump", "sqlite3", "mongosh", "mongo", "usql", "sqlcmd", "clickhouse-client"},
		[]string{"db_query", "db_schema", "db_connections", "db_connection"}, "databases (db_connections lists them; db_connection op add builds a missing one)"},
	{[]string{"redis-cli", "valkey-cli"}, []string{"redis_command", "redis_keys", "redis_get", "db_connections"}, "Redis"},
	{[]string{"docker", "podman", "docker-compose", "nerdctl"},
		[]string{"containers_list", "containers_exec", "containers_logs", "compose_up", "images_list", "contexts_list"}, "containers"},
	{[]string{"kubectl", "helm", "k9s", "oc"}, []string{"kube_list", "kube_describe", "kube_logs", "kube_manage"}, "kubernetes"},
	{[]string{"ssh", "scp", "sftp", "mosh"},
		[]string{"contexts_list", "containers_exec", "db_connection"}, "servers (contexts_list; contexts_manage add for a missing one)"},
	{[]string{"git"}, []string{"git_status", "git_diff", "git_log", "git_commit", "git_branch"}, "git"},
}

// shellSplit splits a command line at ; & | newlines, $( and backticks,
// so each part starts with the program it runs.
var shellSplit = regexp.MustCompile("[;&|\\n`()]|\\$\\(")

// shellPrefixes are words that run the next word as the program.
var shellPrefixes = map[string]bool{"sudo": true, "env": true, "time": true, "exec": true, "nohup": true, "command": true, "nice": true, "doas": true, "xargs": true}

// shellPrograms are the programs a command line runs (base names).
func shellPrograms(cmd string) []string {
	var out []string
	for _, part := range shellSplit.Split(cmd, -1) {
		for _, w := range strings.Fields(part) {
			if strings.HasPrefix(w, "-") || shellPrefixes[w] || (strings.Contains(w, "=") && !strings.HasPrefix(w, "=")) {
				continue // a flag of a prefix, a prefix, or VAR=value
			}
			out = append(out, path.Base(strings.Trim(w, "'\"")))
			break
		}
	}
	return out
}

// ShellOwnTool is nil when cmd may run, else the refusal that names the
// termocode tool of tools to use in its place.
func ShellOwnTool(tools []Tool, cmd string) error {
	for _, prog := range shellPrograms(cmd) {
		for _, r := range shellOwnRules {
			if !containsString(r.progs, prog) {
				continue
			}
			var have []string
			for _, n := range r.tools {
				if _, ok := FindTool(tools, n); ok {
					have = append(have, n)
				}
			}
			if len(have) == 0 {
				continue // no termocode tool for it in this turn: the shell is the way
			}
			return fmt.Errorf("%s is not run from the shell here: termocode reaches %s itself; use termocode's tool %s (also: %s). "+
				"Call it for what was asked; if it fails, report its error", prog, r.what, have[0], strings.Join(have, ", "))
		}
	}
	return nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// shellGuardArg is the argument of a shell tool that holds the command.
var shellGuardArg = map[string]string{"run_command": "command", "terminal_send": "text"}
