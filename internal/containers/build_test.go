package containers

import (
	"reflect"
	"testing"
)

func TestBuildCommandArgv(t *testing.T) {
	e := Engine{Binary: "docker"}
	cmd := e.BuildCommand(BuildOptions{
		Dockerfile: "/src/app/Dockerfile.dev",
		Tag:        "app:dev",
		Args:       []string{"GO=1.23", "MODE=two words"},
		Target:     "runtime",
		Platform:   "linux/arm64",
	})
	want := []string{"docker", "build", "-f", "/src/app/Dockerfile.dev", "-t", "app:dev",
		"--build-arg", "GO=1.23", "--build-arg", "MODE=two words",
		"--target", "runtime", "--platform", "linux/arm64", "/src/app"}
	if !reflect.DeepEqual(cmd.Argv, want) {
		t.Fatalf("argv = %#v, want %#v", cmd.Argv, want)
	}
	if cmd.Dir != "/src/app" || cmd.Label != "build: app:dev" {
		t.Fatalf("dir=%q label=%q", cmd.Dir, cmd.Label)
	}
}

func TestBuildCommandOptionalPartsAndContext(t *testing.T) {
	e := Engine{Binary: "podman"}
	cmd := e.BuildCommand(BuildOptions{Dockerfile: "/src/app/docker/Containerfile", Context: "/src/app"})
	want := []string{"podman", "build", "-f", "/src/app/docker/Containerfile", "/src/app"}
	if !reflect.DeepEqual(cmd.Argv, want) {
		t.Fatalf("argv = %#v, want %#v", cmd.Argv, want)
	}
	if cmd.Label != "build: Containerfile" {
		t.Fatalf("label = %q", cmd.Label)
	}
}

func TestIsDockerfileName(t *testing.T) {
	for name, want := range map[string]bool{
		"/a/Dockerfile": true, "/a/Containerfile": true, "/a/Dockerfile.dev": true,
		"/a/api.Dockerfile": true, "/a/api.dockerfile": true,
		"/a/dockerfile.md": false, "/a/compose.yaml": false, "/a/Dockerfiles": false,
	} {
		if got := IsDockerfileName(name); got != want {
			t.Errorf("IsDockerfileName(%q) = %v, want %v", name, got, want)
		}
	}
}
