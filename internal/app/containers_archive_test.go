package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

func TestContainersArchivePaletteIsClassified(t *testing.T) {
	ids := []string{"containers-image-save", "containers-image-load", "containers-image-import", "containers-commit",
		"containers-export", "containers-diff", "containers-image-layers", "containers-image-used-by"}
	have := map[string]bool{}
	for _, it := range containersExtraPaletteItems() {
		have[it.ID] = true
		if _, ok := aiActionPolicies[it.ID]; !ok {
			t.Fatalf("%s has no AI policy", it.ID)
		}
	}
	for _, id := range ids {
		if !have[id] {
			t.Fatalf("%s is not in the palette", id)
		}
	}
}

func TestContainersArchivePath(t *testing.T) {
	if got := archivePath("/work", "out/web.tar"); got != "/work/out/web.tar" {
		t.Fatalf("relative: %q", got)
	}
	if got := archivePath("/work", "/tmp/web.tar"); got != "/tmp/web.tar" {
		t.Fatalf("absolute: %q", got)
	}
	if got := archivePath("/work", "  "); got != "" {
		t.Fatalf("empty: %q", got)
	}
	if got := archiveFileName("library/redis:7"); got != "library-redis-7.tar" {
		t.Fatalf("file name: %q", got)
	}
}

func TestContainersCommitLine(t *testing.T) {
	ref, msg := commitLine("web:snap  fixed the config ")
	if ref != "web:snap" || msg != "fixed the config" {
		t.Fatalf("got %q %q", ref, msg)
	}
	if ref, msg := commitLine(""); ref != "" || msg != "" {
		t.Fatalf("empty: %q %q", ref, msg)
	}
}

func TestContainersDiffText(t *testing.T) {
	text := containerDiffText("web", containers.ParseDiff("A /new\nC /etc\nD /old\n"))
	for _, want := range []string{"Changes in web", "3 paths", "+ /new", "~ /etc", "- /old"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	if got := containerDiffText("web", nil); !strings.Contains(got, "No changes") {
		t.Fatalf("empty diff: %q", got)
	}
}

func TestContainersLayerText(t *testing.T) {
	r := containers.LayerSizes([]containers.HistoryEntry{
		{ID: "a", CreatedBy: "RUN apt-get install", Size: "80MB"},
		{ID: "b", CreatedBy: "COPY . /app", Size: "20MB"},
		{ID: "c", CreatedBy: "ENV X=1", Size: "0B"},
	})
	text := layerSizesText("web:1", r)
	for _, want := range []string{"Layers of web:1", "80.0%", "RUN apt-get install", "1 empty layer"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "COPY") < strings.Index(text, "RUN apt-get") {
		t.Fatalf("largest layer must come first:\n%s", text)
	}
}

func TestContainersUsedByText(t *testing.T) {
	img := containers.Image{ID: "sha256:abc", RepoTags: []string{"web:1"}}
	cs := []containers.Container{
		{ID: "1", Names: []string{"web-a"}, Image: "web:1", State: "running"},
		{ID: "2", Names: []string{"db"}, Image: "postgres:16", State: "running"},
	}
	text := usedByText("web:1", containers.ImageUsedBy(img, cs))
	if !strings.Contains(text, "web-a") || strings.Contains(text, "db") {
		t.Fatalf("used by:\n%s", text)
	}
	if got := usedByText("web:1", nil); !strings.Contains(got, "No container uses") {
		t.Fatalf("unused: %q", got)
	}
}
