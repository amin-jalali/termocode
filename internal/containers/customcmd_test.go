package containers

import (
	"reflect"
	"strings"
	"testing"
)

func TestCustomCommandExpandsPlaceholdersPerArg(t *testing.T) {
	c := CustomCommand{Name: "Tail nginx log", View: "containers", Key: "ctrl+l",
		Argv: []string{"docker", "exec", "{{id}}", "tail", "-n", "50", "/var/log/{{name}}.log"}}
	got, err := c.Expand(map[string]string{"id": "abc123", "name": "web; rm -rf /"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"docker", "exec", "abc123", "tail", "-n", "50", "/var/log/web; rm -rf /.log"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %q", got)
	}
}

func TestCustomCommandRefusesBadExpansion(t *testing.T) {
	c := CustomCommand{Name: "x", View: "pods", Argv: []string{"kubectl", "describe", "pod", "{{name}}", "{{nope}}"}}
	if _, err := c.Expand(map[string]string{"name": "api"}); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown placeholder: err = %v", err)
	}
	c = CustomCommand{Name: "x", View: "pods", Argv: []string{"kubectl", "describe", "pod", "{{name}}"}}
	if _, err := c.Expand(map[string]string{"name": "--kubeconfig=/x"}); err == nil {
		t.Fatal("a value that turns into a flag must be refused")
	}
	if _, err := c.Expand(map[string]string{"name": ""}); err == nil {
		t.Fatal("an empty value must be refused")
	}
}

func TestValidateCustomCommands(t *testing.T) {
	ok := []CustomCommand{
		{Name: "a", View: "containers", Key: "ctrl+t", Argv: []string{"docker", "top", "{{id}}"}},
		{Name: "b", View: "pods", Key: "ctrl+t", Argv: []string{"kubectl", "top", "pod", "{{name}}"}},
	}
	if err := ValidateCustomCommands(ok); err != nil {
		t.Fatalf("valid set: %v", err)
	}
	bad := map[string][]CustomCommand{
		"no name":      {{View: "containers", Argv: []string{"docker"}}},
		"no argv":      {{Name: "a", View: "containers"}},
		"unknown view": {{Name: "a", View: "galaxy", Argv: []string{"docker"}}},
		"argv[0] tmpl": {{Name: "a", View: "containers", Argv: []string{"{{name}}"}}},
		"same key":     {ok[0], {Name: "c", View: "containers", Key: "ctrl+t", Argv: []string{"docker"}}},
		"reserved key": {{Name: "a", View: "containers", Key: "enter", Argv: []string{"docker"}}},
	}
	for name, set := range bad {
		if err := ValidateCustomCommands(set); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestCustomCommandsForView(t *testing.T) {
	all := []CustomCommand{
		{Name: "a", View: "containers", Argv: []string{"docker"}},
		{Name: "b", View: "pods", Argv: []string{"kubectl"}},
		{Name: "c", View: "containers", Argv: []string{"docker"}},
	}
	got := CustomCommandsFor(all, "containers")
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "c" {
		t.Fatalf("got %+v", got)
	}
}

func TestCustomCommandPreviewQuotesArgs(t *testing.T) {
	if got := CommandPreview([]string{"docker", "exec", "web 1", "sh"}); got != "docker exec 'web 1' sh" {
		t.Fatalf("preview = %q", got)
	}
}
