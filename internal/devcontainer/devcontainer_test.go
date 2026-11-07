package devcontainer

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const sample = `{
  // a comment
  "name": "api",
  "image": "mcr.example/go:1",
  "workspaceFolder": "/workspaces/api",
  "remoteUser": "vscode",
  "containerEnv": {"A": "1"},
  "remoteEnv": {"B": "2"},
  "forwardPorts": [8080, "db:5432"],
  "runArgs": ["--cap-drop=ALL"],
  "mounts": ["source=x,target=/x,type=volume", {"source": "y", "target": "/y", "type": "bind"}],
  "features": {"ghcr.io/devcontainers/features/node:1": {}},
  "initializeCommand": "echo local",
  "onCreateCommand": ["make", "setup"],
  "postCreateCommand": {"deps": "go mod download", "tools": ["go", "install", "x"]},
  "postStartCommand": "",
}`

func TestParseReadsFields(t *testing.T) {
	c, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "api" || c.Image != "mcr.example/go:1" || c.WorkspaceFolder != "/workspaces/api" || c.RemoteUser != "vscode" {
		t.Fatalf("basic fields: %+v", c)
	}
	if c.ContainerEnv["A"] != "1" || c.RemoteEnv["B"] != "2" {
		t.Fatalf("env: %+v %+v", c.ContainerEnv, c.RemoteEnv)
	}
	if !reflect.DeepEqual(c.ForwardPorts, []string{"8080", "db:5432"}) {
		t.Fatalf("ports: %v", c.ForwardPorts)
	}
	if !reflect.DeepEqual(c.RunArgs, []string{"--cap-drop=ALL"}) {
		t.Fatalf("runArgs: %v", c.RunArgs)
	}
	if !reflect.DeepEqual(c.Mounts, []string{"source=x,target=/x,type=volume", "source=y,target=/y,type=bind"}) {
		t.Fatalf("mounts: %v", c.Mounts)
	}
	if len(c.Features) != 1 {
		t.Fatalf("features: %v", c.Features)
	}
	if c.Kind() != KindImage {
		t.Fatalf("kind = %v", c.Kind())
	}
	if !c.NeedsCLI() {
		t.Fatal("features need the devcontainer CLI")
	}
}

func TestParseBuildAndCompose(t *testing.T) {
	b, err := Parse([]byte(`{"build": {"dockerfile": "Dockerfile", "context": ".."}}`))
	if err != nil {
		t.Fatal(err)
	}
	if b.Kind() != KindBuild || b.Dockerfile != "Dockerfile" || b.BuildContext != ".." || b.NeedsCLI() {
		t.Fatalf("build: %+v", b)
	}
	old, err := Parse([]byte(`{"dockerFile": "D"}`))
	if err != nil || old.Kind() != KindBuild || old.Dockerfile != "D" {
		t.Fatalf("old dockerFile key: %+v %v", old, err)
	}
	c, err := Parse([]byte(`{"dockerComposeFile": ["a.yml", "b.yml"], "service": "app"}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind() != KindCompose || !reflect.DeepEqual(c.ComposeFiles, []string{"a.yml", "b.yml"}) || c.Service != "app" || !c.NeedsCLI() {
		t.Fatalf("compose: %+v", c)
	}
	one, err := Parse([]byte(`{"dockerComposeFile": "c.yml", "service": "app"}`))
	if err != nil || !reflect.DeepEqual(one.ComposeFiles, []string{"c.yml"}) {
		t.Fatalf("compose string: %+v %v", one, err)
	}
	none, err := Parse([]byte(`{}`))
	if err != nil || none.Kind() != KindNone {
		t.Fatalf("empty: %+v %v", none, err)
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	for _, src := range []string{`{`, `[]`, `{"forwardPorts": [true]}`, `{"onCreateCommand": 5}`, `{"mounts": [3]}`} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("Parse(%s) = nil error", src)
		}
	}
}

func TestPlanListsEveryCommandInOrderAndWhere(t *testing.T) {
	c, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	want := []Command{
		{Stage: "initializeCommand", Shell: "echo local", Local: true},
		{Stage: "onCreateCommand", Args: []string{"make", "setup"}},
		{Stage: "postCreateCommand", Name: "deps", Shell: "go mod download"},
		{Stage: "postCreateCommand", Name: "tools", Args: []string{"go", "install", "x"}},
	}
	if got := c.Plan(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Plan =\n%+v\nwant\n%+v", got, want)
	}
	if got := want[1].String(); got != "make setup" {
		t.Fatalf("String = %q", got)
	}
	if got := want[0].Where(); got != "this computer" {
		t.Fatalf("Where local = %q", got)
	}
	if got := want[1].Where(); got != "the new container" {
		t.Fatalf("Where container = %q", got)
	}
}

func TestTrustHashChangesOnlyWithCommands(t *testing.T) {
	a, _ := Parse([]byte(`{"image": "x", "postCreateCommand": "make"}`))
	b, _ := Parse([]byte(`{"image": "y", /* c */ "postCreateCommand": "make"}`))
	c, _ := Parse([]byte(`{"image": "x", "postCreateCommand": "make all"}`))
	if a.TrustHash() == "" || a.TrustHash() != b.TrustHash() {
		t.Fatal("hash must depend on the commands only")
	}
	if a.TrustHash() == c.TrustHash() {
		t.Fatal("a changed command must change the hash")
	}
}

func TestFindPrefersFolderFile(t *testing.T) {
	root := t.TempDir()
	if Find(root) != "" {
		t.Fatal("empty dir has no file")
	}
	top := filepath.Join(root, ".devcontainer.json")
	if err := os.WriteFile(top, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if Find(root) != top {
		t.Fatalf("Find = %q", Find(root))
	}
	if err := os.MkdirAll(filepath.Join(root, ".devcontainer"), 0o700); err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(root, ".devcontainer", "devcontainer.json")
	if err := os.WriteFile(in, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if Find(root) != in {
		t.Fatalf("Find = %q, want %q", Find(root), in)
	}
}
