package devcontainer

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func joinSteps(ss []Step) []string {
	var out []string
	for _, s := range ss {
		out = append(out, strings.Join(s.Argv, " "))
	}
	return out
}

func TestPlanSetupImage(t *testing.T) {
	c, err := Parse([]byte(`{
		"image": "golang:1.23",
		"workspaceFolder": "/src",
		"remoteUser": "dev",
		"forwardPorts": [8080, "db:5432"],
		"runArgs": ["--cap-drop=ALL"],
		"initializeCommand": "echo hi",
		"postCreateCommand": ["go", "mod", "download"],
		"postStartCommand": "make ${containerWorkspaceFolderBasename}",
	}`))
	if err != nil {
		t.Fatal(err)
	}
	root := "/home/u/proj"
	s, err := c.PlanSetup(root, filepath.Join(root, ".devcontainer", "devcontainer.json"), []string{"docker", "--context", "x"}, "/tmp/e.env")
	if err != nil {
		t.Fatal(err)
	}
	name := ContainerName(root)
	if s.Name != name || s.WorkspaceFolder != "/src" || s.Image != "golang:1.23" {
		t.Fatalf("setup = %+v", s)
	}
	create := joinSteps(s.Create)
	want := []string{
		"sh -c echo hi",
		"docker --context x run -d --name " + name + " --label termocode.devcontainer=/home/u/proj -v /home/u/proj:/src -w /src --env-file /tmp/e.env -p 127.0.0.1:8080:8080 --cap-drop=ALL --entrypoint  golang:1.23 sh -c while sleep 1000; do :; done",
	}
	if strings.Join(create, "\n") != strings.Join(want, "\n") {
		t.Fatalf("create =\n%s\nwant\n%s", strings.Join(create, "\n"), strings.Join(want, "\n"))
	}
	if !s.Create[0].Local || s.Create[0].Dir != root || !s.Create[0].Lifecycle || s.Create[1].Lifecycle {
		t.Fatalf("create flags = %+v", s.Create)
	}
	life := joinSteps(s.Lifecycle)
	wantLife := []string{
		"docker --context x exec -w /src -u dev " + name + " go mod download",
		"docker --context x exec -w /src -u dev " + name + " sh -c make src",
	}
	if strings.Join(life, "\n") != strings.Join(wantLife, "\n") {
		t.Fatalf("lifecycle =\n%s", strings.Join(life, "\n"))
	}
}

func TestPlanSetupBuildResolvesNextToTheFile(t *testing.T) {
	c, err := Parse([]byte(`{"build": {"dockerfile": "Dockerfile", "context": ".."}}`))
	if err != nil {
		t.Fatal(err)
	}
	root := "/home/u/proj"
	s, err := c.PlanSetup(root, root+"/.devcontainer/devcontainer.json", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if s.WorkspaceFolder != "/workspaces/proj" || s.Image != ContainerName(root)+":latest" {
		t.Fatalf("setup = %+v", s)
	}
	if got := strings.Join(s.Create[0].Argv, " "); got != "docker build -f /home/u/proj/.devcontainer/Dockerfile -t "+s.Image+" /home/u/proj" {
		t.Fatalf("build = %q", got)
	}
	if strings.Contains(strings.Join(s.Create[1].Argv, " "), "--env-file") {
		t.Fatal("no env file without containerEnv")
	}
}

func TestPlanSetupComposeAndFeaturesNeedTheCLI(t *testing.T) {
	for _, src := range []string{
		`{"dockerComposeFile": "c.yml", "service": "app"}`,
		`{"image": "x", "features": {"ghcr.io/devcontainers/features/go:1": {}}}`,
	} {
		c, err := Parse([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.PlanSetup("/p", "/p/.devcontainer.json", nil, ""); !errors.Is(err, ErrNeedsCLI) {
			t.Fatalf("%s: err = %v", src, err)
		}
	}
	c, _ := Parse([]byte(`{}`))
	if _, err := c.PlanSetup("/p", "/p/.devcontainer.json", nil, ""); !errors.Is(err, ErrNoImage) {
		t.Fatalf("empty: err = %v", err)
	}
}

func TestEnvFileTextKeepsValuesOutOfArgv(t *testing.T) {
	c, _ := Parse([]byte(`{"image": "x", "containerEnv": {"B": "2", "A": "s3cr3t"}}`))
	got, err := c.EnvFileText()
	if err != nil || got != "A=s3cr3t\nB=2\n" {
		t.Fatalf("env = %q %v", got, err)
	}
	s, _ := c.PlanSetup("/p", "/p/.devcontainer.json", nil, "/f.env")
	for _, st := range s.Create {
		if strings.Contains(strings.Join(st.Argv, " "), "s3cr3t") {
			t.Fatal("a containerEnv value is in argv")
		}
	}
	bad := Config{ContainerEnv: map[string]string{"K": "a\nb"}}
	if _, err := bad.EnvFileText(); err == nil {
		t.Fatal("a newline value must fail")
	}
}

func TestTrustRemembersTheCommandHash(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	c, _ := Parse([]byte(`{"image": "x", "postCreateCommand": "make"}`))
	if Trusted("/p", c) {
		t.Fatal("trusted before Trust")
	}
	if err := Trust("/p", c); err != nil {
		t.Fatal(err)
	}
	if !Trusted("/p", c) || Trusted("/q", c) {
		t.Fatal("trust must be per folder")
	}
	c2, _ := Parse([]byte(`{"image": "x", "postCreateCommand": "make all"}`))
	if Trusted("/p", c2) {
		t.Fatal("a changed command is trusted")
	}
}

func TestContainerNameIsStablePerFolder(t *testing.T) {
	if ContainerName("/a/b") != ContainerName("/a/b/") || ContainerName("/a/b") == ContainerName("/a/c") {
		t.Fatal("name must follow the folder")
	}
	if !strings.HasPrefix(ContainerName("/a"), "termocode-dc-") {
		t.Fatal(ContainerName("/a"))
	}
}
