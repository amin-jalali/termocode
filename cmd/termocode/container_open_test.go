//go:build unix

package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
	"github.com/amin-jalali/termocode/internal/workspace"
)

func TestParseContainerOpen(t *testing.T) {
	cases := []struct {
		args []string
		want containerOpen
		rest []string
		err  string
	}{
		{args: []string{"--no-x"}, rest: []string{"--no-x"}},
		{args: []string{"--container", "api-1", "/app"}, want: containerOpen{Name: "api-1", Root: "/app"}},
		{args: []string{"--container=api-1", "--context=prod-ssh", "/srv/app/"}, want: containerOpen{Name: "api-1", Context: "prod-ssh", Root: "/srv/app"}},
		{args: []string{"--context", "prod-ssh", "--container", "api-1", "/app"}, want: containerOpen{Name: "api-1", Context: "prod-ssh", Root: "/app"}},
		{args: []string{"--container", "api-1"}, err: "give the folder inside the container"},
		{args: []string{"--container", "api-1", "app"}, err: "must be absolute"},
		{args: []string{"--container"}, err: "--container needs a container name"},
		{args: []string{"--container", "-x", "/app"}, err: "--container needs a container name"},
		{args: []string{"--context", "prod", "/app"}, err: "--context only works with --container"},
		{args: []string{"--container", "api-1", "/app", "/other"}, err: "only one folder"},
	}
	for _, c := range cases {
		got, rest, err := parseContainerOpen(c.args)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%q: err = %v, want %q", c.args, err, c.err)
			}
			continue
		}
		if err != nil || got != c.want || !reflect.DeepEqual(rest, c.rest) {
			t.Errorf("%q: got %+v rest %q err %v", c.args, got, rest, err)
		}
	}
}

func TestRunContainerOpensTheContainerWorkspace(t *testing.T) {
	covMainEnv(t)
	d := fakedocker.New()
	c := d.Add(t, "api-1")
	c.Mkdir(t, "/app")
	oldX, oldSet := containerEngine, setWorkspace
	t.Cleanup(func() { containerEngine, setWorkspace = oldX, oldSet })
	containerEngine = d
	var got workspace.Workspace
	setWorkspace = func(ws workspace.Workspace) { got = ws }

	r := covCall(t, []string{"--container", "api-1", "--context", "prod-ssh", "/app"}, nil)
	if r.code != 0 || r.tuiStart != 1 || len(r.clients) != 0 {
		t.Fatalf("run = %+v", r)
	}
	if got.Kind != workspace.KindContainer || got.Root != "/app" || got.ID != "container:prod-ssh/api-1:/app" || got.Host == nil {
		t.Fatalf("workspace = %+v", got)
	}
	for _, call := range d.Calls() {
		if call.Context != "prod-ssh" {
			t.Fatalf("engine call without the context: %+v", call)
		}
	}
}

func TestRunContainerStoppedSaysSoAndStartsNothing(t *testing.T) {
	covMainEnv(t)
	d := fakedocker.New()
	c := d.Add(t, "api-1")
	c.Mkdir(t, "/app")
	c.Stop()
	oldX, oldSet := containerEngine, setWorkspace
	t.Cleanup(func() { containerEngine, setWorkspace = oldX, oldSet })
	containerEngine = d
	set := false
	setWorkspace = func(workspace.Workspace) { set = true }

	r := covCall(t, []string{"--container", "api-1", "/app"}, nil)
	if r.code != 1 || r.tuiStart != 0 || set {
		t.Fatalf("run = %+v, set %v", r, set)
	}
	if !strings.Contains(r.stderr, "termocode: cannot open api-1:/app: the container is not running") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestRunContainerBadArgsSaysWhy(t *testing.T) {
	covMainEnv(t)
	r := covCall(t, []string{"--container", "api-1"}, nil)
	if r.code != 2 || r.tuiStart != 0 || !strings.Contains(r.stderr, "give the folder inside the container") {
		t.Fatalf("run = %+v", r)
	}
}

func TestUsageListsContainer(t *testing.T) {
	covMainEnv(t)
	r := covCall(t, []string{"--help"}, nil)
	if !strings.Contains(r.stdout, "termocode --container name [--context c] /folder") {
		t.Fatalf("usage = %q", r.stdout)
	}
}
