package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDockerBuildTask(t *testing.T) {
	list, err := Parse([]byte(`{"tasks": [{
		"label": "image",
		"type": "docker-build",
		"dockerBuild": {"context": "./api", "dockerfile": "api/Dockerfile.dev", "tag": "api:dev",
			"target": "dev", "buildArgs": {"B": "2", "A": "1 2"}, "pull": true}
	}]}`))
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v err = %v", list, err)
	}
	got := list[0]
	want := "docker build -f api/Dockerfile.dev -t api:dev --target dev --pull --build-arg 'A=1 2' --build-arg B=2 ./api"
	if got.Command != want {
		t.Fatalf("command =\n %q\nwant\n %q", got.Command, want)
	}
	if got.Group != GroupBuild {
		t.Fatalf("group = %q, a docker-build task is a build task", got.Group)
	}
}

func TestParseDockerBuildDefaultsToTheRootContext(t *testing.T) {
	list, _ := Parse([]byte(`[{"label": "b", "type": "docker-build"}]`))
	if len(list) != 1 || list[0].Command != "docker build ." {
		t.Fatalf("list = %+v", list)
	}
}

// Env values of a docker-run task go in the task's env, never on the
// command line: docker reads "-e KEY" from its own environment.
func TestParseDockerRunTaskKeepsEnvValuesOffTheCommandLine(t *testing.T) {
	list, err := Parse([]byte(`[{
		"label": "run api",
		"type": "docker-run",
		"dockerRun": {"image": "api:dev", "containerName": "api", "ports": ["8080:80"],
			"env": {"TOKEN": "${secret:api-token}", "MODE": "dev"}, "envFiles": [".env"],
			"volumes": ["./data:/data"], "network": "dev", "command": "serve --debug"}
	}]`))
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v err = %v", list, err)
	}
	got := list[0]
	want := "docker run --rm --name api -p 8080:80 -e MODE -e TOKEN --env-file .env -v ./data:/data --network dev api:dev serve --debug"
	if got.Command != want {
		t.Fatalf("command =\n %q\nwant\n %q", got.Command, want)
	}
	if strings.Contains(got.Command, "secret") {
		t.Fatal("an env value leaked onto the command line")
	}
	if got.Env["TOKEN"] != "${secret:api-token}" || got.Env["MODE"] != "dev" {
		t.Fatalf("env = %v", got.Env)
	}
}

func TestParseDockerRunRemoveFalseKeepsTheContainer(t *testing.T) {
	list, _ := Parse([]byte(`[{"label": "r", "type": "docker-run", "dockerRun": {"image": "x", "remove": false}}]`))
	if len(list) != 1 || list[0].Command != "docker run x" {
		t.Fatalf("list = %+v", list)
	}
}

func TestParseDockerRunWithoutImageIsSkipped(t *testing.T) {
	list, _ := Parse([]byte(`[{"label": "r", "type": "docker-run", "dockerRun": {}}]`))
	if len(list) != 0 {
		t.Fatalf("a docker-run task without an image must be skipped, got %+v", list)
	}
}

func TestParseDockerComposeTask(t *testing.T) {
	list, err := Parse([]byte(`[
		{"label": "up", "type": "docker-compose", "dockerCompose": {"files": ["compose.yaml", "compose.dev.yaml"],
			"profiles": ["dev"], "detached": true, "build": true, "services": ["api", "db"]}},
		{"label": "down", "type": "docker-compose", "dockerCompose": {"down": true}}
	]`))
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v err = %v", list, err)
	}
	if want := "docker compose -f compose.yaml -f compose.dev.yaml --profile dev up -d --build api db"; list[0].Command != want {
		t.Fatalf("up =\n %q\nwant\n %q", list[0].Command, want)
	}
	if want := "docker compose down"; list[1].Command != want {
		t.Fatalf("down = %q, want %q", list[1].Command, want)
	}
}

func TestParseDockerTaskQuotesOddValues(t *testing.T) {
	list, _ := Parse([]byte(`[{"label": "b", "type": "docker-build", "dockerBuild": {"tag": "x; rm -rf /"}}]`))
	if len(list) != 1 || list[0].Command != "docker build -t 'x; rm -rf /' ." {
		t.Fatalf("list = %+v", list)
	}
}

func TestParseExplicitCommandWinsOverDockerType(t *testing.T) {
	list, _ := Parse([]byte(`[{"label": "b", "type": "docker-build", "command": "make image"}]`))
	if len(list) != 1 || list[0].Command != "make image" {
		t.Fatalf("list = %+v", list)
	}
}

func TestDetectDockerfileAndCompose(t *testing.T) {
	root := filepath.Join(t.TempDir(), "My App")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Dockerfile", "compose.yaml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]string{}
	for _, tk := range Detect(root) {
		if tk.Source == "docker" {
			got[tk.Label] = tk.Command
		}
	}
	want := map[string]string{
		"docker: build":        "docker build -t my-app .",
		"docker: compose up":   "docker compose -f compose.yaml up",
		"docker: compose down": "docker compose -f compose.yaml down",
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
}

func TestDetectNoDockerFilesNoDockerTasks(t *testing.T) {
	for _, tk := range Detect(t.TempDir()) {
		if tk.Source == "docker" {
			t.Fatalf("unexpected docker task %+v", tk)
		}
	}
}
