package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/containers"
)

func TestContainersBuildsPaletteIsClassified(t *testing.T) {
	want := map[string]bool{"containers-builds": false, "containers-build-detail": false, "containers-build-cache-prune": false, "containers-build-check": false}
	for _, it := range containersExtraPaletteItems() {
		if _, ok := want[it.ID]; ok {
			want[it.ID] = true
		}
		if _, ok := aiActionPolicies[it.ID]; !ok {
			t.Fatalf("%s has no AI policy", it.ID)
		}
	}
	for id, ok := range want {
		if !ok {
			t.Fatalf("%s is not in the palette", id)
		}
	}
	if aiActionPolicies["containers-build-cache-prune"].Gate != aiGateNever {
		t.Fatal("the AI must not prune the build cache")
	}
}

func TestContainersBuildsPageText(t *testing.T) {
	at := time.Date(2026, 11, 7, 8, 15, 0, 0, time.UTC)
	hist := []containers.BuildRecord{
		{Ref: "abc123", Name: "web", Status: "Completed", Created: at, Duration: "12s", Steps: 10, Done: 10, Cached: 7},
		{Ref: "def456", Name: "api", Status: "Error", Created: at.Add(time.Minute), Error: "exit code 1"},
	}
	builders := []containers.Builder{{Name: "default", Driver: "docker", Current: true, Status: "running", Platforms: []string{"linux/amd64", "linux/arm64"}}}
	cache := containers.BuildCacheUsage{Count: 40, Active: 2, Size: "1.2GB", Reclaimable: "900MB"}
	text := buildsPageText(hist, nil, builders, nil, cache, nil, time.UTC)
	for _, want := range []string{"Build history", "abc123", "web", "7/10 cached", "def456", "failed: exit code 1", "Builders", "* default", "linux/amd64, linux/arm64", "Build cache", "40 entries", "900MB"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Index(text, "def456") > strings.Index(text, "abc123") {
		t.Fatalf("newest build must come first:\n%s", text)
	}
	if got := buildsPageText(nil, errString("needs buildx"), nil, nil, cache, nil, time.UTC); !strings.Contains(got, "needs buildx") {
		t.Fatalf("history error not shown:\n%s", got)
	}
}

func TestContainersBuildCachePruneCard(t *testing.T) {
	body := buildCachePruneBody(containers.BuildCacheUsage{Count: 3, Size: "10MB", Reclaimable: "8MB"}, "docker builder prune --force")
	for _, want := range []string{"3 entries", "8MB", "docker builder prune --force"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %q", want, body)
		}
	}
}
