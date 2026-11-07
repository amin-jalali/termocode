package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

func TestContainerDetailPageShowsEverySection(t *testing.T) {
	d := containers.Detail{
		ID: "abc123", Name: "web", Status: "exited", ExitCode: 137, RestartCount: 3,
		Health:   containers.DetailHealth{Status: "unhealthy", FailingStreak: 2, Log: []containers.DetailProbe{{Start: "2026-11-07T10:00:00Z", ExitCode: 1, Output: "curl failed"}}},
		EnvNames: []string{"PATH", "SECRET_TOKEN"},
		Mounts:   []containers.DetailMount{{Type: "bind", Source: "/src", Destination: "/app", RW: true}},
		Labels:   []containers.DetailLabel{{Key: "com.docker.compose.project", Value: "shop"}},
	}
	top := containers.TopTable{Titles: []string{"PID", "CMD"}, Rows: [][]string{{"1", "nginx -g daemon off;"}}}
	page := containerDetailPage(d, top, "")
	for _, want := range []string{"# web", "Exit code: 137", "Restarts: 3", "unhealthy", "curl failed", "SECRET_TOKEN", "/src", "/app", "com.docker.compose.project", "nginx -g daemon off;", "## Processes"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page has no %q:\n%s", want, page)
		}
	}
}

func TestContainerDetailPageNeverShowsEnvValues(t *testing.T) {
	d, err := containers.ParseDetail([]byte(`[{"Id":"x","Name":"/db","Config":{"Env":["PASSWORD=hunter2"]},"State":{"Status":"running"}}]`))
	if err != nil {
		t.Fatal(err)
	}
	page := containerDetailPage(d, containers.TopTable{}, "no processes")
	if strings.Contains(page, "hunter2") || !strings.Contains(page, "PASSWORD") {
		t.Fatalf("env value leaked or name missing:\n%s", page)
	}
}

func TestContainerDetailCommandIsWired(t *testing.T) {
	found := false
	for _, it := range servicesPaletteItems() {
		found = found || it.ID == "containers-detail"
	}
	if !found || !containerAITargetRequired("containers-detail") {
		t.Fatal("containers-detail is not in the palette or does not need a target")
	}
	if p := aiActionPolicies["containers-detail"]; p.Gate != aiGateAuto {
		t.Fatalf("containers-detail policy = %+v, want auto (read only)", p)
	}
}
