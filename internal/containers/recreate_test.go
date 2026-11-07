package containers

import (
	"slices"
	"strings"
	"testing"
)

func TestRecreatePlanPullsRemovesAndRunsTheSameSettings(t *testing.T) {
	e := &Engine{Binary: "docker"}
	o := RunOptions{Image: "nginx:1.27", Name: "web", Detach: true, Ports: []string{"8080:80"}, Env: map[string]string{"TOKEN": "s3cret"}}
	steps, env, err := e.RecreatePlan("c5", o)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 3 || !slices.Equal(steps[0], []string{"docker", "pull", "nginx:1.27"}) ||
		!slices.Equal(steps[1], []string{"docker", "rm", "-f", "c5"}) || steps[2][1] != "run" {
		t.Fatalf("steps %q", steps)
	}
	if strings.Contains(strings.Join(steps[2], " "), "s3cret") || env["TOKEN"] != "s3cret" {
		t.Fatalf("env value must stay out of argv: %q %v", steps[2], env)
	}
	if _, _, err := e.RecreatePlan("-x", o); err == nil {
		t.Fatal("a flag as the container must be refused")
	}
}
