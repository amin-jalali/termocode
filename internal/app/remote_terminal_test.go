package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// RC-5 (phase 4a): a terminal tab of a container workspace runs the
// shell inside the container (`exec -it -w <dir> <c> sh …`), never the
// local $SHELL, and without the local shell integration.
func TestTermShellSpecContainerRunsInContainer(t *testing.T) {
	withConfigDir(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	m, _, _, _ := newContainerModel(t)
	var sp termShellSpec
	if err := json.Unmarshal([]byte(m.termShellSpecFor("/bin/bash", "")), &sp); err != nil {
		t.Fatal(err)
	}
	if sp.Argv != nil || sp.Env != nil {
		t.Errorf("container tab must not use the local shell integration: %+v", sp)
	}
	for _, want := range []string{"'exec'", "'-it'", "'-w'", "'/app'", "'api-1'", "'sh'"} {
		if !strings.Contains(sp.Plain, want) {
			t.Errorf("plain %q lacks %s", sp.Plain, want)
		}
	}
	if strings.HasPrefix(sp.Plain, "/bin/bash") {
		t.Errorf("plain runs the local shell: %q", sp.Plain)
	}
	if sp.Tab == "" {
		t.Error("container tab needs its control id")
	}

	// A folder under the root starts there inside the container.
	if err := json.Unmarshal([]byte(m.termShellSpecFor("/bin/bash", "/app/pkg")), &sp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sp.Plain, "'/app/pkg'") {
		t.Errorf("dir not passed: %q", sp.Plain)
	}
	// The local termopen never gets a container path as its cwd.
	if wd := m.termLocalCwd("/app/pkg"); wd != "" {
		t.Errorf("local cwd %q, want none", wd)
	}
}

// A local workspace keeps the old spec and its cwd.
func TestTermShellSpecLocalUnchanged(t *testing.T) {
	withConfigDir(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	var m Model
	var a, b termShellSpec
	if json.Unmarshal([]byte(m.termShellSpecFor("/bin/sh", "/tmp")), &a) != nil || json.Unmarshal([]byte(termShellSpecJSON("/bin/sh")), &b) != nil {
		t.Fatal("bad json")
	}
	if a.Plain != "/bin/sh" || a.Plain != b.Plain {
		t.Errorf("local plain %q", a.Plain)
	}
	if wd := m.termLocalCwd("/tmp"); wd != "/tmp" {
		t.Errorf("local cwd %q", wd)
	}
}
