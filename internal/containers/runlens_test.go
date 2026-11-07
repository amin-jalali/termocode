package containers

import (
	"strings"
	"testing"
)

func TestRunTargetsDockerfileStages(t *testing.T) {
	lines := []string{
		"# syntax=docker/dockerfile:1",
		"FROM --platform=$BUILDPLATFORM golang:1.23 AS build",
		"RUN go build",
		"from alpine:3 as runtime",
		"COPY --from=build /out /app",
	}
	ts := RunTargets("/w/Dockerfile", lines)
	if len(ts) != 2 || ts[0].Line != 2 || ts[0].Stage != "build" || ts[0].Last || ts[1].Line != 4 || ts[1].Stage != "runtime" || !ts[1].Last {
		t.Fatalf("targets = %+v", ts)
	}
	if got, ok := RunTargetAt(ts, 3); !ok || got.Stage != "build" {
		t.Fatalf("line 3 → %+v %v, want the build stage", got, ok)
	}
	if _, ok := RunTargetAt(ts, 1); ok {
		t.Fatal("a line above the first FROM found a target")
	}
	if _, ok := RunTargetOn(ts, 3); ok {
		t.Fatal("RunTargetOn matched a line without a mark")
	}
}

func TestRunTargetsComposeServicesOnly(t *testing.T) {
	lines := []string{
		"name: shop",
		"services:",
		"  # the api",
		"  api:",
		"    build: .",
		"    environment:",
		"      KEY: v",
		"  db:",
		"    image: postgres:16",
		"volumes:",
		"  data:",
	}
	ts := RunTargets("/w/compose.yaml", lines)
	if len(ts) != 2 || ts[0].Service != "api" || ts[0].Line != 4 || ts[1].Service != "db" || ts[1].Line != 8 {
		t.Fatalf("targets = %+v, want api and db only", ts)
	}
	if RunTargets("/w/main.go", lines) != nil {
		t.Fatal("a Go file has run targets")
	}
}

func TestIsComposeFileName(t *testing.T) {
	for _, p := range []string{"compose.yaml", "/a/docker-compose.yml", "compose.dev.yaml", "docker-compose.override.yml"} {
		if !IsComposeFileName(p) {
			t.Errorf("%s is a compose file", p)
		}
	}
	for _, p := range []string{"compose.json", "mycompose.yaml", "values.yaml"} {
		if IsComposeFileName(p) {
			t.Errorf("%s is not a compose file", p)
		}
	}
}

func TestRunTargetCommandBuildsStageOrStartsService(t *testing.T) {
	e := Engine{Binary: "docker"}
	c := e.RunTargetCommand("/w/Dockerfile", RunTarget{Kind: RunBuild, Stage: "build"}, "w:latest")
	line := strings.Join(c.Argv, " ")
	if !strings.Contains(line, "--target build") || !strings.Contains(line, "-t w:build") {
		t.Fatalf("stage build = %q", line)
	}
	c = e.RunTargetCommand("/w/Dockerfile", RunTarget{Kind: RunBuild, Stage: "runtime", Last: true}, "w:latest")
	if line := strings.Join(c.Argv, " "); strings.Contains(line, "--target") || !strings.Contains(line, "-t w:latest") {
		t.Fatalf("last stage build = %q", line)
	}
	c = e.RunTargetCommand("/w/compose.yaml", RunTarget{Kind: RunService, Service: "db"}, "")
	if got := strings.Join(c.Argv, " "); got != "docker compose --project-directory /w -f /w/compose.yaml up db" {
		t.Fatalf("service = %q", got)
	}
}
