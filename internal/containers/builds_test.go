package containers

import (
	"strings"
	"testing"
)

func TestParseBuildHistory(t *testing.T) {
	data := `{"ref":"abc123","name":"web","status":"Completed","created_at":"2026-11-01T10:00:00Z","total_steps":12,"completed_steps":12,"cached_steps":9,"duration":"4.2s"}
{"Ref":"def456","Name":"api","Status":"Error","CreatedAt":"2026-11-02T10:00:00Z","TotalSteps":3,"CompletedSteps":1,"CachedSteps":0,"Error":"failed to solve"}
`
	got, err := ParseBuildHistory([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d records", len(got))
	}
	a, b := got[0], got[1]
	if a.Ref != "abc123" || a.Name != "web" || a.Status != "Completed" || a.Steps != 12 || a.Cached != 9 || a.Created.IsZero() || a.Duration != "4.2s" {
		t.Fatalf("first = %+v", a)
	}
	if b.Ref != "def456" || !b.Failed() || b.Error != "failed to solve" || b.Done != 1 {
		t.Fatalf("second = %+v", b)
	}
	if a.Failed() {
		t.Fatal("completed build reported as failed")
	}
}

func TestParseBuildHistoryArray(t *testing.T) {
	got, err := ParseBuildHistory([]byte(`[{"ref":"r1","name":"x","status":"Completed"},{"ref":"r2","name":"y","status":"Running"}]`))
	if err != nil || len(got) != 2 || got[1].Ref != "r2" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestParseBuilders(t *testing.T) {
	data := `{"Name":"default","Driver":"docker","Current":false,"Nodes":[{"Name":"default","Status":"running","Platforms":["linux/amd64","linux/arm64"]}]}
{"Name":"multi","Driver":"docker-container","Current":true,"Nodes":[{"Name":"multi0","Status":"inactive"}]}
`
	got, err := ParseBuilders([]byte(data))
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got[0].Name != "default" || got[0].Status != "running" || strings.Join(got[0].Platforms, ",") != "linux/amd64,linux/arm64" || got[0].Current {
		t.Fatalf("first = %+v", got[0])
	}
	if !got[1].Current || got[1].Driver != "docker-container" || got[1].Status != "inactive" {
		t.Fatalf("second = %+v", got[1])
	}
}

func TestParseBuildCacheUsage(t *testing.T) {
	data := `{"Type":"Images","TotalCount":"5","Active":"2","Size":"1.2GB","Reclaimable":"800MB (66%)"}
{"Type":"Build Cache","TotalCount":"41","Active":"0","Size":"2.5GB","Reclaimable":"2.5GB"}
`
	u, err := ParseBuildCacheUsage([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if u.Count != 41 || u.Size != "2.5GB" || u.Reclaimable != "2.5GB" {
		t.Fatalf("usage = %+v", u)
	}
	if _, err := ParseBuildCacheUsage([]byte(`{"Type":"Images","TotalCount":"5"}`)); err == nil {
		t.Fatal("no build cache row: want an error")
	}
}

func TestBuildArgv(t *testing.T) {
	d := &Engine{Binary: "docker", Kind: "docker"}
	p := &Engine{Binary: "podman", Kind: "podman"}
	if got := strings.Join(d.BuildHistoryArgs(), " "); got != "buildx history ls --format json" {
		t.Fatalf("history = %q", got)
	}
	if got := strings.Join(d.BuildersArgs(), " "); got != "buildx ls --format json" {
		t.Fatalf("builders = %q", got)
	}
	if got := strings.Join(d.BuildCachePruneArgs(false), " "); got != "builder prune --force" {
		t.Fatalf("prune = %q", got)
	}
	if got := strings.Join(d.BuildCachePruneArgs(true), " "); got != "builder prune --force --all" {
		t.Fatalf("prune all = %q", got)
	}
	if got := strings.Join(p.BuildCachePruneArgs(true), " "); got != "image prune --build-cache --force" {
		t.Fatalf("podman prune = %q", got)
	}
	if !d.SupportsBuildHistory() || p.SupportsBuildHistory() {
		t.Fatal("build history is docker buildx only")
	}
}

func TestBuildDetailAndCheckCommands(t *testing.T) {
	e := &Engine{Binary: "docker", Kind: "docker"}
	c, err := e.BuildDetailCommand("abc123")
	if err != nil || strings.Join(c.Argv, " ") != "docker buildx history inspect abc123" {
		t.Fatalf("detail = %+v, %v", c, err)
	}
	if _, err := e.BuildDetailCommand("--help"); err == nil {
		t.Fatal("a ref that looks like a flag must be refused")
	}
	if _, err := e.BuildDetailCommand(""); err == nil {
		t.Fatal("empty ref must be refused")
	}
	c = e.BuildCheckCommand("/work/app", "Dockerfile.prod")
	if strings.Join(c.Argv, " ") != "docker build --check -f Dockerfile.prod ." || c.Dir != "/work/app" {
		t.Fatalf("check = %+v", c)
	}
	c = e.BuildCheckCommand("/work/app", "")
	if strings.Join(c.Argv, " ") != "docker build --check ." {
		t.Fatalf("check default = %+v", c)
	}
}
