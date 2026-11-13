package main

import (
	"strings"
	"testing"
)

func TestMCPAdminUsageErrors(t *testing.T) {
	cwd := adminEnv(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"add", "--scope"}, "needs a value"},
		{[]string{"add", "--scope", "galaxy", "x", "--", "y"}, "--scope is user or project"},
		{[]string{"add", "--env", "NOEQUALS", "x", "--", "y"}, "--env needs NAME=VALUE"},
		{[]string{"add", "--npm", "a", "--pip", "b"}, "use one of"},
		{[]string{"add", "--share", "everyone", "x", "--", "y"}, "--share can only be user"},
		{[]string{"add", "--bogus"}, "unknown option --bogus"},
		{[]string{"add", "--env", "A=1", "name", "https://example.com/mcp"}, "are for a local server"},
		{[]string{"add"}, "give a name and a command"},
		{[]string{"add", "a", "b", "c"}, "give a name and a command"},
		{[]string{"add", "--"}, "-- needs a command"},
		{[]string{"add", "a", "b", "--", "cmd"}, "put the command after --"},
		{[]string{"add", "--scope", "project", "--share", "user", "x", "--", "cmd"}, "--share user is only for your own"},
		{[]string{"add", "--scope", "project", "--dir", "/definitely/not/here", "x", "--", "cmd"}, "is not a folder"},
		{[]string{"get"}, "give one server name"},
		{[]string{"get", "a", "b"}, "give one server name"},
		{[]string{"remove"}, "give one server name"},
		{[]string{"add", "name", "{\"mcpServers\":{}}"}, ""},
	} {
		code, _, errs := runAdmin(t, cwd, "", c.args...)
		if code == 0 || !strings.Contains(errs, c.want) {
			t.Errorf("%v: %d %q", c.args, code, errs)
		}
	}
	if code, out, _ := runAdmin(t, cwd, "", "add", "--help"); code != 0 || !strings.Contains(out, "termocode mcp") {
		t.Errorf("help: %d %q", code, out)
	}
	if code, out, _ := runAdmin(t, cwd, "", "list"); code != 0 || !strings.Contains(out, "No MCP servers") {
		t.Errorf("empty list: %d %q", code, out)
	}
}

func TestMCPAdminSecretProblemsAndRemoveScopes(t *testing.T) {
	cwd := adminEnv(t)
	// An empty secret value is refused, so is a value that looks like text.
	if code, _, errs := runAdmin(t, cwd, "\n", "add", "--secret", "API_KEY", "s1", "--", "srv"); code == 0 || !strings.Contains(errs, "is empty") {
		t.Errorf("empty secret: %d %q", code, errs)
	}
	// The same name in both files needs --scope on remove.
	if code, _, errs := runAdmin(t, cwd, "", "add", "dup", "--", "one"); code != 0 {
		t.Fatal(errs)
	}
	if code, _, errs := runAdmin(t, cwd, "", "add", "--scope", "project", "dup", "--", "two"); code != 0 {
		t.Fatal(errs)
	}
	if code, _, errs := runAdmin(t, cwd, "", "remove", "dup"); code == 0 || !strings.Contains(errs, "both files") {
		t.Errorf("remove in both: %d %q", code, errs)
	}
	if code, _, errs := runAdmin(t, cwd, "", "remove", "dup", "--scope", "project"); code != 0 {
		t.Errorf("remove project: %d %q", code, errs)
	}
	// get shows a URL server and a command server with args and env.
	if code, _, errs := runAdmin(t, cwd, "", "add", "web", "https://example.com/mcp?token=abc123def456"); code != 0 {
		t.Fatal(errs)
	}
	if code, out, _ := runAdmin(t, cwd, "", "get", "web"); code != 0 || !strings.Contains(out, "URL:") || strings.Contains(out, "abc123def456") {
		t.Errorf("get url: %d %q", code, out)
	}
	if code, out, _ := runAdmin(t, cwd, "", "add", "args", "--", "srv", "--flag", "v"); code != 0 {
		t.Fatal(out)
	}
	if code, out, _ := runAdmin(t, cwd, "", "get", "args"); code != 0 || !strings.Contains(out, "Args:") {
		t.Errorf("get args: %d %q", code, out)
	}
	if code, out, _ := runAdmin(t, cwd, "", "list"); code != 0 || !strings.Contains(out, "web") {
		t.Errorf("list: %d %q", code, out)
	}
}

func TestMCPTopRoutesAdminCommands(t *testing.T) {
	adminEnv(t)
	if code := runMCPTop([]string{"list"}); code != 0 {
		t.Fatalf("mcp list: %d", code)
	}
}
