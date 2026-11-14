package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func writeTestDockerfile(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(p, []byte("FROM alpine:3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBuildImageAsksTagArgsTargetPlatformThenCancelFirstCard(t *testing.T) {
	m := svcTestModel(svcHostState(), 120, 40)
	p := writeTestDockerfile(t)
	if cmd := m.askImageBuild(p); cmd != nil {
		t.Fatalf("ask returned unexpected command: %v", cmd)
	}
	if !m.promptOpen || m.promptKind != promptKindContainerBuildTag {
		t.Fatalf("no tag prompt: open=%v kind=%v", m.promptOpen, m.promptKind)
	}
	if !strings.Contains(m.prompt.View(), "shop:latest") {
		t.Fatalf("tag prompt does not suggest the folder name: %q", ansi.Strip(m.prompt.View()))
	}
	steps := []struct {
		kind  promptKindEnum
		value string
	}{
		{promptKindContainerBuildTag, "shop:dev"},
		{promptKindContainerBuildArgs, "GO=1.23"},
		{promptKindContainerBuildArgs, ""},
		{promptKindContainerBuildTarget, "runtime"},
		{promptKindContainerBuildPlatform, "linux/amd64"},
	}
	for _, s := range steps {
		if cmd := m.onContainerBuildPrompt(s.kind, s.value); cmd != nil {
			t.Fatalf("step %v %q returned unexpected command", s.kind, s.value)
		}
	}
	if !m.confirmOpen || m.confirmKind != confirmKindContainerHostAction || m.pendingHostOperation != "build-image" {
		t.Fatalf("no build card: open=%v kind=%v op=%q", m.confirmOpen, m.confirmKind, m.pendingHostOperation)
	}
	if b := m.confirm.Buttons(); len(b) != 2 || b[0].ID != "cancel" || b[1].ID != "build" {
		t.Fatalf("buttons = %#v, want Cancel first then Build", b)
	}
	// The command wraps over the card's lines: read it without the borders.
	card := strings.Join(strings.Fields(strings.ReplaceAll(ansi.Strip(m.confirm.View()), "│", " ")), " ")
	for _, want := range []string{"-t shop:dev", "--build-arg GO=1.23", "--target runtime", "--platform linux/amd64"} {
		if !strings.Contains(card, want) {
			t.Errorf("card %q misses %q", card, want)
		}
	}
	m.runHostAction("cancel")
	if m.pendingBuild.Dockerfile != "" || len(m.terminalTabs) != 0 {
		t.Fatal("Cancel kept the build or started a terminal job")
	}
}

func TestBuildImageRejectsDashValues(t *testing.T) {
	m := svcTestModel(svcHostState(), 120, 40)
	m.askImageBuild(writeTestDockerfile(t))
	if cmd := m.onContainerBuildPrompt(promptKindContainerBuildTag, "--push"); cmd == nil {
		t.Fatal("dash tag was accepted")
	}
	if m.confirmOpen {
		t.Fatal("dash tag opened the build card")
	}
}

func TestBuildImageMissingDockerfileShowsToast(t *testing.T) {
	m := svcTestModel(svcHostState(), 120, 40)
	if cmd := m.askImageBuild(filepath.Join(t.TempDir(), "Dockerfile")); cmd == nil || m.promptOpen {
		t.Fatal("missing Dockerfile did not show a toast")
	}
}

func TestExplorerMenuOffersBuildImageOnlyForDockerfiles(t *testing.T) {
	has := func(items []string) bool {
		for _, id := range items {
			if id == "build-image" {
				return true
			}
		}
		return false
	}
	ids := func(path string) []string {
		var out []string
		for _, it := range explorerFileMenuItemsFor(path) {
			out = append(out, it.ID)
		}
		return out
	}
	if !has(ids("/w/Dockerfile")) || !has(ids("/w/api.Dockerfile")) {
		t.Fatal("Dockerfile menu misses Build Image…")
	}
	if has(ids("/w/main.go")) {
		t.Fatal("plain file menu offers Build Image…")
	}
}

func TestBuildImagePaletteEntryHasPolicy(t *testing.T) {
	found := false
	for _, c := range servicesPaletteItems() {
		if c.ID == "containers-image-build" {
			found = true
		}
	}
	if !found {
		t.Fatal("palette has no containers-image-build")
	}
	if p, ok := aiActionPolicies["containers-image-build"]; !ok || p.Gate == aiGateAuto {
		t.Fatalf("containers-image-build policy = %+v, %v; want a non-auto policy", p, ok)
	}
}
