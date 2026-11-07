package devcontainer

import (
	"errors"
	"strings"
	"testing"
)

func TestPlanCLISetupComposeRunsTheCLIOnce(t *testing.T) {
	c, err := Parse([]byte(`{"dockerComposeFile": "c.yml", "service": "app", "workspaceFolder": "/src", "postCreateCommand": "make"}`))
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.PlanCLISetup("/p", "/usr/bin/devcontainer", []string{"docker"})
	if err != nil {
		t.Fatal(err)
	}
	if !s.CLI || len(s.Create) != 1 || len(s.Lifecycle) != 0 {
		t.Fatalf("setup = %+v", s)
	}
	got := strings.Join(s.Create[0].Argv, " ")
	want := "/usr/bin/devcontainer up --workspace-folder /p --id-label " + Label + "=/p --docker-path docker"
	if got != want || !s.Create[0].Up || s.Create[0].Dir != "/p" {
		t.Fatalf("argv = %q (up %v), want %q", got, s.Create[0].Up, want)
	}
	if s.WorkspaceFolder != "/src" || s.Name != "" {
		t.Fatalf("ws %q name %q", s.WorkspaceFolder, s.Name)
	}
}

func TestPlanCLISetupFeaturesDefaultFolderAndContexts(t *testing.T) {
	c, _ := Parse([]byte(`{"image": "x", "features": {"ghcr.io/devcontainers/features/go:1": {}}}`))
	s, err := c.PlanCLISetup("/home/a/proj", "devcontainer", []string{"docker", "--context", "far"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.Create[0].Argv, " "); !strings.HasPrefix(got, "env DOCKER_CONTEXT=far devcontainer up ") {
		t.Fatalf("argv = %q", got)
	}
	if s.WorkspaceFolder != "/workspaces/proj" {
		t.Fatalf("ws = %q", s.WorkspaceFolder)
	}
	s, _ = c.PlanCLISetup("/home/a/proj", "devcontainer", []string{"podman", "--connection", "m"})
	got := strings.Join(s.Create[0].Argv, " ")
	if !strings.HasPrefix(got, "env CONTAINER_CONNECTION=m devcontainer up ") || !strings.HasSuffix(got, "--docker-path podman") {
		t.Fatalf("podman argv = %q", got)
	}
	empty, _ := Parse([]byte(`{}`))
	if _, err := empty.PlanCLISetup("/p", "devcontainer", nil); !errors.Is(err, ErrNoImage) {
		t.Fatalf("empty: %v", err)
	}
}

func TestFeatureIDsAreSorted(t *testing.T) {
	c, _ := Parse([]byte(`{"image": "x", "features": {"b:1": {}, "a:2": {}}}`))
	if got := strings.Join(c.FeatureIDs(), ","); got != "a:2,b:1" {
		t.Fatalf("ids = %q", got)
	}
}

func TestParseUpResult(t *testing.T) {
	out := "[1 ms] Start: Run\nsome log\n" + `{"outcome":"success","containerId":"abc123","remoteUser":"vscode","remoteWorkspaceFolder":"/workspaces/p"}` + "\n"
	r, err := ParseUpResult(out)
	if err != nil || r.ContainerID != "abc123" || r.RemoteWorkspaceFolder != "/workspaces/p" {
		t.Fatalf("r = %+v err %v", r, err)
	}
	if _, err := ParseUpResult(`{"outcome":"error","message":"boom","description":"no image"}`); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error outcome: %v", err)
	}
	if _, err := ParseUpResult("just logs\n"); err == nil {
		t.Fatal("no JSON line must be an error")
	}
}

func TestOfferNeverIsPerFolder(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if OfferMuted("/p") {
		t.Fatal("muted before")
	}
	if err := MuteOffer("/p/"); err != nil {
		t.Fatal(err)
	}
	if !OfferMuted("/p") || OfferMuted("/q") {
		t.Fatal("mute must be for /p only")
	}
}

func TestTrustHashCoversFeatures(t *testing.T) {
	a, _ := Parse([]byte(`{"image": "x", "postCreateCommand": "make"}`))
	b, _ := Parse([]byte(`{"image": "y", "postCreateCommand": "make", "features": {"f:1": {}}}`))
	c, _ := Parse([]byte(`{"image": "y", "postCreateCommand": "make", "features": {"f:1": {"version": "2"}}}`))
	if a.TrustHash() == b.TrustHash() || b.TrustHash() == c.TrustHash() {
		t.Fatal("a feature or its options must change the trust hash")
	}
}
