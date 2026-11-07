package app

import (
	"reflect"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

func TestContainerExecOptionsFromPrompts(t *testing.T) {
	cases := []struct {
		run, user, dir string
		want           containers.ExecOptions
	}{
		{"", "", "", containers.ExecOptions{}},
		{"zsh", "root", "/app", containers.ExecOptions{Shell: "zsh", User: "root", Workdir: "/app"}},
		{"  ps aux  ", "", "", containers.ExecOptions{Command: []string{"ps", "aux"}}},
	}
	for _, tc := range cases {
		if got := containerExecOptions(tc.run, tc.user, tc.dir); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q %q %q = %+v, want %+v", tc.run, tc.user, tc.dir, got, tc.want)
		}
	}
	e := containers.Engine{Binary: "docker"}
	cmd, err := e.ExecCommand("abc", containerExecOptions("ps aux", "app", "/srv"))
	if err != nil || svcShellLine(cmd.Argv) != "docker exec -it --user app --workdir /srv abc ps aux" {
		t.Fatalf("argv = %q, %v", svcShellLine(cmd.Argv), err)
	}
}

func TestContainerExecOptionsCommandIsWired(t *testing.T) {
	found := false
	for _, it := range servicesPaletteItems() {
		found = found || it.ID == "containers-exec-options"
	}
	if !found {
		t.Fatal("containers-exec-options is not in the palette")
	}
	if p := aiActionPolicies["containers-exec-options"]; p.Gate != aiGateNever {
		t.Fatalf("policy = %+v, want never (like containers-shell)", p)
	}
}
